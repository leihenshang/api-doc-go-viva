package importers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// newCollection 建一个空集合（有 opencollection.yml），返回目录。
func newCollection(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := explorer.InitCollection(dir, name); err != nil {
		t.Fatalf("InitCollection: %v", err)
	}
	return dir
}

const postmanSample = `{
  "info": {"name": "商城 API", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}"}]},
  "variable": [{"key": "host", "value": "https://api.example.com"}],
  "item": [
    {
      "name": "用户",
      "item": [
        {
          "name": "登录",
          "request": {
            "method": "POST",
            "header": [{"key": "X-Trace", "value": "abc"}, {"key": "X-Off", "value": "1", "disabled": true}],
            "url": {"raw": "https://api.example.com/v1/login?from=web&lang=zh", "query": []},
            "body": {"mode": "urlencoded", "urlencoded": [{"key": "user", "value": "a"}]},
            "description": "账号密码换令牌"
          }
        },
        {
          "name": "详情",
          "request": {
            "method": "GET",
            "header": [],
            "url": {"protocol": "https", "host": ["api", "example", "com"], "path": ["v1", "me"], "query": [{"key": "with", "value": "teams"}]},
            "auth": {"type": "basic", "basic": [{"key": "username", "value": "u"}, {"key": "password", "value": "p"}]}
          }
        }
      ]
    },
    {
      "name": "健康检查",
      "request": {
        "method": "GET",
        "header": [],
        "url": {"raw": "https://api.example.com/health"},
        "body": {"mode": "raw", "raw": "{\"ping\":1}", "options": {"raw": {"language": "json"}}},
        "auth": {"type": "apikey", "apikey": [{"key": "key", "value": "X-Key"}, {"key": "value", "value": "k"}, {"key": "in", "value": "query"}]}
      }
    }
  ]
}`

// TestPostmanImportWritesCollection Postman 导出的集合能落成 OpenCollection：
// 分组照层级、查询参数进 params、认证与 body 跟着过来、集合变量进 Vars。
func TestPostmanImportWritesCollection(t *testing.T) {
	items, summary, err := Postman([]byte(postmanSample))
	if err != nil {
		t.Fatalf("Postman: %v", err)
	}
	if len(summary.Vars) != 1 || summary.Vars[0].Name != "host" {
		t.Fatalf("集合变量应被带出来：%+v", summary.Vars)
	}
	dir := newCollection(t, "导入目标")
	written, err := Write(dir, "", items)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.Folders != 1 || written.Requests != 3 {
		t.Fatalf("应建 1 个分组、3 条请求，实际 %+v（%v）", written, written.Notes)
	}

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(col.Folders) != 1 || col.Folders[0].Name != "用户" {
		t.Fatalf("分组没建对：%+v", col.Folders)
	}
	byName := map[string]*explorer.Entry{}
	for _, entry := range col.Entries {
		byName[entry.Name] = entry
	}
	login := byName["登录"]
	if login == nil || login.Payload == nil {
		t.Fatalf("缺少「登录」：%v", paintedNames(col))
	}
	if login.Payload.HTTP.Method != "POST" || login.Payload.HTTP.URL != "https://api.example.com/v1/login" {
		t.Fatalf("方法或地址不对：%+v", login.Payload.HTTP)
	}
	if len(login.Payload.HTTP.Params) != 2 {
		t.Fatalf("查询参数应拆进 params：%+v", login.Payload.HTTP.Params)
	}
	if login.Payload.HTTP.Body.Type != "form" || len(login.Payload.HTTP.Body.Form) != 1 {
		t.Fatalf("urlencoded 应落成 form：%+v", login.Payload.HTTP.Body)
	}
	// 集合级 bearer 兜底到没写认证的请求。
	if login.Payload.HTTP.Auth == nil || login.Payload.HTTP.Auth.Type != "bearer" ||
		login.Payload.HTTP.Auth.Token != "{{token}}" {
		t.Fatalf("bearer 认证没兜底过来：%+v", login.Payload.HTTP.Auth)
	}
	if login.Payload.Docs != "账号密码换令牌" {
		t.Fatalf("描述应进 docs：%q", login.Payload.Docs)
	}
	if len(login.Payload.HTTP.Headers) != 2 || login.Payload.HTTP.Headers[1].Enabled {
		t.Fatalf("请求头应带 disabled 状态：%+v", login.Payload.HTTP.Headers)
	}

	detail := byName["详情"]
	if detail == nil || detail.Payload.HTTP.URL != "https://api.example.com/v1/me" {
		t.Fatalf("host/path 数组没拼成地址：%+v", detail)
	}
	if detail.Payload.HTTP.Auth == nil || detail.Payload.HTTP.Auth.Type != "basic" {
		t.Fatalf("请求自带认证应优先：%+v", detail.Payload.HTTP.Auth)
	}
	if len(detail.Payload.HTTP.Params) != 1 || detail.Payload.HTTP.Params[0].Name != "with" {
		t.Fatalf("url.query 应进 params：%+v", detail.Payload.HTTP.Params)
	}

	health := byName["健康检查"]
	if health == nil || health.Payload.HTTP.Body.Type != "json" {
		t.Fatalf("raw+json 应落成 json body：%+v", health)
	}
	if health.Payload.HTTP.Auth == nil || health.Payload.HTTP.Auth.In != "query" {
		t.Fatalf("apikey 位置没带过来：%+v", health.Payload.HTTP.Auth)
	}
}

// TestPostmanUnsupportedPartsAreNoted 不支持的东西跳过并留提示，不拖垮整次导入。
func TestPostmanUnsupportedPartsAreNoted(t *testing.T) {
	sample := `{"info":{"name":"x"},"item":[
	  {"name":"图查询","request":{"method":"POST","url":"https://a/b","body":{"mode":"graphql"}}},
	  {"name":"空文件夹","item":[]}
	]}`
	items, summary, err := Postman([]byte(sample))
	if err != nil {
		t.Fatalf("Postman: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应只剩一条请求：%+v", items)
	}
	joined := strings.Join(summary.Notes, "；")
	if !strings.Contains(joined, "graphql") || !strings.Contains(joined, "空文件夹") {
		t.Fatalf("提示不完整：%v", summary.Notes)
	}
}

// TestOpenCollectionCopiesTree 另一个 OpenCollection 目录能整棵搬过来：
// 分组照层级、请求新 uid、同名加 -2，源集合不受影响。
func TestOpenCollectionCopiesTree(t *testing.T) {
	src := newCollection(t, "源集合")
	if _, err := explorer.CreateFolder(src, "", "订单"); err != nil {
		t.Fatalf("建分组: %v", err)
	}
	if _, err := explorer.CreateRequest(src, "订单", "下单", "POST", "https://a/order"); err != nil {
		t.Fatalf("建请求: %v", err)
	}
	if _, err := explorer.CreateDoc(src, "接口约定"); err != nil {
		t.Fatalf("建文档: %v", err)
	}
	dst := newCollection(t, "目标集合")
	// 先放一条同名的请求，导入时应当加 -2 而不是覆盖。
	if _, err := explorer.CreateRequest(dst, "", "下单", "GET", "https://b/other"); err != nil {
		t.Fatalf("预置请求: %v", err)
	}

	summary, err := OpenCollection(dst, "", src)
	if err != nil {
		t.Fatalf("OpenCollection: %v", err)
	}
	if summary.Folders != 1 || summary.Requests != 1 || summary.Docs != 1 {
		t.Fatalf("导入条数不对：%+v", summary)
	}
	col, err := explorer.Load(dst)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var files []string
	for _, entry := range col.Entries {
		files = append(files, entry.File)
	}
	joined := strings.Join(files, ",")
	if !strings.Contains(joined, "订单/下单.yml") {
		t.Fatalf("请求应落在同名分组里：%v", files)
	}
	if !strings.Contains(joined, "docs/接口约定.md") {
		t.Fatalf("文档没导进来：%v", files)
	}
	// 导入过来的请求带的是源里的地址，而不是预置那条。
	for _, entry := range col.Entries {
		if entry.File == "订单/下单.yml" && (entry.Payload == nil || entry.Payload.HTTP.URL != "https://a/order") {
			t.Fatalf("导入内容不对：%+v", entry.Payload)
		}
	}

	// 再导一次：同名文件加 -2，绝不覆盖上一次的结果。
	if _, err := OpenCollection(dst, "", src); err != nil {
		t.Fatalf("第二次导入: %v", err)
	}
	again, err := explorer.Load(dst)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 同名分组会建出「订单-2」，请求跟着进去；关键是原文件一个没被覆盖。
	ordered := 0
	for _, entry := range again.Entries {
		if strings.HasSuffix(entry.File, "下单.yml") {
			ordered++
		}
	}
	if ordered != 3 { // 预置 1 条 + 两次导入各 1 条
		var files []string
		for _, entry := range again.Entries {
			files = append(files, entry.File)
		}
		t.Fatalf("两次导入应各自新建文件：%v", files)
	}
	// 源集合原样还在。
	before, err := explorer.Load(src)
	if err != nil || len(before.Entries) != 2 {
		t.Fatalf("源集合被改动了：%v %v", err, before)
	}
}

// request 造一条最小请求（测试里就地拼条目用）。
func request(method, target string) *collection.RequestFile {
	return &collection.RequestFile{
		Info: collection.FileInfo{Name: "深层请求", Type: "http"},
		HTTP: collection.HTTPBlock{Method: method, URL: target},
	}
}

// paintedNames 是断言失败时给出条目名，便于定位。
func paintedNames(col *explorer.Collection) []string {
	names := make([]string, 0, len(col.Entries))
	for _, entry := range col.Entries {
		names = append(names, entry.Name)
	}
	return names
}

// TestPostmanRejectsOtherJSON 不是 Postman 集合的 JSON 要报错，而不是导出一堆空请求。
func TestPostmanRejectsOtherJSON(t *testing.T) {
	if _, _, err := Postman([]byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("应当报「没有可导入的请求」")
	}
	if _, _, err := Postman([]byte(`not json`)); err == nil {
		t.Fatal("应当报解析失败")
	}
}

// TestWriteKeepsFolderHierarchy 两层分组要按层级建出来。
func TestWriteKeepsFolderHierarchy(t *testing.T) {
	dir := newCollection(t, "多层")
	items := []Item{{
		Name: "外层",
		Items: []Item{{
			Name:  "内层",
			Items: []Item{{Name: "深层请求", Request: request("GET", "https://a/deep")}},
		}},
	}}
	summary, err := Write(dir, "", items)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if summary.Folders != 2 || summary.Requests != 1 {
		t.Fatalf("条数不对：%+v", summary)
	}
	if _, err := os.Stat(filepath.Join(dir, "外层", "内层", "深层请求.yml")); err != nil {
		t.Fatalf("文件没落在「外层/内层」：%v", err)
	}
}
