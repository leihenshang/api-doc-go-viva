package importers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bruSample 是一份贴近真实的 .bru：查询参数（含一条禁用的）、请求头、
// JSON 正文、bearer 认证、docs，以及一个不导入的脚本段。
const bruSample = `meta {
  name: 列表
  type: http
  seq: 3
}

get {
  url: {{host}}/api/users
  body: none
  auth: bearer
}

params:query {
  page: 1
  size: 20
  ~draft: true
}

headers {
  Accept: application/json
  ~X-Debug: 1
}

auth:bearer {
  token: {{token}}
}

body:json {
  {
    "q": "neo"
  }
}

script:pre-request {
  bru.setVar("t", Date.now());
}

docs {
  用户列表。
}
`

// writeBru 在临时目录里落一个 .bru。
func writeBru(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("写 %s: %v", rel, err)
	}
}

// TestBrunoParsesRequest 一个 .bru 的方法 / 地址 / 参数 / 头 / 正文 / 认证 / 文档都要转过来。
func TestBrunoParsesRequest(t *testing.T) {
	dir := t.TempDir()
	writeBru(t, dir, "bruno.json", `{"version":"1","name":"示例集合"}`)
	writeBru(t, dir, "用户/列表.bru", bruSample)

	items, summary, err := Bruno(dir, "")
	if err != nil {
		t.Fatalf("Bruno: %v", err)
	}
	if len(items) != 1 || items[0].Name != "用户" {
		t.Fatalf("应有一个「用户」分组：%+v", items)
	}
	if len(items[0].Items) != 1 {
		t.Fatalf("分组里应有一条请求：%+v", items[0].Items)
	}
	req := items[0].Items[0].Request
	if req == nil {
		t.Fatal("没有解析出请求")
	}
	if req.Info.Name != "列表" || req.Info.Seq != 3 {
		t.Fatalf("meta 没解析对：%+v", req.Info)
	}
	if req.HTTP.Method != "GET" || req.HTTP.URL != "{{host}}/api/users" {
		t.Fatalf("方法或地址不对：%s %s", req.HTTP.Method, req.HTTP.URL)
	}
	if len(req.HTTP.Params) != 3 {
		t.Fatalf("应有 3 个查询参数：%+v", req.HTTP.Params)
	}
	if req.HTTP.Params[2].Name != "draft" || req.HTTP.Params[2].Enabled {
		t.Fatalf("~ 前缀应表示未启用：%+v", req.HTTP.Params[2])
	}
	if len(req.HTTP.Headers) != 2 || req.HTTP.Headers[1].Enabled {
		t.Fatalf("请求头解析不对：%+v", req.HTTP.Headers)
	}
	if req.HTTP.Body.Type != "json" || req.HTTP.Body.Raw == "" {
		t.Fatalf("正文没转过来：%+v", req.HTTP.Body)
	}
	if req.HTTP.Auth == nil || req.HTTP.Auth.Type != "bearer" || req.HTTP.Auth.Token != "{{token}}" {
		t.Fatalf("认证没转过来：%+v", req.HTTP.Auth)
	}
	if req.Docs != "用户列表。" {
		t.Fatalf("docs 没转过来：%q", req.Docs)
	}
	if summary.Requests != 1 {
		t.Fatalf("统计不对：%+v", summary)
	}
	hasScriptNote := false
	for _, note := range summary.Notes {
		if strings.Contains(note, "脚本") {
			hasScriptNote = true
		}
	}
	if !hasScriptNote {
		t.Fatalf("应提示脚本段没导入：%+v", summary.Notes)
	}
}

// TestBrunoBodyForms 表单与 multipart 的键值行。
func TestBrunoBodyForms(t *testing.T) {
	dir := t.TempDir()
	writeBru(t, dir, "登录.bru", `meta {
  name: 登录
}

post {
  url: {{host}}/login
}

body:form-urlencoded {
  user: neo
  ~remember: 1
}

auth:basic {
  username: neo
  password: secret
}
`)
	items, _, err := Bruno(dir, "")
	if err != nil {
		t.Fatalf("Bruno: %v", err)
	}
	req := items[0].Request
	if req.HTTP.Method != "POST" {
		t.Fatalf("方法不对：%s", req.HTTP.Method)
	}
	if req.HTTP.Body.Type != "form" || len(req.HTTP.Body.Form) != 2 {
		t.Fatalf("表单没转过来：%+v", req.HTTP.Body)
	}
	if req.HTTP.Body.Form[1].Enabled {
		t.Fatalf("禁用行应保持禁用：%+v", req.HTTP.Body.Form[1])
	}
	if req.HTTP.Auth == nil || req.HTTP.Auth.Username != "neo" {
		t.Fatalf("basic 认证没转过来：%+v", req.HTTP.Auth)
	}
}

// TestBrunoRejectsEmptyDir 没有 .bru 的目录要给出人话错误。
func TestBrunoRejectsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Bruno(dir, ""); err == nil {
		t.Fatal("空目录应报错")
	}
	if _, _, err := Bruno("", ""); err == nil {
		t.Fatal("空路径应报错")
	}
}
