package send

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestDoRejectsUnsupportedBody(t *testing.T) {
	file := &collection.RequestFile{
		Info: collection.FileInfo{Type: "http"},
		HTTP: collection.HTTPBlock{Method: "POST", URL: "http://example.invalid", Body: collection.Body{Type: "multipart"}},
	}
	out := Do(context.Background(), Plan{File: file})
	if !strings.Contains(out.Err, "multipart") {
		t.Fatalf("Err = %q", out.Err)
	}
}
