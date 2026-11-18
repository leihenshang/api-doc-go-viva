// Package send 把一条集合请求变成真实的 HTTP 调用。
//
// 只依赖 net/http 与集合格式定义：界面上「发送」按下的那一层就是它。验证版刻意不做
// multipart、流式响应与脚本断言，那些是客户端正式实现的部分，不是框架能力问题。
package send

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// defaultTimeout 是未配置超时时的上限，避免界面被一个坏地址挂住。
const defaultTimeout = 20 * time.Second

// placeholderPattern 匹配 {{变量}}；变量名里不允许再出现花括号。
var placeholderPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Plan 一次发送的输入：请求本身、环境变量与上限。
type Plan struct {
	File    *collection.RequestFile
	Vars    map[string]string
	Timeout time.Duration
	MaxBody int64
	// FollowRedirects 是全局默认；请求文件里的 settings.followRedirects 优先。
	FollowRedirects *bool
}

// Outcome 一次发送的结果；Err 非空表示根本没拿到响应。
type Outcome struct {
	Status   int
	Reason   string
	Duration time.Duration
	Size     int
	Body     string
	Missing  []string // 未解析的 {{变量}}
	Err      string
}

// Do 执行一条 http 请求并读回响应体（最多 Plan.MaxBody 字节）。
func Do(ctx context.Context, plan Plan) Outcome {
	start := time.Now()
	req, missing, err := build(ctx, plan)
	if err != nil {
		return Outcome{Err: err.Error(), Missing: missing, Duration: time.Since(start)}
	}
	resp, err := newClient(plan).Do(req)
	if err != nil {
		return Outcome{Err: err.Error(), Missing: missing, Duration: time.Since(start)}
	}
	defer func() { _ = resp.Body.Close() }()

	limit := plan.MaxBody
	if limit <= 0 {
		limit = 1 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Outcome{Err: "读取响应: " + err.Error(), Status: resp.StatusCode, Missing: missing}
	}
	body := string(data)
	if int64(len(data)) > limit {
		body = string(data[:limit]) + "\n…（响应超过上限，已截断）"
	}
	return Outcome{
		Status:   resp.StatusCode,
		Reason:   http.StatusText(resp.StatusCode),
		Duration: time.Since(start),
		Size:     len(data),
		Body:     body,
		Missing:  missing,
	}
}

// newClient 按请求的超时与证书设置建客户端。
func newClient(plan Plan) *http.Client {
	timeout := plan.Timeout
	if timeout <= 0 && plan.File != nil && plan.File.Settings != nil {
		timeout = time.Duration(plan.File.Settings.TimeoutSec) * time.Second
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	follow := true
	if plan.FollowRedirects != nil {
		follow = *plan.FollowRedirects
	}
	if plan.File != nil && plan.File.Settings != nil && plan.File.Settings.FollowRedirects != nil {
		follow = *plan.File.Settings.FollowRedirects
	}
	client := &http.Client{Timeout: timeout}
	if !follow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if plan.File != nil && plan.File.Settings != nil && plan.File.Settings.InsecureSSL != nil &&
		*plan.File.Settings.InsecureSSL {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 由集合里的 insecureSsl 显式开启
		client.Transport = transport
	}
	return client
}

// build 把集合请求翻译成 *http.Request，同时收集没解析到的变量名。
func build(ctx context.Context, plan Plan) (*http.Request, []string, error) {
	if plan.File == nil {
		return nil, nil, fmt.Errorf("没有请求可发送")
	}
	block := plan.File.HTTP
	if plan.File.Info.Type != "http" && block.Method == "" && block.URL == "" {
		return nil, nil, fmt.Errorf("只支持 http 请求：%s", plan.File.Info.Type)
	}
	exp := &expander{vars: plan.Vars}
	method := strings.ToUpper(strings.TrimSpace(block.Method))
	if method == "" {
		method = http.MethodGet
	}
	target, err := neturl.Parse(exp.text(block.URL))
	if err != nil {
		return nil, exp.missing, fmt.Errorf("URL 无效: %w", err)
	}
	query := target.Query()
	for _, kv := range block.Params {
		if kv.Enabled && strings.TrimSpace(kv.Name) != "" {
			query.Add(exp.text(kv.Name), exp.text(kv.Value))
		}
	}
	if a := block.Auth; a != nil && a.Type == "apikey" && a.In == "query" {
		query.Set(exp.text(a.Key), exp.text(a.Value))
	}
	target.RawQuery = query.Encode()

	body, contentType, err := bodyOf(block.Body, exp)
	if err != nil {
		return nil, exp.missing, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, exp.missing, fmt.Errorf("构造请求: %w", err)
	}
	applyHeaders(req, block.Headers, exp)
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	applyAuth(req, block.Auth, exp)
	return req, exp.missing, nil
}

// bodyOf 返回请求体与默认 Content-Type；类型口径与客户端一致（”/'none' 都表示没有请求体）。
func bodyOf(body collection.Body, exp *expander) (io.Reader, string, error) {
	switch body.Type {
	case "", "none":
		return nil, "", nil
	case "json":
		return strings.NewReader(exp.text(rawOf(body))), "application/json", nil
	case "text":
		return strings.NewReader(exp.text(rawOf(body))), "text/plain; charset=utf-8", nil
	case "form":
		values := neturl.Values{}
		for _, kv := range body.Form {
			if kv.Enabled {
				values.Add(exp.text(kv.Name), exp.text(kv.Value))
			}
		}
		return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", fmt.Errorf("请求体类型 %q 验证版未支持（只做 json/text/form）", body.Type)
	}
}

// rawOf 取正文：客户端把 json/text 的原文放 raw，Bruno 兼容字段是 data。
func rawOf(body collection.Body) string {
	if body.Raw != "" {
		return body.Raw
	}
	return body.Data
}

func applyHeaders(req *http.Request, headers []collection.KV, exp *expander) {
	for _, kv := range headers {
		if !kv.Enabled || strings.TrimSpace(kv.Name) == "" {
			continue
		}
		req.Header.Set(exp.text(kv.Name), exp.text(kv.Value))
	}
}

// applyAuth 处理 basic/bearer/apikey(header)。apikey 的 query 位置在 build 里已拼好。
func applyAuth(req *http.Request, auth *collection.Auth, exp *expander) {
	if auth == nil {
		return
	}
	switch auth.Type {
	case "basic":
		req.SetBasicAuth(exp.text(auth.Username), exp.text(auth.Password))
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+exp.text(auth.Token))
	case "apikey":
		if auth.In != "query" {
			req.Header.Set(exp.text(auth.Key), exp.text(auth.Value))
		}
	}
}

// expander 做 {{变量}} 替换：环境变量 + 少量内置（$uuid / $timestamp）。
// 没解析到的变量保留原文并记名，界面据此提示，而不是静默发一个坏请求。
type expander struct {
	vars    map[string]string
	missing []string
	seen    map[string]bool
}

func (e *expander) text(value string) string {
	return placeholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := placeholderPattern.FindStringSubmatch(match)[1]
		if v, ok := e.lookup(name); ok {
			return v
		}
		if e.seen == nil {
			e.seen = map[string]bool{}
		}
		if !e.seen[name] {
			e.seen[name] = true
			e.missing = append(e.missing, name)
		}
		return match
	})
}

func (e *expander) lookup(name string) (string, bool) {
	switch name {
	case "$uuid":
		return newUUID(), true
	case "$timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10), true
	}
	value, ok := e.vars[name]
	return value, ok
}

// newUUID 生成 v4 UUID（不引第三方依赖）。
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
