package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// writeCollection 造一个有两条请求的集合：一条带字段表（用来生成示例），一条没有。
func writeCollection(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	must("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 桩集合\n")
	must("用户.yml", `info:
  name: 用户详情
  type: http
  seq: 1
meta:
  uid: mock-user
http:
  method: GET
  url: "{{host}}/api/users/1?trace=1"
  headers: []
  body:
    type: json
    fields:
      - name: id
        type: number
        example: "7"
      - name: name
        type: string
        example: 张三
      - name: active
        type: boolean
  params: []
`)
	must("健康检查.yml", `info:
  name: 健康检查
  type: http
  seq: 2
meta:
  uid: mock-health
http:
  method: GET
  url: "http://127.0.0.1:9/health"
  headers: []
  body:
    type: none
`)
	return dir
}

// TestMockServesSampleFromFields 桩按请求文件的方法 + 路径应答，响应体来自字段表。
func TestMockServesSampleFromFields(t *testing.T) {
	server, err := Start("127.0.0.1:0", []string{writeCollection(t)}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Close() }()

	resp, err := http.Get("http://" + server.Addr() + "/api/users/1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Mock") != "api-doc-go" {
		t.Fatalf("应标明这是桩：%v", resp.Header)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if body["name"] != "张三" {
		t.Fatalf("示例值没带过来：%v", body)
	}
	if number, ok := body["id"].(float64); !ok || number != 7 {
		t.Fatalf("数字字段不该加引号：%v", body["id"])
	}
	if active, ok := body["active"].(bool); !ok || active {
		t.Fatalf("布尔字段该是 false：%v", body["active"])
	}

	// 没配字段表的请求也有桩，回最小对象。
	health, err := http.Get("http://" + server.Addr() + "/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = health.Body.Close() }()
	if health.StatusCode != 200 {
		t.Fatalf("最小桩状态码 = %d", health.StatusCode)
	}
}

// TestMockReportsRoutesAndMismatches 路由表可查；路径对而方法不对给 405，都对不上给 404。
func TestMockReportsRoutesAndMismatches(t *testing.T) {
	server, err := Start("127.0.0.1:0", []string{writeCollection(t)}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Close() }()
	base := "http://" + server.Addr()

	resp, err := http.Get(base + "/__mock/routes")
	if err != nil {
		t.Fatalf("GET routes: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var routes []Route
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		t.Fatalf("路由表不是 JSON: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("应有两条桩：%+v", routes)
	}
	if routes[0].Path != "/api/users/1" || routes[0].Method != "GET" {
		t.Fatalf("路径解析不对：%+v", routes[0])
	}

	post, err := http.Post(base+"/api/users/1", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = post.Body.Close() }()
	if post.StatusCode != http.StatusMethodNotAllowed || post.Header.Get("Allow") != "GET" {
		t.Fatalf("方法不对应给 405 + Allow：%d %v", post.StatusCode, post.Header)
	}

	missing, err := http.Get(base + "/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("未注册路径应 404，实际 %d", missing.StatusCode)
	}
}

// TestMockRuleOverridesStatusDelayAndBody 单条规则：状态码、延迟与整段响应体都能按请求覆盖。
func TestMockRuleOverridesStatusDelayAndBody(t *testing.T) {
	dir := t.TempDir()
	must := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	must("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 规则桩\n")
	must("下单.yml", `info:
  name: 下单
  type: http
  seq: 1
meta:
  uid: mock-rule
http:
  method: POST
  url: "{{host}}/api/orders"
  headers: []
  body:
    type: none
mock:
  status: 201
  delayMs: 60
  body: '{"orderId":"A-1"}'
`)

	server, err := Start("127.0.0.1:0", []string{dir}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Close() }()

	routes := server.Routes()
	if len(routes) != 1 || routes[0].Status != 201 || routes[0].DelayMS != 60 {
		t.Fatalf("路由表应带上规则：%+v", routes)
	}

	start := time.Now()
	resp, err := http.Post("http://"+server.Addr()+"/api/orders", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	elapsed := time.Since(start)
	if resp.StatusCode != 201 {
		t.Fatalf("状态码应被规则覆盖成 201，实际 %d", resp.StatusCode)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("延迟没生效：%v", elapsed)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}
	if !strings.Contains(string(body), "A-1") {
		t.Fatalf("响应体应被规则覆盖：%s", body)
	}
}

// TestMockRuleRoundTrip 规则写进 Extra 再读出来一致；清空后键消失。
func TestMockRuleRoundTrip(t *testing.T) {
	file := &collection.RequestFile{}
	ApplyRule(file, Rule{Status: 500, DelayMS: 120, Body: `{"e":true}`})
	rule := RuleOf(file)
	if rule.Status != 500 || rule.DelayMS != 120 || rule.Body != `{"e":true}` {
		t.Fatalf("读回来不对：%+v", rule)
	}
	ApplyRule(file, Rule{})
	if _, ok := file.Extra[RuleKey]; ok {
		t.Fatal("清空后应删掉 mock 键")
	}
	if rule := RuleOf(file); rule.Status != 0 || rule.DelayMS != 0 || rule.Body != "" {
		t.Fatalf("清空后不该还有规则：%+v", rule)
	}
}

// TestPathOfKeepsVariables PathOf 只做文本切分，{{host}} 这种变量不影响路径。
func TestPathOfKeepsVariables(t *testing.T) {
	cases := map[string]string{
		"{{host}}/api/v1/login": "/api/v1/login",
		"https://a.com/x?y=1":   "/x",
		"http://a.com":          "/",
		"http://a.com/":         "/",
		"/already/relative":     "/already/relative",
		"":                      "",
	}
	for input, want := range cases {
		if got := PathOf(input); got != want {
			t.Fatalf("PathOf(%q) = %q，应为 %q", input, got, want)
		}
	}
}

// TestStartReportsListenErrors 端口被占时把错误交回去，而不是静默失败。
func TestStartReportsListenErrors(t *testing.T) {
	first, err := Start("127.0.0.1:0", nil, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = first.Close() }()
	if _, err := Start(first.Addr(), nil, nil); err == nil {
		t.Fatal("同一地址再起一个应当报错")
	}
}

// TestReloadPicksUpNewRequests 集合改过以后 Reload 能认出新桩。
func TestReloadPicksUpNewRequests(t *testing.T) {
	dir := writeCollection(t)
	server, err := Start("127.0.0.1:0", []string{dir}, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = server.Close() }()
	if len(server.Routes()) != 2 {
		t.Fatalf("初始应有两条：%+v", server.Routes())
	}
	if _, err := explorer.CreateRequest(dir, "", "新增", "POST", "https://a/new"); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	server.Reload([]string{dir})
	if len(server.Routes()) != 3 {
		t.Fatalf("Reload 后应有三条：%+v", server.Routes())
	}
}
