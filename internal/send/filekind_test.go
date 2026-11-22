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

// TestIsFileResponse 只有「明确要下载」或「不是文本」才算文件；JSON / 文本照常渲染。
func TestIsFileResponse(t *testing.T) {
	cases := []struct {
		name     string
		outcome  Outcome
		wantFile bool
	}{
		{"json", Outcome{ContentType: "application/json; charset=utf-8"}, false},
		{"纯文本", Outcome{ContentType: "text/plain"}, false},
		{"html", Outcome{ContentType: "text/html; charset=utf-8"}, false},
		{"没给类型", Outcome{}, false},
		{"图片", Outcome{ContentType: "image/png"}, true},
		{"pdf", Outcome{ContentType: "application/pdf"}, true},
		{"二进制流", Outcome{ContentType: "application/octet-stream"}, true},
		{"带 filename", Outcome{ContentType: "application/json", Filename: "a.json"}, true},
	}
	for _, tc := range cases {
		if got := tc.outcome.IsFile(); got != tc.wantFile {
			t.Errorf("%s: IsFile = %v，期望 %v", tc.name, got, tc.wantFile)
		}
	}
}

// TestFilenameFromDisposition 从 Content-Disposition 取名字，路径会被剥掉。
func TestFilenameFromDisposition(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{`attachment; filename="report.pdf"`, "report.pdf"},
		{`attachment; filename=../../etc/passwd`, "passwd"},
		{`inline`, ""},
		{``, ""},
		{`attachment; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`, "报告.pdf"},
	}
	for _, tc := range cases {
		header := http.Header{}
		if tc.header != "" {
			header.Set("Content-Disposition", tc.header)
		}
		if got := filenameFrom(header); got != tc.want {
			t.Errorf("%q → %q，期望 %q", tc.header, got, tc.want)
		}
	}
}

// TestSuggestedFileName 没有文件名时按类型给默认名。
func TestSuggestedFileName(t *testing.T) {
	if got := (Outcome{ContentType: "image/png"}).SuggestedFileName(); got != "response.bin" {
		t.Errorf("图片默认名 = %q", got)
	}
	if got := (Outcome{ContentType: "application/json"}).SuggestedFileName(); got != "response.json" {
		t.Errorf("JSON 默认名 = %q", got)
	}
	if got := (Outcome{Filename: "导出.csv"}).SuggestedFileName(); got != "导出.csv" {
		t.Errorf("有文件名时应直接用：%q", got)
	}
}

// TestOutcomeCarriesConnectionInfo 协议 / 跳数 / 最终地址都要带回来。
func TestOutcomeCarriesConnectionInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mid", http.StatusFound)
	})
	mux.HandleFunc("/mid", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/end", http.StatusFound)
	})
	mux.HandleFunc("/end", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	plan := Plan{File: &collection.RequestFile{
		Info: collection.FileInfo{Name: "跳转", Type: "http"},
		HTTP: collection.HTTPBlock{Method: "GET", URL: server.URL + "/start"},
	}}
	out := Do(context.Background(), plan)
	if out.Err != "" {
		t.Fatalf("Do: %s", out.Err)
	}
	if out.Redirects != 2 {
		t.Fatalf("应记到 2 次跳转，实际 %d", out.Redirects)
	}
	if !strings.HasSuffix(out.Remote, strings.TrimPrefix(server.URL, "http://")) {
		t.Fatalf("最终地址不对：%q", out.Remote)
	}
	if !strings.HasPrefix(out.Proto, "HTTP/") {
		t.Fatalf("协议不对：%q", out.Proto)
	}
	if out.TLS != "" {
		t.Fatalf("明文响应不该有 TLS 版本：%q", out.TLS)
	}
}

// TestOutcomeTLSVersionTLS1x HTTPS 响应要报出 TLS 版本。
func TestOutcomeTLSVersionTLS1x(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	plan := Plan{
		File: &collection.RequestFile{
			Info: collection.FileInfo{Name: "TLS", Type: "http"},
			HTTP: collection.HTTPBlock{Method: "GET", URL: server.URL + "/"},
		},
		InsecureSSL: true, // 自签证书
	}
	out := Do(context.Background(), plan)
	if out.Err != "" {
		t.Fatalf("Do: %s", out.Err)
	}
	if !strings.HasPrefix(out.TLS, "TLS 1.") {
		t.Fatalf("TLS 版本不对：%q", out.TLS)
	}
}
