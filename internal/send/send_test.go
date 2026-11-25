package send

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// captured 是服务端真收到的东西：用来证明界面上的字段确实进了请求。
type captured struct {
	method      string
	path        string
	query       string
	trace       string
	auth        string
	contentType string
	body        string
}

func server(t *testing.T, got *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = captured{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			trace:       r.Header.Get("X-Trace-Id"),
			auth:        r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"),
			body:        string(body),
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDoSendsTheCollectionRequest(t *testing.T) {
	var got captured
	srv := server(t, &got)
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: "创建", Type: "http"},
		HTTP: collection.HTTPBlock{
			Method: "POST",
			URL:    srv.URL + "/api/{{ver}}/users",
			Params: []collection.KV{{Name: "page", Value: "2", Enabled: true}},
			Headers: []collection.KV{
				{Name: "X-Trace-Id", Value: "{{$uuid}}", Enabled: true},
				{Name: "X-Off", Value: "no", Enabled: false},
			},
			Body: collection.Body{Type: "json", Raw: `{"name":"{{username}}"}`},
			Auth: &collection.Auth{Type: "bearer", Token: "{{token}}"},
		},
	}
	out := Do(context.Background(), Plan{
		File: file,
		Vars: map[string]string{"ver": "v1", "username": "neo", "token": "t0k"},
	})
	if out.Err != "" {
		t.Fatalf("发送失败: %s", out.Err)
	}
	if out.Status != http.StatusCreated || !strings.Contains(out.Body, `"ok":true`) {
		t.Fatalf("响应 = %d %q", out.Status, out.Body)
	}
	if got.method != "POST" || got.path != "/api/v1/users" || got.query != "page=2" {
		t.Fatalf("服务端收到 %s %s?%s", got.method, got.path, got.query)
	}
	if got.auth != "Bearer t0k" || got.body != `{"name":"neo"}` {
		t.Fatalf("认证/正文 = %q / %q", got.auth, got.body)
	}
	if !strings.HasPrefix(got.contentType, "application/json") {
		t.Fatalf("Content-Type = %q", got.contentType)
	}
	if len(got.trace) != 36 {
		t.Fatalf("$uuid 未展开：%q", got.trace)
	}
	if len(out.Missing) != 0 {
		t.Fatalf("不应有未解析变量：%v", out.Missing)
	}
}

// TestDoTruncatesWithoutDirtyingTheBody 正文超过上限时只留前 N 字节，
// 不在正文里拼提示（拼了 JSON 就毁了，格式化视图会整段退化成纯文本）。
func TestDoTruncatesWithoutDirtyingTheBody(t *testing.T) {
	payload := strings.Repeat("a", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: "大响应", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL},
	}
	out := Do(context.Background(), Plan{File: file, MaxBody: 32})
	if out.Err != "" {
		t.Fatalf("发送失败: %s", out.Err)
	}
	if !out.Truncated {
		t.Fatal("应标记为截断")
	}
	if len(out.Body) != 32 || out.Size != 32 {
		t.Fatalf("正文应只留前 32 字节：len=%d size=%d", len(out.Body), out.Size)
	}
	if strings.ContainsAny(out.Body, "…（）") {
		t.Fatalf("正文里不应拼提示文字：%q", out.Body)
	}
	if out.TotalSize != int64(len(payload)) {
		t.Fatalf("总长度应取 Content-Length：%d，期望 %d", out.TotalSize, len(payload))
	}
}

func TestDoReportsUnresolvedVariables(t *testing.T) {
	var got captured
	srv := server(t, &got)
	file := &collection.RequestFile{
		Info: collection.FileInfo{Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL + "/{{nope}}"},
	}
	out := Do(context.Background(), Plan{File: file, Vars: nil})
	if len(out.Missing) != 1 || out.Missing[0] != "nope" {
		t.Fatalf("missing = %v", out.Missing)
	}
}

func TestDoReportsConnectionFailure(t *testing.T) {
	srv := server(t, &captured{})
	url := srv.URL
	srv.Close()
	file := &collection.RequestFile{
		Info: collection.FileInfo{Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: url},
	}
	if out := Do(context.Background(), Plan{File: file}); out.Err == "" {
		t.Fatal("连接失败应写进 Err")
	}
}

func TestDoSendsMultipartForm(t *testing.T) {
	var got captured
	srv := server(t, &got)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "note.txt"), "附件内容")
	file := &collection.RequestFile{
		Info: collection.FileInfo{Type: "http"},
		HTTP: collection.HTTPBlock{Method: "POST", URL: srv.URL + "/upload", Body: collection.Body{
			Type: "multipart",
			Form: []collection.KV{
				{Name: "title", Value: "{{name}}", Enabled: true},
				{Name: "file", Value: "note.txt", Type: "file", Enabled: true},
				{Name: "off", Value: "no", Enabled: false},
			},
		}},
	}
	out := Do(context.Background(), Plan{File: file, Vars: map[string]string{"name": "报告"}, Dir: dir})
	if out.Err != "" {
		t.Fatalf("发送失败: %s", out.Err)
	}
	if !strings.HasPrefix(got.contentType, "multipart/form-data") {
		t.Fatalf("Content-Type = %q", got.contentType)
	}
	if !strings.Contains(got.body, "name=\"title\"") || !strings.Contains(got.body, "报告") {
		t.Fatalf("表单缺少文本字段：%q", got.body)
	}
	if !strings.Contains(got.body, "filename=\"note.txt\"") || !strings.Contains(got.body, "附件内容") {
		t.Fatalf("表单缺少文件：%q", got.body)
	}
	if strings.Contains(got.body, "name=\"off\"") {
		t.Fatalf("禁用的表单行不应发送：%q", got.body)
	}
	if len(out.Headers) == 0 {
		t.Fatal("响应头应被记录")
	}
}

func TestDoRejectsUnsupportedBody(t *testing.T) {
	file := &collection.RequestFile{
		Info: collection.FileInfo{Type: "http"},
		HTTP: collection.HTTPBlock{Method: "POST", URL: "http://example.invalid", Body: collection.Body{Type: "xml"}},
	}
	out := Do(context.Background(), Plan{File: file})
	if !strings.Contains(out.Err, "xml") {
		t.Fatalf("Err = %q", out.Err)
	}
}

// writeFile 是测试里落一个文件（multipart 的 file 行要从磁盘读）。
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
}

func TestDoInsecureSSLDefaultsAndFileOverride(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: "自签", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: srv.URL},
	}

	// 全局默认打开：不校验证书，请求能通。
	out := Do(context.Background(), Plan{File: file, InsecureSSL: true})
	if out.Err != "" || out.Status != http.StatusOK {
		t.Fatalf("全局忽略证书失败: %+v", out)
	}

	// 请求文件显式 false 优先于全局 true。
	off := false
	file.Settings = &collection.RequestSettings{InsecureSSL: &off}
	out = Do(context.Background(), Plan{File: file, InsecureSSL: true})
	if out.Err == "" || !strings.Contains(out.Err, "certificate") {
		t.Fatalf("请求文件的 insecureSsl:false 应覆盖全局：%+v", out)
	}
}

// TestCollectionDefaultsApply 集合公共参数：默认头每条请求都带，同名以请求为准；
// 认证为 inherit 时用集合的 bearer。
func TestCollectionDefaultsApply(t *testing.T) {
	type captured struct {
		team, debug, auth string
	}
	got := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- captured{
			team:  r.Header.Get("X-Team"),
			debug: r.Header.Get("X-Debug"),
			auth:  r.Header.Get("Authorization"),
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	plan := Plan{
		File: &collection.RequestFile{
			Info: collection.FileInfo{Name: "继承", Type: "http"},
			HTTP: collection.HTTPBlock{
				Method: "GET", URL: srv.URL + "/x",
				Headers: []collection.KV{{Name: "X-Debug", Value: "请求值", Enabled: true}},
				Auth:    &collection.Auth{Type: "inherit"},
			},
		},
		Defaults: collection.Defaults{
			Headers: []collection.KV{
				{Name: "X-Team", Value: "{{team}}", Enabled: true},
				{Name: "X-Debug", Value: "集合值", Enabled: true},
			},
			Auth: &collection.Auth{Type: "bearer", Token: "{{token}}"},
		},
		Vars: map[string]string{"team": "core", "token": "t-1"},
	}
	if out := Do(context.Background(), plan); out.Err != "" {
		t.Fatalf("发送失败：%+v", out)
	}
	seen := <-got
	if seen.team != "core" {
		t.Fatalf("默认头没带上或变量没替换：%q", seen.team)
	}
	if seen.debug != "请求值" {
		t.Fatalf("同名头应以请求为准，实际 %q", seen.debug)
	}
	if seen.auth != "Bearer t-1" {
		t.Fatalf("inherit 应沿用集合认证，实际 %q", seen.auth)
	}

	// 请求自己写了认证时，集合认证不生效。
	plan.File.HTTP.Auth = &collection.Auth{Type: "bearer", Token: "own"}
	Do(context.Background(), plan)
	if seen := <-got; seen.auth != "Bearer own" {
		t.Fatalf("请求自己的认证应覆盖 inherit，实际 %q", seen.auth)
	}
}

// TestPlaceholders 列出 {{变量}} 的名字与字节区间：切片必须原样落在占位符上。
func TestPlaceholders(t *testing.T) {
	text := "{{ host }}/api/{{token}}/{{host}}"
	got := Placeholders(text)
	if len(got) != 3 {
		t.Fatalf("应有 3 处占位符，实际 %d: %+v", len(got), got)
	}
	for index, want := range []string{"host", "token", "host"} {
		if got[index].Name != want {
			t.Fatalf("第 %d 个变量名应为 %q，实际 %q", index, want, got[index].Name)
		}
		if span := text[got[index].Start:got[index].End]; !strings.HasPrefix(span, "{{") || !strings.HasSuffix(span, "}}") {
			t.Fatalf("第 %d 处区间没框住占位符：%q", index, span)
		}
	}
	if Placeholders("没有变量") != nil {
		t.Fatal("没有变量时应返回 nil")
	}
}

// TestVarNames 变量名去重且保持出现顺序：界面提示按这个顺序列。
func TestVarNames(t *testing.T) {
	got := VarNames("{{a}} {{b}}", "{{b}} {{c}}")
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("变量名应为 a,b,c，实际 %v", got)
	}
	if VarNames("plain", "") != nil {
		t.Fatal("没有变量时应返回 nil")
	}
}
