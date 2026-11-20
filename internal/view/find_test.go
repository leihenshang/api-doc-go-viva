package view

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
)

// TestFindMatches 命中按行返回、忽略大小写、同一行可有多处。
func TestFindMatches(t *testing.T) {
	lines := []string{
		`  "id": 1,`,
		`  "name": "neo",`,
		`  "note": "neo and NEO",`,
	}
	hits := findMatches(lines, "neo")
	if len(hits) != 3 {
		t.Fatalf("应有 3 处命中，实际 %d: %+v", len(hits), hits)
	}
	if hits[0].line != 1 || hits[1].line != 2 || hits[2].line != 2 {
		t.Fatalf("命中行不对：%+v", hits)
	}
	if hits[1].start != 11 || hits[1].end != 14 {
		t.Fatalf("列位置不对：%+v", hits[1])
	}
	if got := findMatches(lines, "  "); got != nil {
		t.Fatalf("纯空白查询会被裁掉，不该有命中：%+v", got)
	}
	if got := findMatches(lines, ""); got != nil {
		t.Fatalf("空查询不该有命中：%+v", got)
	}
}

// TestFindMatchesChinese 中文按 rune 计列，双宽字符不能按字节算。
func TestFindMatchesChinese(t *testing.T) {
	hits := findMatches([]string{`  "名字": "张三丰",`}, "张三")
	if len(hits) != 1 || hits[0].start != 9 {
		t.Fatalf("中文命中位置不对：%+v", hits)
	}
}

// TestFindInResponse Ctrl+F 打开查找条 → 输入命中 → 计数出现 → 下一处 → 关闭。
func TestFindInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"items":[{"ok":1},{"ok":2}]}`)
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

	if err := driver.Chord(gfx.ModifierControl, "f"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if !model.find.open {
		t.Fatalf("Ctrl+F 应打开查找条：%v", painted(driver))
	}

	click(t, driver, "在响应里查找")
	pump(t, driver)
	if err := driver.Type("ok"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if len(model.find.hits) != 3 {
		t.Fatalf("应命中 3 处（ok / 两个 items 的 ok），实际 %d: %+v", len(model.find.hits), model.find.hits)
	}
	if !shows(driver, "1/3") {
		t.Fatalf("计数应显示 1/3：%v", painted(driver))
	}

	click(t, driver, "下一处")
	pump(t, driver)
	if model.find.index != 1 {
		t.Fatalf("下一处应到第 2 个，实际 %d", model.find.index)
	}
	if !shows(driver, "2/3") {
		t.Fatalf("计数应变成 2/3：%v", painted(driver))
	}

	click(t, driver, "关闭查找")
	pump(t, driver)
	if model.find.open || len(model.find.hits) != 0 {
		t.Fatal("关闭后应清空")
	}
}
