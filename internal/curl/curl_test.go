package curl

import (
	"strings"
	"testing"
)

func TestParseCopiedCurl(t *testing.T) {
	file, err := Parse(`curl 'https://api.example.com/v1/users?page=2' \
  -X POST \
  -H 'Content-Type: application/json' \
  -H 'X-Trace: abc' \
  -u neo:secret \
  -d '{"name":"张三"}'`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.HTTP.Method != "POST" || !strings.Contains(file.HTTP.URL, "/v1/users?page=2") {
		t.Fatalf("请求 = %s %s", file.HTTP.Method, file.HTTP.URL)
	}
	if file.HTTP.Body.Type != "json" || file.HTTP.Body.Raw != `{"name":"张三"}` {
		t.Fatalf("正文 = %+v", file.HTTP.Body)
	}
	if len(file.HTTP.Headers) != 2 || file.HTTP.Headers[1].Name != "X-Trace" {
		t.Fatalf("请求头 = %+v", file.HTTP.Headers)
	}
	if auth := file.HTTP.Auth; auth == nil || auth.Type != "basic" || auth.Username != "neo" || auth.Password != "secret" {
		t.Fatalf("认证 = %+v", file.HTTP.Auth)
	}
	if file.Info.Name != "users" {
		t.Fatalf("默认名 = %q", file.Info.Name)
	}
}

func TestParseGetWithDataGoesToQuery(t *testing.T) {
	file, err := Parse(`curl -G 'http://example.invalid/search' -d 'q=viva' -d 'page=1'`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.HTTP.Method != "GET" || !strings.Contains(file.HTTP.URL, "q=viva") || file.HTTP.Body.Type != "none" {
		t.Fatalf("请求 = %s %s body=%+v", file.HTTP.Method, file.HTTP.URL, file.HTTP.Body)
	}
}

func TestGenerateCurl(t *testing.T) {
	file, err := Parse(`curl 'https://api.example.com/x' -X PUT -H 'Content-Type: application/json' -d '{"a":1}'`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	text := Generate(file)
	for _, want := range []string{"curl", "-X PUT", "-H 'Content-Type: application/json'", "-d '{\"a\":1}'"} {
		if !strings.Contains(text, want) {
			t.Fatalf("导出的 curl 缺少 %q：\n%s", want, text)
		}
	}
}

func TestParseRejectsUnsupported(t *testing.T) {
	if _, err := Parse(`curl -F 'file=@/tmp/a.bin' http://example.invalid/upload`); err == nil {
		t.Fatal("multipart 应明确报错")
	}
	if _, err := Parse(`wget http://example.invalid`); err == nil {
		t.Fatal("非 curl 应报错")
	}
}
