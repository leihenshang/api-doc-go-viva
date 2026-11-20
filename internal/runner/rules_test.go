package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// TestRulesRoundTrip 规则写进 Extra 再读出来要一模一样；清空后键要消失。
func TestRulesRoundTrip(t *testing.T) {
	file := &collection.RequestFile{}
	rules := Rules{
		Extract: []Extract{{Name: "token", From: "json", Path: "$.data.token"}},
		Assert:  []Assert{{Kind: "status", Op: "equals", Want: "200"}},
	}
	Apply(file, rules)
	if file.Extra[Key] == nil {
		t.Fatal("规则没写进 Extra")
	}
	got, err := RulesOf(file)
	if err != nil {
		t.Fatalf("RulesOf: %v", err)
	}
	if len(got.Extract) != 1 || got.Extract[0].Name != "token" || got.Extract[0].Path != "$.data.token" {
		t.Fatalf("提取规则读回来不对：%+v", got.Extract)
	}
	if len(got.Assert) != 1 || got.Assert[0].Kind != "status" || got.Assert[0].Want != "200" {
		t.Fatalf("断言规则读回来不对：%+v", got.Assert)
	}

	Apply(file, Rules{})
	if _, ok := file.Extra[Key]; ok {
		t.Fatal("清空规则后应删掉这个键")
	}
	if rules, _ := RulesOf(file); len(rules.Extract)+len(rules.Assert) != 0 {
		t.Fatal("清空后不该还有规则")
	}
}

// TestJSONLookup JSON 路径支持对象、数组下标与标量转换。
func TestJSONLookup(t *testing.T) {
	body := `{"data":{"token":"abc","items":[{"id":7},{"id":9}],"ok":true}}`
	cases := map[string]string{
		"$.data.token":       "abc",
		"$.data.items[1].id": "9",
		"$.data.ok":          "true",
		"data.items[0].id":   "7",
	}
	for path, want := range cases {
		got, ok := jsonLookup(body, path)
		if !ok || got != want {
			t.Fatalf("jsonLookup(%q) = %q,%v，应为 %q", path, got, ok, want)
		}
	}
	if _, ok := jsonLookup(body, "$.data.missing"); ok {
		t.Fatal("不存在的路径应当返回 false")
	}
	if _, ok := jsonLookup("不是 JSON", "$.a"); ok {
		t.Fatal("非 JSON 应当返回 false")
	}
}

// TestEvaluateExtractAndAssert 提取三种来源，断言四种方式，失败文案要能看懂。
func TestEvaluateExtractAndAssert(t *testing.T) {
	observed := Observation{
		Status:   201,
		Body:     `{"code":0,"data":{"token":"t-1"}}`,
		Headers:  []send.Header{{Name: "X-Trace-Id", Value: "trace-9"}},
		Duration: 120 * time.Millisecond,
	}
	rules := Rules{
		Extract: []Extract{
			{Name: "token", From: "json", Path: "$.data.token"},
			{Name: "trace", From: "header", Path: "x-trace-id"},
			{Name: "status", From: "status"},
		},
		Assert: []Assert{
			{Kind: "status", Op: "equals", Want: "201"},
			{Kind: "json", Path: "$.code", Op: "equals", Want: "0"},
			{Kind: "header", Path: "X-Trace-Id", Op: "contains", Want: "trace"},
			{Kind: "time", Op: "lt", Want: "500"},
			{Kind: "json", Path: "$.data.token", Op: "exists"},
		},
	}
	vars, failures := Evaluate(rules, observed)
	if len(failures) != 0 {
		t.Fatalf("不该有失败：%v", failures)
	}
	if vars["token"] != "t-1" || vars["trace"] != "trace-9" || vars["status"] != "201" {
		t.Fatalf("提取结果不对：%+v", vars)
	}

	bad := Rules{Assert: []Assert{
		{Kind: "status", Op: "equals", Want: "200"},
		{Kind: "json", Path: "$.code", Op: "equals", Want: "1"},
		{Kind: "time", Op: "lt", Want: "50"},
		{Kind: "header", Path: "X-None", Op: "exists"},
		{Kind: "json", Path: "$.code", Op: "wat"},
	}}
	_, failures = Evaluate(bad, observed)
	if len(failures) != 5 {
		t.Fatalf("应有 5 条失败，实际 %d：%v", len(failures), failures)
	}
	joined := strings.Join(failures, "\n")
	for _, want := range []string{"期望 \"200\"，实际 \"201\"", "期望 \"1\"，实际 \"0\"",
		"期望小于 \"50\"", "实际没找到", "不认识的比较方式"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("失败文案缺 %q：\n%s", want, joined)
		}
	}
}

// TestRunCarriesExtractedVars 第一条提到 token，第二条带着它请求——变量要跨请求传递。
func TestRunCarriesExtractedVars(t *testing.T) {
	const token = "tok-42"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = fmt.Fprintf(w, `{"data":{"token":%q}}`, token)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	login := Case{
		Name: "登录", File: "登录.yml",
		Plan:  send.Plan{File: requestFile("POST", srv.URL+"/login")},
		Rules: Rules{Extract: []Extract{{Name: "token", From: "json", Path: "$.data.token"}}},
	}
	meFile := requestFile("GET", srv.URL+"/me")
	meFile.HTTP.Headers = []collection.KV{
		{Name: "Authorization", Value: "Bearer {{token}}", Enabled: true},
	}
	me := Case{
		Name: "我的信息", File: "我的信息.yml",
		Plan:  send.Plan{File: meFile, Vars: map[string]string{"token": "旧值"}},
		Rules: Rules{Assert: []Assert{{Kind: "status", Op: "equals", Want: "200"}}},
	}
	results := Run(context.Background(), []Case{login, me}, Options{
		Vars: map[string]string{"host": "http://example.invalid"},
	}, nil)
	if len(results) != 2 {
		t.Fatalf("应跑两条：%+v", results)
	}
	if results[0].Extracted["token"] != token {
		t.Fatalf("第一条没提到 token：%+v", results[0])
	}
	if results[1].Status != 200 || len(results[1].Failures) != 0 {
		t.Fatalf("第二条应带上新 token 并成功：%+v", results[1])
	}
	if results[1].Failed {
		t.Fatalf("第二条不该判失败：%+v", results[1])
	}
}

// TestRunFailsOnAssertion 断言不过也算失败，并且受「失败即停」约束。
func TestRunFailsOnAssertion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1}`))
	}))
	defer srv.Close()

	cases := []Case{
		{Name: "一", Plan: send.Plan{File: requestFile("GET", srv.URL+"/a")},
			Rules: Rules{Assert: []Assert{{Kind: "json", Path: "$.code", Op: "equals", Want: "0"}}}},
		{Name: "二", Plan: send.Plan{File: requestFile("GET", srv.URL+"/b")}},
	}
	results := Run(context.Background(), cases, Options{StopOnFailure: true}, nil)
	if len(results) != 1 {
		t.Fatalf("断言失败应当停下：%+v", results)
	}
	if !results[0].Failed || len(results[0].Failures) != 1 {
		t.Fatalf("应记一条断言失败：%+v", results[0])
	}
	if results[0].Status != 200 {
		t.Fatalf("状态码本身是 200，失败来自断言：%+v", results[0])
	}
}

// requestFile 造一条最小请求。
func requestFile(method, target string) *collection.RequestFile {
	return &collection.RequestFile{
		Info: collection.FileInfo{Name: "用例", Type: "http"},
		HTTP: collection.HTTPBlock{Method: method, URL: target},
	}
}
