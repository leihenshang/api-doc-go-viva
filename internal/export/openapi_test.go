package export

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/importers"
)

// TestOpenAPIRoundTrip 导出成 OpenAPI 后再用本仓库的导入器读回：路径 / 方法 / 参数都在。
func TestOpenAPIRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", rel, err)
		}
	}
	write("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 导出集合\n")
	write("用户/列表.yml", `info:
  name: 列表
  type: http
  seq: 1
meta:
  uid: exp-1
http:
  method: GET
  url: "{{host}}/api/users"
  headers:
    - name: X-Trace
      value: "1"
      enabled: true
  params:
    - name: page
      value: "1"
      enabled: true
  body:
    type: none
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := OpenAPI(col, nil)
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	items, summary, err := importers.OpenAPI(data, "")
	if err != nil {
		t.Fatalf("导入 OpenAPI: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应导入 1 条请求：items=%d summary=%+v", len(items), summary)
	}
	var flat []importers.Item
	var walk func(list []importers.Item)
	walk = func(list []importers.Item) {
		for _, item := range list {
			if item.Request != nil {
				flat = append(flat, item)
			}
			walk(item.Items)
		}
	}
	walk(items)
	if len(flat) != 1 {
		t.Fatalf("拍平后应 1 条：%d", len(flat))
	}
	req := flat[0].Request
	if req.HTTP.Method != "GET" || req.HTTP.URL != "/api/users" {
		t.Fatalf("路径或方法没还原：%s %s", req.HTTP.Method, req.HTTP.URL)
	}
	var hasPage, hasTrace bool
	for _, kv := range req.HTTP.Params {
		if kv.Name == "page" {
			hasPage = true
		}
	}
	for _, kv := range req.HTTP.Headers {
		if kv.Name == "X-Trace" {
			hasTrace = true
		}
	}
	if !hasPage || !hasTrace {
		t.Fatalf("参数或请求头丢了：params=%+v headers=%+v", req.HTTP.Params, req.HTTP.Headers)
	}
}

// TestPathOf 各种写法的地址都能取出路径。
func TestPathOf(t *testing.T) {
	cases := map[string]string{
		"{{host}}/api/users":          "/api/users",
		"https://x.dev/api/users?a=1": "/api/users",
		"/api/users?page=1":           "/api/users",
		"{{base}}":                    "/",
		"api/users":                   "/api/users",
	}
	for raw, want := range cases {
		if got := pathOf(raw); got != want {
			t.Errorf("%q → %q，期望 %q", raw, got, want)
		}
	}
}
