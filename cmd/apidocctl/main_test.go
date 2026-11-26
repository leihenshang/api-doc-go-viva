package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeCollection 造一个最小集合：清单 + 两条请求（一条成功、一条状态码不对）。
func writeCollection(t *testing.T, url string, failURL string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"opencollection.yml": "opencollection: 1.0.0\ninfo:\n  name: CLI 集合\n",
		"健康.yml": fmt.Sprintf(`info:
  name: 健康检查
  type: http
  seq: 1
meta:
  uid: cli-1
http:
  method: GET
  url: %s/ok
  body:
    type: none
`, url),
		"故障.yml": fmt.Sprintf(`info:
  name: 故障
  type: http
  seq: 2
meta:
  uid: cli-2
http:
  method: GET
  url: %s/fail
  body:
    type: none
`, failURL),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	return dir
}

// TestRunTextReport 终端报告：一条通过一条失败，退出码 1。
func TestRunTextReport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--collection", dir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("有失败应返回 1，实际 %d（stderr=%s）", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "健康检查") || !strings.Contains(out, "故障") {
		t.Fatalf("报告里缺条目：%s", out)
	}
	if !strings.Contains(out, "2 条：1 通过，1 失败") {
		t.Fatalf("汇总不对：%s", out)
	}
}

// TestRunJUnitToFile 报告写文件：XML 里有 testcase。
func TestRunJUnitToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	out := filepath.Join(t.TempDir(), "report.xml")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"run", "--collection", dir, "--filter", "健康",
		"--reporter", "junit", "--output", out,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("全通过应返回 0，实际 %d（stderr=%s）", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读报告: %v", err)
	}
	if !strings.Contains(string(data), "<testcase") || !strings.Contains(string(data), "健康检查") {
		t.Fatalf("报告内容不对：%s", data)
	}
}

// TestRunRejectsMissingCollection 用法错误返回 2。
func TestRunRejectsMissingCollection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run"}, &stdout, &stderr); code != 2 {
		t.Fatalf("缺 --collection 应返回 2，实际 %d", code)
	}
	if code := Run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("无参数应返回 2，实际 %d", code)
	}
	if code := Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help 应返回 0，实际 %d", code)
	}
}

// TestListShowsGroupsAndFilters list：方法 / 名称 / 分组 / 地址都列出来，--filter 生效。
func TestListShowsGroupsAndFilters(t *testing.T) {
	dir := writeCollection(t, "http://example.invalid", "http://example.invalid")
	mkdir := filepath.Join(dir, "用户")
	if err := os.MkdirAll(mkdir, 0o755); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	nested := `info:
  name: 登录
  type: http
  seq: 1
meta:
  uid: cli-3
http:
  method: POST
  url: http://example.invalid/login
  body:
    type: none
`
	if err := os.WriteFile(filepath.Join(mkdir, "登录.yml"), []byte(nested), 0o644); err != nil {
		t.Fatalf("写分组请求: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"list", "--collection", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("list 退出码 %d（stderr=%s）", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"分组：/", "分组：用户", "GET", "POST", "登录", "用户/登录.yml", "http://example.invalid/login"} {
		if !strings.Contains(out, want) {
			t.Fatalf("列表缺少 %q：\n%s", want, out)
		}
	}

	stdout.Reset()
	if code := Run([]string{"list", "--collection", dir, "--filter", "登录"}, &stdout, &stderr); code != 0 {
		t.Fatalf("带过滤退出码 %d", code)
	}
	filtered := stdout.String()
	if !strings.Contains(filtered, "登录") || strings.Contains(filtered, "健康检查") {
		t.Fatalf("--filter 没生效：\n%s", filtered)
	}
	if !strings.Contains(filtered, "1 个请求") {
		t.Fatalf("计数不对：\n%s", filtered)
	}
}

// TestRunJSONReport：--reporter json 输出可被解析，字段与 Result 对齐（毫秒、failed、汇总）。
func TestRunJSONReport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--collection", dir, "--reporter", "json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("有失败应返回 1，实际 %d（stderr=%s）", code, stderr.String())
	}
	var report struct {
		Collection string `json:"collection"`
		Total      int    `json:"total"`
		Passed     int    `json:"passed"`
		Failed     int    `json:"failed"`
		Results    []struct {
			Name       string `json:"name"`
			Method     string `json:"method"`
			Status     int    `json:"status"`
			DurationMS int64  `json:"durationMs"`
			Failed     bool   `json:"failed"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, stdout.String())
	}
	if report.Total != 2 || report.Passed != 1 || report.Failed != 1 {
		t.Fatalf("汇总不对：%+v", report)
	}
	if len(report.Results) != 2 || report.Results[0].Method != "GET" || report.Results[0].Status != 200 {
		t.Fatalf("逐条结果不对：%+v", report.Results)
	}
}

// TestRunRejectsUnknownReporter 未知格式按用法错退出，而不是悄悄退回 text。
func TestRunRejectsUnknownReporter(t *testing.T) {
	dir := writeCollection(t, "http://example.invalid", "http://example.invalid")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--collection", dir, "--reporter", "yaml"}, &stdout, &stderr); code != 2 {
		t.Fatalf("未知格式应返回 2，实际 %d", code)
	}
	if !strings.Contains(stderr.String(), "不认识的报告格式") {
		t.Fatalf("stderr 提示不对：%s", stderr.String())
	}
}

// TestUsageListsNewCommands --help 列出 list 与 json。
func TestUsageListsNewCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--help 退出码 %d", code)
	}
	for _, want := range []string{"apidocctl list", "junit / json", "--no-color"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("--help 缺少 %q：\n%s", want, stdout.String())
		}
	}
}

// TestRunRetryFlag --retry 1：偶发失败的请求重试一次就通过，退出码从 1 变 0。
func TestRunRetryFlag(t *testing.T) {
	var failHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			// 第一次 500，重试那次 200：模拟偶发抖动。
			if failHits.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run", "--collection", dir}, &stdout, &stderr); code != 1 {
		t.Fatalf("不重试时应失败（1），实际 %d", code)
	}

	failHits.Store(0)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"run", "--collection", dir, "--retry", "1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--retry 1 应通过（0），实际 %d（stderr=%s）", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "2 条：2 通过，0 失败") {
		t.Fatalf("汇总不对：%s", stdout.String())
	}
	if fails := failHits.Load(); fails != 2 {
		t.Fatalf("/fail 应被请求两次（一次失败 + 一次重试）：%d", fails)
	}

	// 负数按用法错退出。
	if code := Run([]string{"run", "--collection", dir, "--retry", "-1"}, &stdout, &stderr); code != 2 {
		t.Fatalf("负数 --retry 应返回 2，实际 %d", code)
	}
}
