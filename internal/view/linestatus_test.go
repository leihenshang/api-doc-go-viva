package view

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
)

// bodyBox 找响应正文的命中区（状态栏行号要靠它定位点击点）。
func bodyBox(t *testing.T, driver *app.Driver) (x, y, w, h float32) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "响应正文" {
			return hit.Bounds.X, hit.Bounds.Y, hit.Bounds.Width, hit.Bounds.Height
		}
	}
	t.Fatalf("找不到响应正文区域：%v", painted(driver))
	return 0, 0, 0, 0
}

// TestLineStatusFollowsCaret 响应正文的行号在状态栏：点第 2 行就显示「行 2」，总行数是整篇。
func TestLineStatusFollowsCaret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"a":1,"b":{"c":2}}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	// 格式化后是 6 行：{ / "a": 1, / "b": { / "c": 2 / } / } —— 按 selLineH=18 数行。
	if !shows(driver, "行 1 列 1") {
		t.Fatalf("发送后应显示行号：%v", painted(driver))
	}
	if !shows(driver, "共 6 行") {
		t.Fatalf("总行数应为 6：%v", painted(driver))
	}

	x, y, _, _ := bodyBox(t, driver)
	// 点第二行（可见行 index 1 → 原始 JSON 第 2 行）。
	if err := driver.Click(x+20, y+1.5*18); err != nil {
		t.Fatalf("Click: %v", err)
	}
	pump(t, driver)
	if model.jsonSel.head.line != 1 {
		t.Fatalf("光标应在第 2 个可见行：%+v", model.jsonSel.head)
	}
	if !shows(driver, "行 2 ") {
		t.Fatalf("点第二行后应显示「行 2」：%v", painted(driver))
	}
}

// TestLineStatusRawWrapNotNumbered 原始视图折行不单独编号：一条长行折成多行后仍是「共 1 行」。
func TestLineStatusRawWrapNotNumbered(t *testing.T) {
	long := strings.Repeat("a", 400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, `{"long":"`+long+`"}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	// 切到「原始」视图（不格式化，按原样折行）。
	clickText(t, driver, "原始")
	pump(t, driver)
	if !model.rawResp {
		t.Fatalf("应切到原始视图：%v", painted(driver))
	}
	if len(model.jsonSel.vis) < 2 {
		t.Fatalf("长行应被折成多行：%d", len(model.jsonSel.vis))
	}
	if !shows(driver, "共 1 行") {
		t.Fatalf("折行不该被记成多行：%v", painted(driver))
	}

	// 点折出来的第三行，逻辑行号仍是 1。
	x, y, _, _ := bodyBox(t, driver)
	if err := driver.Click(x+20, y+2.5*18); err != nil {
		t.Fatalf("Click: %v", err)
	}
	pump(t, driver)
	if !shows(driver, "行 1 ") {
		t.Fatalf("折行续行应记回上一行：%v", painted(driver))
	}
}

// TestLineStatusDocPreview 文档预览同样给出行数（只读视图没有光标）。
func TestLineStatusDocPreview(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	click(t, driver, "文档")
	pump(t, driver)
	// writeCollection 的 docs 是三行：标题 / 空行 / 段落。
	if !shows(driver, "文档 3 行") {
		t.Fatalf("文档预览应显示行数：%v", painted(driver))
	}
}
