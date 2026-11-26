package importers

import (
	"strings"
	"testing"
)

// postmanV1Doc 是 Postman v1 的典型导出：requests / folders 平铺 + order 定顺序，
// 请求头可以是一整段文本，body 用 dataMode 表示。
const postmanV1Doc = `{
  "id": "c1",
  "name": "v1 集合",
  "description": "老版导出",
  "order": ["req1", "f1"],
  "folders": [{"id": "f1", "name": "用户", "order": ["req2"], "collectionId": "c1"}],
  "requests": [
    {
      "id": "req1", "name": "健康", "method": "GET",
      "url": "http://example.invalid/health?probe=1",
      "headers": "X-Trace: abc\nX-Team: core",
      "dataMode": "raw", "rawModeData": "{\"a\":1}"
    },
    {
      "id": "req2", "name": "登录", "method": "POST",
      "url": "http://example.invalid/login",
      "headers": [{"key": "Content-Type", "value": "application/json", "enabled": true}],
      "dataMode": "urlencoded",
      "data": [{"key": "user", "value": "bob", "enabled": true}]
    }
  ],
  "variables": [{"key": "host", "value": "http://example.invalid"}]
}`

func TestPostmanV1Import(t *testing.T) {
	items, summary, err := Postman([]byte(postmanV1Doc))
	if err != nil {
		t.Fatalf("Postman v1: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("顶层应有一条请求 + 一个分组：%+v", items)
	}
	// order 决定顺序：先 req1，再 f1。
	if items[0].Request == nil || items[0].Name != "健康" {
		t.Fatalf("第一条应是「健康」：%+v", items[0])
	}
	health := items[0].Request
	if health.HTTP.Method != "GET" || health.HTTP.URL != "http://example.invalid/health" {
		t.Fatalf("地址里的查询串应拆进 params：%+v", health.HTTP)
	}
	if len(health.HTTP.Params) != 1 || health.HTTP.Params[0].Name != "probe" {
		t.Fatalf("查询参数不对：%+v", health.HTTP.Params)
	}
	// v1 的请求头可能是整段文本。
	if len(health.HTTP.Headers) != 2 || health.HTTP.Headers[0].Name != "X-Trace" ||
		health.HTTP.Headers[1].Value != "core" {
		t.Fatalf("文本形式的请求头没解析：%+v", health.HTTP.Headers)
	}
	if health.HTTP.Body.Type != "json" || health.HTTP.Body.Raw != `{"a":1}` {
		t.Fatalf("raw body 应记成 json：%+v", health.HTTP.Body)
	}

	if items[1].Request != nil || items[1].Name != "用户" || len(items[1].Items) != 1 {
		t.Fatalf("第二条应是分组「用户」：%+v", items[1])
	}
	login := items[1].Items[0].Request
	if login == nil || login.HTTP.Method != "POST" || len(login.HTTP.Body.Form) != 1 {
		t.Fatalf("urlencoded body 没转成 form：%+v", login)
	}
	if login.HTTP.Body.Form[0].Name != "user" || login.HTTP.Body.Form[0].Value != "bob" {
		t.Fatalf("form 行不对：%+v", login.HTTP.Body.Form)
	}

	if len(summary.Vars) != 1 || summary.Vars[0].Name != "host" {
		t.Fatalf("v1 的 variables 应转成环境变量：%+v", summary.Vars)
	}
}

// 没有 order 的导出（或 order 只写了一半）也要把所有条目导进来。
func TestPostmanV1WithoutOrder(t *testing.T) {
	doc := `{
  "name": "无 order",
  "folders": [{"id": "f1", "name": "分组"}],
  "requests": [
    {"id": "r1", "name": "甲", "method": "GET", "url": "http://example.invalid/a"},
    {"id": "r2", "name": "乙", "method": "GET", "url": "http://example.invalid/b"}
  ]
}`
	items, _, err := Postman([]byte(doc))
	if err != nil {
		t.Fatalf("Postman v1: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("三条（一个分组 + 两条请求）都应导入：%+v", items)
	}
	names := []string{items[0].Name, items[1].Name, items[2].Name}
	if strings.Join(names, ",") != "分组,甲,乙" {
		t.Fatalf("顺序/内容不对：%v", names)
	}
}

// Insomnia 导出：request_group + request、{{ _.var }} 语法、环境变量、认证。
const insomniaDoc = `{
  "_type": "export",
  "__export_format": 4,
  "resources": [
    {"_id": "wrk_1", "_type": "workspace", "name": "我的工作区"},
    {"_id": "env_base", "_type": "environment", "parentId": "wrk_1", "name": "Base Environment",
     "data": {"host": "http://example.invalid", "retry": 2, "nested": {"a": 1}}},
    {"_id": "fld_1", "_type": "request_group", "parentId": "wrk_1", "name": "用户"},
    {"_id": "req_1", "_type": "request", "parentId": "fld_1", "name": "登录", "method": "POST",
     "url": "{{ _.host }}/login",
     "headers": [{"name": "Content-Type", "value": "application/json"}],
     "parameters": [{"name": "with", "value": "teams"}],
     "body": {"mimeType": "application/json", "text": "{\"user\":\"{{ _.user }}\"}"},
     "authentication": {"type": "bearer", "token": "t-1"}},
    {"_id": "req_2", "_type": "request", "parentId": "wrk_1", "name": "健康", "method": "GET",
     "url": "http://example.invalid/health"}
  ]
}`

func TestInsomniaImport(t *testing.T) {
	items, summary, err := Insomnia([]byte(insomniaDoc))
	if err != nil {
		t.Fatalf("Insomnia: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("顶层应是一个分组 + 一条请求：%+v", items)
	}
	if items[0].Name != "用户" || len(items[0].Items) != 1 {
		t.Fatalf("分组结构不对：%+v", items[0])
	}
	login := items[0].Items[0].Request
	if login == nil {
		t.Fatalf("分组里没有请求：%+v", items[0].Items)
	}
	if login.HTTP.Method != "POST" || login.HTTP.URL != "{{host}}/login" {
		t.Fatalf("{{ _.host }} 应转成 {{host}}：%+v", login.HTTP)
	}
	if len(login.HTTP.Params) != 1 || login.HTTP.Params[0].Name != "with" {
		t.Fatalf("parameters 应进 params：%+v", login.HTTP.Params)
	}
	if login.HTTP.Body.Type != "json" || login.HTTP.Body.Raw != `{"user":"{{user}}"}` {
		t.Fatalf("body 变量没转换：%+v", login.HTTP.Body)
	}
	if login.HTTP.Auth == nil || login.HTTP.Auth.Type != "bearer" || login.HTTP.Auth.Token != "t-1" {
		t.Fatalf("认证不对：%+v", login.HTTP.Auth)
	}
	if len(items[0].Items[0].Request.HTTP.Headers) != 1 {
		t.Fatalf("请求头没导入：%+v", login.HTTP.Headers)
	}

	if len(summary.Vars) != 2 {
		t.Fatalf("环境变量应只收标量（host、retry）：%+v", summary.Vars)
	}
	values := map[string]string{}
	for _, variable := range summary.Vars {
		values[variable.Name] = variable.Value
	}
	if values["host"] != "http://example.invalid" || values["retry"] != "2" {
		t.Fatalf("变量值不对：%+v", values)
	}
}

// 非 Insomnia 的 JSON（比如 Postman 集合）交给 Insomnia 解析要给出可操作的提示。
func TestInsomniaRejectsOtherJSON(t *testing.T) {
	if _, _, err := Insomnia([]byte(`{"info":{"name":"x"},"item":[]}`)); err == nil ||
		!strings.Contains(err.Error(), "resources") {
		t.Fatalf("应提示缺少 resources：%v", err)
	}
}

// 地址里的 {{变量}} 不能被当成 URL 转义：拆查询串时只切 ?，不重建地址。
func TestImportKeepsPlaceholdersInURL(t *testing.T) {
	doc := `{
  "name": "占位符集合",
  "order": ["r1"],
  "requests": [{
    "id": "r1", "name": "查用户", "method": "GET",
    "url": "{{host}}/users?with={{with}}&page=2"
  }]
}`
	items, _, err := Postman([]byte(doc))
	if err != nil {
		t.Fatalf("Postman v1: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("条目数：%+v", items)
	}
	http := items[0].Request.HTTP
	if http.URL != "{{host}}/users" {
		t.Fatalf("地址里的占位符被转义了：%q", http.URL)
	}
	if len(http.Params) != 2 || http.Params[0].Name != "with" || http.Params[0].Value != "{{with}}" {
		t.Fatalf("查询参数应按原顺序保留：%+v", http.Params)
	}
}
