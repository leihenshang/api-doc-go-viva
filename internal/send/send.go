// Package send 把一条集合请求变成真实的 HTTP 调用。
//
// 只依赖 net/http 与集合格式定义：界面上「发送」按下的那一层就是它。验证版刻意不做
// multipart、流式响应与脚本断言，那些是客户端正式实现的部分，不是框架能力问题。
package send

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	// Dir 是集合根：multipart 的 file 行按它解析相对路径。
	Dir string
	// FollowRedirects 是全局默认；请求文件里的 settings.followRedirects 优先。
	FollowRedirects *bool
	// InsecureSSL 是全局默认；请求文件里的 settings.insecureSsl 显式设置时优先。
	InsecureSSL bool
	// Proxy 是代理地址（http / https / socks5），空表示直连。
	Proxy string
	// ClientCert / ClientKey 是双向 TLS 的证书与私钥（PEM 路径），都填才生效。
	ClientCert, ClientKey string
	// Defaults 是集合级公共参数：请求头默认值，以及认证类型为 inherit 时用的认证。
	Defaults collection.Defaults
}

// Header 响应头的一行（按名称排序，便于稳定展示）。
type Header struct {
	Name  string
	Value string
}

// Outcome 一次发送的结果；Err 非空表示根本没拿到响应。
type Outcome struct {
	Status   int
	Reason   string
	Duration time.Duration
	Size     int // 实际读回的字节数（截断时就是上限）
	Body     string
	Headers  []Header
	Missing  []string // 未解析的 {{变量}}
	Err      string
	// URL 是真正发出去的地址（变量已替换、查询参数已拼上），历史与错误提示都用它。
	URL string
	// ContentType 是响应体的 MIME；Filename 来自 Content-Disposition（没有就是空）。
	// 两者一起决定「这是不是一份要保存的文件」。
	ContentType string
	Filename    string
	// Proto 是实际用的协议（HTTP/1.1、HTTP/2.0），TLS 是协商到的 TLS 版本（明文为空）。
	// Proto / TLS / Redirects / Remote 四个一起构成响应区那行「连接信息」。
	Proto     string
	TLS       string
	Redirects int
	Remote    string
	// ReqHeaders / ReqParams / ReqBody 是**真正发出去**的请求快照（变量已替换），
	// 给历史详情用。请求体超过 reqBodyLimit 会截断并在 ReqBodyTruncated 标出来。
	ReqHeaders       []Header
	ReqParams        []Header
	ReqBody          string
	ReqBodyTruncated bool
	// Truncated 表示正文超过 MaxBody，只读回了前 MaxBody 字节；TotalSize 是
	// 服务端声明的总长度（没有 Content-Length 时为 0，即未知）。
	// Body 里**不拼提示文字**：那会破坏 JSON，让格式化视图整段退化成纯文本。
	Truncated bool
	TotalSize int64
}

// Do 执行一条 http 请求并读回响应体（最多 Plan.MaxBody 字节）。
func Do(ctx context.Context, plan Plan) Outcome {
	start := time.Now()
	req, missing, err := build(ctx, plan)
	if err != nil {
		return Outcome{Err: err.Error(), Missing: missing, Duration: time.Since(start)}
	}
	sent := req.URL.String()
	snapshot := snapshotRequest(req)
	// 记下 host：Cookie Jar 只能按 URL 反查，列全部 cookie 时要用。
	noteURL(req.URL)
	client, redirectsPtr, err := newClient(plan)
	if err != nil {
		return Outcome{Err: err.Error(), Missing: missing, Duration: time.Since(start), URL: sent}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Outcome{Err: err.Error(), Missing: missing, Duration: time.Since(start), URL: sent}
	}
	defer func() { _ = resp.Body.Close() }()
	// 客户端在 CheckRedirect 里累加跳数，这里取快照（读响应时不再变化）。
	redirects := 0
	if redirectsPtr != nil {
		redirects = *redirectsPtr
	}
	// 重定向后的最终地址也要记：cookie 可能落在链尾那个域名上。
	if resp.Request != nil {
		noteURL(resp.Request.URL)
	}
	// 这次请求可能种下 / 更新了 cookie：落盘一次（没开落盘时是空操作）。
	persist()

	limit := plan.MaxBody
	if limit <= 0 {
		limit = 1 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Outcome{Err: "读取响应: " + err.Error(), Status: resp.StatusCode, Missing: missing}
	}
	body := string(data)
	truncated := int64(len(data)) > limit
	if truncated {
		// 只留正文字节，提示由界面按 Truncated/TotalSize 自己画。
		body = string(data[:limit])
	}
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	return Outcome{
		Status:           resp.StatusCode,
		Reason:           http.StatusText(resp.StatusCode),
		Duration:         time.Since(start),
		Size:             len(body),
		Body:             body,
		Headers:          headerRows(resp.Header),
		Missing:          missing,
		URL:              sent,
		ContentType:      resp.Header.Get("Content-Type"),
		Filename:         filenameFrom(resp.Header),
		Proto:            resp.Proto,
		TLS:              tlsLabel(resp),
		Redirects:        redirects,
		Remote:           remoteHost(resp),
		ReqHeaders:       snapshot.headers,
		ReqParams:        snapshot.params,
		ReqBody:          snapshot.body,
		ReqBodyTruncated: snapshot.truncated,
		Truncated:        truncated,
		TotalSize:        total,
	}
}

// newClient 按请求的超时与证书设置建客户端。
func newClient(plan Plan) (*http.Client, *int, error) {
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
	redirects := 0
	client, err := newClientWith(plan, timeout, follow, &redirects)
	if err != nil {
		return nil, nil, err
	}
	return client, &redirects, nil
}

// newClientWith 组装客户端：超时、重定向、TLS、代理与客户端证书。
func newClientWith(plan Plan, timeout time.Duration, follow bool, redirects *int) (*http.Client, error) {
	// 共享 Cookie Jar：同一个进程里发出去的请求共用一个会话（登录一次，后面自动带上）。
	client := useJar(&http.Client{Timeout: timeout})
	// 记下跳了几次：响应区那行「连接信息」要显示它，用户才知道自己看到的是第几跳的页面。
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if redirects != nil {
			*redirects = len(via)
		}
		if !follow {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("重定向超过 10 次，停止")
		}
		return nil
	}
	insecure := plan.InsecureSSL
	if plan.File != nil && plan.File.Settings != nil && plan.File.Settings.InsecureSSL != nil {
		insecure = *plan.File.Settings.InsecureSSL
	}
	if insecure || plan.Proxy != "" || (plan.ClientCert != "" && plan.ClientKey != "") {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		var tlsConfig *tls.Config
		if insecure || plan.ClientCert != "" {
			tlsConfig = &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // 由集合里的 insecureSsl 显式开启
		}
		if plan.ClientCert != "" && plan.ClientKey != "" {
			cert, err := tls.LoadX509KeyPair(plan.ClientCert, plan.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("客户端证书: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		transport.TLSClientConfig = tlsConfig
		if proxy := strings.TrimSpace(plan.Proxy); proxy != "" {
			proxyURL, err := neturl.Parse(proxy)
			if err != nil || proxyURL.Host == "" {
				return nil, fmt.Errorf("代理地址无效（要写成 http://host:port）: %q", proxy)
			}
			transport.Proxy = http.ProxyURL(proxyURL)
		}
		client.Transport = transport
	}
	return client, nil
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
	if a := effectiveAuth(block.Auth, plan.Defaults); a != nil && a.Type == "apikey" && a.In == "query" {
		query.Set(exp.text(a.Key), exp.text(a.Value))
	}
	target.RawQuery = query.Encode()

	body, contentType, err := bodyOf(block.Body, exp, plan.Dir)
	if err != nil {
		return nil, exp.missing, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, exp.missing, fmt.Errorf("构造请求: %w", err)
	}
	applyHeaders(req, plan.Defaults.Headers, exp)
	applyHeaders(req, block.Headers, exp)
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	applyAuth(req, effectiveAuth(block.Auth, plan.Defaults), exp)
	return req, exp.missing, nil
}

// bodyOf 返回请求体与默认 Content-Type；类型口径与客户端一致（空串 / 'none' 都表示没有请求体）。
// multipart 的 file 行按集合根解析相对路径，绝对路径原样使用。
func bodyOf(body collection.Body, exp *expander, dir string) (io.Reader, string, error) {
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
	case "multipart":
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for _, kv := range body.Form {
			if !kv.Enabled || strings.TrimSpace(kv.Name) == "" {
				continue
			}
			name := exp.text(kv.Name)
			value := exp.text(kv.Value)
			if kv.Type == "file" {
				path := value
				if !filepath.IsAbs(path) {
					path = filepath.Join(dir, filepath.FromSlash(path))
				}
				file, err := os.Open(path)
				if err != nil {
					return nil, "", fmt.Errorf("打开表单文件 %s: %w", value, err)
				}
				part, err := writer.CreateFormFile(name, filepath.Base(path))
				if err != nil {
					_ = file.Close()
					return nil, "", fmt.Errorf("构造表单文件项: %w", err)
				}
				_, copyErr := io.Copy(part, file)
				closeErr := file.Close()
				if copyErr != nil {
					return nil, "", fmt.Errorf("读取表单文件 %s: %w", value, copyErr)
				}
				if closeErr != nil {
					return nil, "", fmt.Errorf("关闭表单文件 %s: %w", value, closeErr)
				}
				continue
			}
			if err := writer.WriteField(name, value); err != nil {
				return nil, "", fmt.Errorf("写表单字段: %w", err)
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", fmt.Errorf("结束表单: %w", err)
		}
		return &buf, writer.FormDataContentType(), nil
	default:
		return nil, "", fmt.Errorf("请求体类型 %q 验证版未支持（只做 json/text/form/multipart）", body.Type)
	}
}

// rawOf 取正文：客户端把 json/text 的原文放 raw，Bruno 兼容字段是 data。
func rawOf(body collection.Body) string {
	if body.Raw != "" {
		return body.Raw
	}
	return body.Data
}

// effectiveAuth 选这次请求实际生效的认证：请求写了 inherit 就用集合的 defaults.auth，
// 否则用请求自己的（类型为 none 时不会挂任何头）。
func effectiveAuth(auth *collection.Auth, defaults collection.Defaults) *collection.Auth {
	if auth == nil {
		return nil
	}
	if auth.Type == "inherit" {
		return defaults.Auth
	}
	return auth
}

// applyHeaders 把启用的请求头写到 req；后写的覆盖先写的（集合默认在前、请求在后）。
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

// headerRows 把响应头摊平成排好序的键值行（同名字段用逗号连接）。
func headerRows(header http.Header) []Header {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Header, 0, len(names))
	for _, name := range names {
		out = append(out, Header{Name: name, Value: strings.Join(header[name], ", ")})
	}
	return out
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

// reqBodyLimit 是历史里保存的请求体上限：整段 body 进 jsonl 会把历史文件撑爆。
const reqBodyLimit = 8 << 10

// requestSnapshot 是这次发送的请求快照（历史详情用）。
type requestSnapshot struct {
	headers   []Header
	params    []Header
	body      string
	truncated bool
}

// snapshotRequest 抓最终请求：头（含认证与默认头）、查询参数、请求体。
// 请求体已经被 build 写进 req.GetBody，这里不去重新编码一遍。
func snapshotRequest(req *http.Request) requestSnapshot {
	out := requestSnapshot{headers: headerRows(req.Header)}
	query := req.URL.Query()
	for key, values := range query {
		for _, value := range values {
			out.params = append(out.params, Header{Name: key, Value: value})
		}
	}
	sort.Slice(out.params, func(i, j int) bool {
		if out.params[i].Name != out.params[j].Name {
			return out.params[i].Name < out.params[j].Name
		}
		return out.params[i].Value < out.params[j].Value
	})
	if req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			data, _ := io.ReadAll(io.LimitReader(body, reqBodyLimit+1))
			_ = body.Close()
			out.body = string(data)
			if len(data) > reqBodyLimit {
				out.body = string(data[:reqBodyLimit])
				out.truncated = true
			}
		}
	}
	return out
}

// MaskHeaders 把敏感的请求头值打码：历史会留在这台机器上，不把会话抄进明文。
func MaskHeaders(headers []Header) []Header {
	out := make([]Header, len(headers))
	for i, header := range headers {
		out[i] = header
		if sensitiveHeader(header.Name) {
			out[i].Value = "••••（已打码）"
		}
	}
	return out
}

// sensitiveHeader 报告某个头是不是「不该落盘明文」的那种。
func sensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "cookie", "set-cookie", "proxy-authorization", "x-api-key", "api-key":
		return true
	}
	return false
}

// Resolve 把一段文本里的 {{变量}} 替换掉，返回结果与没解析到的变量名。
// gRPC 的地址 / 消息 / 元数据复用这一套语义，免得两处各写一份。
func Resolve(text string, vars map[string]string) (string, []string) {
	exp := &expander{vars: vars}
	out := exp.text(text)
	return out, exp.missing
}

// Placeholder 是文本里的一处 {{变量}}：名字，以及它在原文里的字节区间（半开）。
type Placeholder struct {
	Name  string
	Start int
	End   int
}

// Placeholders 按出现顺序列出文本里的 {{变量}}，同一名字出现多次就列多次。
//
// 界面用它把「悬停显示变量值」落到具体的一段文本上；真正替换仍走 Resolve，
// 两处共用同一个 placeholderPattern，免得识别规则分叉。
func Placeholders(text string) []Placeholder {
	indexes := placeholderPattern.FindAllStringSubmatchIndex(text, -1)
	if len(indexes) == 0 {
		return nil
	}
	out := make([]Placeholder, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, Placeholder{
			Name:  strings.TrimSpace(text[index[2]:index[3]]),
			Start: index[0],
			End:   index[1],
		})
	}
	return out
}

// VarNames 是文本里出现过的变量名，按出现顺序去重；界面悬停提示用它列清单。
func VarNames(texts ...string) []string {
	var names []string
	seen := make(map[string]bool, 4)
	for _, text := range texts {
		for _, placeholder := range Placeholders(text) {
			if placeholder.Name == "" || seen[placeholder.Name] {
				continue
			}
			seen[placeholder.Name] = true
			names = append(names, placeholder.Name)
		}
	}
	return names
}

// tlsLabel 把协商到的 TLS 版本翻成人看的字；明文响应返回空串。
func tlsLabel(resp *http.Response) string {
	if resp == nil || resp.TLS == nil {
		return ""
	}
	switch resp.TLS.Version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	}
	return "TLS"
}

// remoteHost 是最终落到的地址（重定向后是链尾那个）。
func remoteHost(resp *http.Response) string {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	return resp.Request.URL.Host
}
