// Package updater 做「检查有没有新版本」这一件事。
//
// 刻意只做到「查 + 告诉你在哪下载」：viva 自带的 update 包能做签名清单校验、
// 断点续传与原地替换，但它要求发布侧维护一份带 ed25519 签名的 feed。本仓库
// 现在没有那条发布流水线，硬接只会做出一个永远装不上东西的更新器。
//
// 所以这里只读 GitHub Releases 的公开 API，不下载、不执行、不问后台悄悄联网：
// 只有用户点「检查更新」时才发一次请求。
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Version 是当前构建的版本；发布时可用 -ldflags "-X ...updater.Version=x.y.z" 覆盖。
var Version = "0.1.0"

// Repo 是默认的发布仓库（设置里可改）。
var Repo = "leihenshang/api-doc-go"

// APIBase 是 GitHub API 根地址（测试里换成 httptest）。
var APIBase = "https://api.github.com"

// ErrUpToDate 表示没有更新的版本。
var ErrUpToDate = errors.New("已经是最新版本")

// Release 是一次检查的结果。
type Release struct {
	Version string
	URL     string
	Notes   string
	// Current 是本地版本，方便界面直接写「v0.1.0 → v0.2.0」。
	Current string
}

// Check 查最新 release 并与本地版本比较。
//
// 返回 ErrUpToDate 时 release 里仍有版本信息（界面可以显示「已是最新 vX」）；
// 网络错误、返回格式不对等真实失败会带上下文返回。
func Check(ctx context.Context, client *http.Client, repo, current string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return Release{}, errors.New("没有配置发布仓库")
	}
	url := strings.TrimRight(APIBase, "/") + "/repos/" + repo + "/releases/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "api-doc-go-viva/"+current)
	response, err := client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("连不上 %s：%w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return Release{}, fmt.Errorf("仓库 %s 还没有发布任何版本", repo)
	}
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("检查更新失败：HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Release{}, fmt.Errorf("读取发布信息：%w", err)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, fmt.Errorf("发布信息格式不对：%w", err)
	}
	latest := strings.TrimSpace(payload.TagName)
	if latest == "" || payload.Draft {
		return Release{}, errors.New("发布信息里没有可用的版本号")
	}
	release := Release{
		Version: latest, Current: current,
		URL: strings.TrimSpace(payload.HTMLURL), Notes: strings.TrimSpace(payload.Body),
	}
	if !newer(latest, current) {
		return release, ErrUpToDate
	}
	return release, nil
}

// newer 报告 candidate 是否比 current 新（按数字段比较，忽略 v 前缀与预发布后缀）。
func newer(candidate, current string) bool {
	left, right := numericParts(candidate), numericParts(current)
	for i := 0; i < len(left) || i < len(right); i++ {
		var a, b int
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

// numericParts 把 "v1.2.3-beta.1" 拆成 [1 2 3]（预发布后缀不参与比较）。
func numericParts(version string) []int {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if at := strings.IndexAny(version, "-+"); at >= 0 {
		version = version[:at]
	}
	parts := strings.Split(version, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, value)
	}
	return out
}
