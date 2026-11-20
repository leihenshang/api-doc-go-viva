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

func TestParseMultipartInsecureAndTimeout(t *testing.T) {
	file, err := Parse(`curl http://example.invalid/upload \
  -F 'title=报告' \
  -F 'file=@/tmp/note.txt;type=text/plain' \
  -k -m 12`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if file.HTTP.Body.Type != "multipart" || len(file.HTTP.Body.Form) != 2 {
		t.Fatalf("multipart 正文 = %+v", file.HTTP.Body)
	}
	rows := file.HTTP.Body.Form
	if rows[0].Name != "title" || rows[0].Type != "text" || rows[0].Value != "报告" {
		t.Fatalf("文本行 = %+v", rows[0])
	}
	if rows[1].Type != "file" || rows[1].Value != "/tmp/note.txt" {
		t.Fatalf("文件行 = %+v", rows[1])
	}
	if file.Settings == nil || file.Settings.InsecureSSL == nil || !*file.Settings.InsecureSSL {
		t.Fatalf("-k 没写进 insecureSsl：%+v", file.Settings)
	}
	if file.Settings.TimeoutSec != 12 {
		t.Fatalf("-m 没写进 timeout：%+v", file.Settings)
	}
}

func TestParseDataURLEncodeWithGet(t *testing.T) {
	file, err := Parse(`curl -G 'http://example.invalid/search' --data-urlencode 'q=a b'`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(file.HTTP.URL, "q=a+b") {
		t.Fatalf("query 没编码：%s", file.HTTP.URL)
	}
}

func TestParseRejectsUnsupported(t *testing.T) {
	if _, err := Parse(`curl -d @/tmp/a.bin http://example.invalid/upload`); err == nil {
		t.Fatal("-d @文件 应明确报错")
	}
	if _, err := Parse(`wget http://example.invalid`); err == nil {
		t.Fatal("非 curl 应报错")
	}
}
