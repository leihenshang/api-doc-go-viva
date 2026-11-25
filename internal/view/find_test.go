package view

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/find"
)

// TestFindMatches 命中按行返回、忽略大小写、同一行可有多处。
func TestFindMatches(t *testing.T) {
	lines := []string{
		`  "id": 1,`,
		`  "name": "neo",`,
		`  "note": "neo and NEO",`,
	}
	hits, err := findMatches(lines, "neo", find.Options{})
	if err != nil {
		t.Fatalf("findMatches: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("应有 3 处命中，实际 %d: %+v", len(hits), hits)
	}
	if hits[0].line != 1 || hits[1].line != 2 || hits[2].line != 2 {
		t.Fatalf("命中行不对：%+v", hits)
	}
	if hits[1].start != 11 || hits[1].end != 14 {
		t.Fatalf("列位置不对：%+v", hits[1])
	}
	if got, _ := findMatches(lines, "  ", find.Options{}); got != nil {
		t.Fatalf("纯空白查询会被裁掉，不该有命中：%+v", got)
	}
	if got, _ := findMatches(lines, "", find.Options{}); got != nil {
		t.Fatalf("空查询不该有命中：%+v", got)
	}
}

// TestFindMatchesChinese 中文按 rune 计列，双宽字符不能按字节算。
func TestFindMatchesChinese(t *testing.T) {
	hits, err := findMatches([]string{`  "名字": "张三丰",`}, "张三", find.Options{})
	if err != nil {
		t.Fatalf("findMatches: %v", err)
	}
	if len(hits) != 1 || hits[0].start != 9 {
		t.Fatalf("中文命中位置不对：%+v", hits)
	}
}

// TestFindMatchesOptions 三个开关各管一件事：大小写、整词、正则。
func TestFindMatchesOptions(t *testing.T) {
	lines := []string{
		`"note": "neo and NEO"`,
		`"neon": 1`,
	}
	cases := []struct {
		name    string
		pattern string
		options find.Options
		want    int
	}{
		{"默认不分大小写", "neo", find.Options{}, 3},
		{"区分大小写", "neo", find.Options{CaseSensitive: true}, 2},
		{"整词跳过 neon", "neo", find.Options{WholeWord: true}, 2},
		{"正则", "ne.", find.Options{Regexp: true}, 3},
	}
	for _, one := range cases {
		hits, err := findMatches(lines, one.pattern, one.options)
		if err != nil {
			t.Fatalf("%s: findMatches: %v", one.name, err)
		}
		if len(hits) != one.want {
			t.Fatalf("%s: 应有 %d 处命中，实际 %d: %+v", one.name, one.want, len(hits), hits)
		}
	}
}

// TestFindMatchesRegexChinese 正则命中报的是字节区间，返回的仍是 rune 列。
func TestFindMatchesRegexChinese(t *testing.T) {
	hits, err := findMatches([]string{`  "名字": "张三丰",`}, "张.", find.Options{Regexp: true})
	if err != nil {
		t.Fatalf("findMatches: %v", err)
	}
	if len(hits) != 1 || hits[0].start != 9 || hits[0].end != 11 {
		t.Fatalf("正则命中位置不对：%+v", hits)
	}
}

// TestFindMatchesBadRegex 写了一半的正则编译失败，查找条据此提示「正则无效」。
func TestFindMatchesBadRegex(t *testing.T) {
	hits, err := findMatches([]string{"abc"}, "[", find.Options{Regexp: true})
	if err == nil || hits != nil {
		t.Fatalf("无效正则应报错：err=%v hits=%+v", err, hits)
	}
	if hits, err := findMatches([]string{"a[b"}, "[", find.Options{}); err != nil || len(hits) != 1 {
		t.Fatalf("关掉正则后 [ 是普通字符：err=%v hits=%+v", err, hits)
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

// TestFindToggles 查找条上的开关真的改扫描模式：Aa 区分大小写，.* 把半截正则报成「正则无效」。
func TestFindToggles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"OK":false}`)
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
	click(t, driver, "在响应里查找")
	pump(t, driver)
	if err := driver.Type("ok"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if len(model.find.hits) != 2 {
		t.Fatalf("忽略大小写应命中 2 处，实际 %d: %+v", len(model.find.hits), model.find.hits)
	}

	click(t, driver, "Aa")
	pump(t, driver)
	if !model.find.caseSensitive || len(model.find.hits) != 1 {
		t.Fatalf("Aa 后应只命中小写那处：开关=%v 命中 %d: %+v",
			model.find.caseSensitive, len(model.find.hits), model.find.hits)
	}

	click(t, driver, "在响应里查找")
	pump(t, driver)
	if err := driver.Type("["); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	click(t, driver, ".*")
	pump(t, driver)
	if !model.find.bad || !shows(driver, "正则无效") {
		t.Fatalf("半截正则应提示「正则无效」：bad=%v %v", model.find.bad, painted(driver))
	}
}
