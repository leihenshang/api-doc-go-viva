package view

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// writeScriptCollection 造一条把请求头 / 查询串原样回显的请求，用来验证
// 变量、脚本写入的变量真的影响发出去的报文。
func writeScriptCollection(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 脚本集合\n")
	request := fmt.Sprintf(`info:
  name: 回显
  type: http
  seq: 1
meta:
  uid: viva-script-e2e
http:
  method: GET
  url: %q
  headers:
    - name: X-Who
      value: "{{who}}"
      enabled: true
  body:
    type: none
`, url)
	writeFile(t, filepath.Join(dir, "回显.yml"), request)
	return dir
}

// echoServer 回显请求头与查询串，便于断言「变量到底有没有生效」。
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"header": r.Header.Get("X-Who"),
			"query":  r.URL.RawQuery,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 「变量」分段：加一行 → 发送时覆盖 {{who}} → 保存进文件的 vars 段。
func TestVarsTabFeedsSendAndSaves(t *testing.T) {
	srv := echoServer(t)
	col, err := explorer.Load(writeScriptCollection(t, srv.URL+"/echo"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 回显")
	pump(t, driver)

	click(t, driver, "变量")
	pump(t, driver)
	if !shows(driver, "请求级变量") {
		t.Fatalf("变量分段应有说明：%v", painted(driver))
	}
	click(t, driver, "添加变量")
	pump(t, driver)
	clickText(t, driver, "traceId")
	pump(t, driver)
	if err := driver.Type("who"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	clickText(t, driver, "{{$uuid}}")
	pump(t, driver)
	if err := driver.Type("viva"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)

	if len(model.req.vars) != 1 || model.req.vars[0].name != "who" || model.req.vars[0].value != "viva" {
		t.Fatalf("模型里的变量 = %+v", model.req.vars)
	}

	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if !strings.Contains(model.resp.Body, `"header":"viva"`) {
		t.Fatalf("请求头里的变量没生效：%s", model.resp.Body)
	}

	// 保存：变量要落进请求文件（Wails 同款 vars.pre-request 段）。
	model.saveRequest()
	raw, err := os.ReadFile(filepath.Join(col.Dir, "回显.yml"))
	if err != nil {
		t.Fatalf("读请求文件: %v", err)
	}
	text := string(raw)
	for _, want := range []string{"vars:", "pre-request:", "who", "viva"} {
		if !strings.Contains(text, want) {
			t.Fatalf("文件里少了 %q：\n%s", want, text)
		}
	}
}

// 「断言」分段：发送后逐条判定，响应区显示通过数，失败的给出表达式。
func TestAssertsTabRunsOnSend(t *testing.T) {
	srv := echoServer(t)
	col, err := explorer.Load(writeScriptCollection(t, srv.URL+"/echo"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 回显")
	pump(t, driver)

	click(t, driver, "断言")
	pump(t, driver)
	// 一键示例：状态码 2xx + 内容是 JSON，两条都应通过。
	click(t, driver, "填充示例断言")
	pump(t, driver)
	if len(model.req.asserts) != 2 {
		t.Fatalf("示例断言条数 = %d", len(model.req.asserts))
	}

	// 再手动加一条必然失败的：验证失败明细会画出来。
	click(t, driver, "添加断言")
	pump(t, driver)
	clickText(t, driver, "状态码为 200")
	pump(t, driver)
	if err := driver.Type("必失败"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	clickText(t, driver, "res.status === 200")
	pump(t, driver)
	if err := driver.Type("res.status === 500"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)

	click(t, driver, "发送")
	waitFor(t, driver, "2/3 通过")
	if len(model.assertOut) != 3 {
		t.Fatalf("断言结果条数 = %d：%+v", len(model.assertOut), model.assertOut)
	}
	if model.assertOut[2].Passed {
		t.Fatalf("最后一条应失败：%+v", model.assertOut[2])
	}
	if !shows(driver, "res.status === 500") {
		t.Fatalf("失败明细应画出表达式：%v", painted(driver))
	}
}

// 「脚本」分段：前置脚本 bru.setVar 写进的变量能用在地址里。
func TestScriptTabPreRequestSetsVar(t *testing.T) {
	srv := echoServer(t)
	col, err := explorer.Load(writeScriptCollection(t, srv.URL+"/echo?tag={{tag}}"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 回显")
	pump(t, driver)

	click(t, driver, "脚本")
	pump(t, driver)
	clickText(t, driver, "bru.setVar('ts', Date.now())")
	pump(t, driver)
	if err := driver.Type("bru.setVar('tag', 'from-script')"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if model.req.scriptPre == "" {
		t.Fatalf("脚本没写进模型：%v", painted(driver))
	}

	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if model.scriptErr != "" {
		t.Fatalf("脚本报错：%s", model.scriptErr)
	}
	if !strings.Contains(model.resp.Body, "from-script") {
		t.Fatalf("脚本变量没进 URL：%s", model.resp.Body)
	}
}

// 「响应字段」分段：从当前响应生成字段表 → 界面出字段名 → 保存落 respdoc 段。
func TestRespFieldsTabGeneratesFromResponse(t *testing.T) {
	srv := echoServer(t)
	col, err := explorer.Load(writeScriptCollection(t, srv.URL+"/echo"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 回显")
	pump(t, driver)

	click(t, driver, "发送")
	waitFor(t, driver, "200")

	click(t, driver, "响应字段")
	pump(t, driver)
	if !shows(driver, "字段表描述这条接口的返回结构") {
		t.Fatalf("响应字段分段应有说明：%v", painted(driver))
	}
	click(t, driver, "从当前响应生成")
	pump(t, driver)

	if len(model.req.resp.fields) == 0 {
		t.Fatalf("没生成字段：%v", painted(driver))
	}
	names := make([]string, 0, len(model.req.resp.fields))
	for _, row := range model.req.resp.fields {
		names = append(names, row.name)
	}
	if !strings.Contains(strings.Join(names, ","), "header") {
		t.Fatalf("字段名里应有 header：%v", names)
	}
	if model.req.resp.status != 200 || model.req.resp.ctype != "json" {
		t.Fatalf("响应快照 = %+v", model.req.resp)
	}

	model.saveRequest()
	raw, err := os.ReadFile(filepath.Join(col.Dir, "回显.yml"))
	if err != nil {
		t.Fatalf("读请求文件: %v", err)
	}
	text := string(raw)
	for _, want := range []string{"respdoc:", "fieldName:", "status: 200"} {
		if !strings.Contains(text, want) {
			t.Fatalf("文件里少了 %q：\n%s", want, text)
		}
	}
	if !shows(driver, "header") {
		t.Fatalf("字段名应画到界面上：%v", painted(driver))
	}
}
