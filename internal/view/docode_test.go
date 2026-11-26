package view

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/window"
)

// TestDocCodeInput 源码区交出的文本服务必须满足两条：
//
//  1. 是 Editor 而不是 State + OnEvent —— 否则 painter 不替它收尾（组合、撤销组）；
//  2. 颜色等于编辑器底色 —— 平台文本层还会画一行「光标所在行」，
//     codeeditor 已经画过一遍，颜色不遮住就会在源码区底部多出一行。
//
// 完整的现象与取舍见 docode.go 的注释。
func TestDocCodeInput(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.editor.file = "docs/说明.md"
	model.editor.text = "# 标题\n\n正文。\n"
	model.docCodeDocument()

	input := model.docCodeInput(window.TextInputContext{})
	if input == nil {
		t.Fatal("源码区应交出文本服务")
	}
	if input.Editor == nil || input.Editor != model.docText {
		t.Fatal("必须交出 text.Editor：State 提供者拿不到 painter 的组合/撤销组收尾")
	}
	if !input.HideNativeOverlays {
		t.Fatal("光标与选区由 codeeditor 自绘，原生浮层要关掉")
	}
	if input.Color != model.pal.panel {
		t.Fatalf("文本层颜色应与编辑器底色一致：color=%+v panel=%+v", input.Color, model.pal.panel)
	}
	if input.Bounds != (gfx.Rect{}) {
		t.Fatalf("还没画过就该把矩形留给命中项，实际 %+v", input.Bounds)
	}

	// 画过之后，交给平台的矩形缩成右下角 1×1：平台会按它裁剪自己那份「光标所在行」。
	model.docCodeRect = gfx.Rect{X: 100, Y: 200, Width: 400, Height: 300}
	input = model.docCodeInput(window.TextInputContext{})
	if want := (gfx.Rect{X: 499, Y: 499, Width: 1, Height: 1}); input.Bounds != want {
		t.Fatalf("交给平台的是 1×1 裁剪矩形，实际 %+v（应为 %+v）", input.Bounds, want)
	}
}

// TestDocCodePointerMovesCaret 点正文中间某一行，插入符要落到那一行：
// 组件只处理滚动条拖动，文本区的点击得靠 docCodePointer 换算。
func TestDocCodePointerMovesCaret(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var doc *explorer.Entry
	for _, entry := range col.Entries {
		if entry.IsDoc() {
			doc = entry
			break
		}
	}
	if doc == nil {
		t.Fatal("夹子里没有文档")
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })

	// 正文要够长：点中间才有落点，短文档点哪都会夹到最后一行。
	long := ""
	for line := 1; line <= 40; line++ {
		long += "第 " + strconv.Itoa(line) + " 行\n"
	}
	model.openEntry(doc)
	model.editor.text = long
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	field := hitBoundsOf(t, driver, "文档内容")
	if err := driver.Click(field.X+field.Width*0.55, field.Y+field.Height/2); err != nil {
		t.Fatalf("Click: %v", err)
	}
	pump(t, driver)

	caret := model.docText.ByteOffset(model.docText.Caret())
	line := strings.Count(model.docText.String()[:caret], "\n") + 1
	if line < 8 || line > 35 {
		t.Fatalf("插入符应落在点中的那几行，实际第 %d 行：%q", line, model.docText.String()[:caret])
	}
	if caret >= len(model.docText.String())-3 {
		t.Fatalf("插入符仍停在文档末尾：%d / %d", caret, len(model.docText.String()))
	}
}

// TestDocCodeHitIsPlaced 守住源码区命中项的形态。
//
// interaction.TextInputHit 取的是「第一个 RoleTextField 命中」，而 ui/codeeditor 的
// wrapper 会先以 RoleTextField 声明一个没有文本几何的命中；那样平台文本层就把这篇
// 多行 Markdown 当单行：按下只按 x 换算插入符，整篇还会被再画一遍。控件自己让成
// RoleGroup 后，剩下的那一份才带 Multiline 与 Placement。
func TestDocCodeHitIsPlaced(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var doc *explorer.Entry
	for _, entry := range col.Entries {
		if entry.IsDoc() {
			doc = entry
			break
		}
	}
	if doc == nil {
		t.Fatal("夹子里没有文档")
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })
	model.openEntry(doc)
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	snap := driver.Snapshot()
	for index, hit := range snap.Hits {
		if hit.ID != model.docCodeID || hit.Role != gfx.RoleTextField {
			continue
		}
		text := snap.HitTexts[index]
		if !hit.Multiline || text.Placement == nil {
			t.Fatalf("文本层会拿这一份当单行：multiline=%v placement=%T bounds=%+v",
				hit.Multiline, text.Placement, hit.Bounds)
		}
		return
	}
	t.Fatal("源码区没有可编辑的文本命中")
}

// TestDocCodeWheelScrolls 源码区的滚轮要能滚动：控件是我们自己声明的，
// 不接 Scroll 回调就没人转给组件。
func TestDocCodeWheelScrolls(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var doc *explorer.Entry
	for _, entry := range col.Entries {
		if entry.IsDoc() {
			doc = entry
			break
		}
	}
	if doc == nil {
		t.Fatal("夹子里没有文档")
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })

	long := ""
	for line := 1; line <= 60; line++ {
		long += "第 " + strconv.Itoa(line) + " 行\n"
	}
	model.openEntry(doc)
	model.editor.text = long
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	before := model.docCode.Code.ScrollY()
	field := hitBoundsOf(t, driver, "文档内容")
	if err := driver.Scroll(gfx.ScrollEvent{X: field.X + field.Width/2, Y: field.Y + field.Height/2, DeltaY: -120}); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	pump(t, driver)
	if model.docCode.Code.ScrollY() == before {
		t.Fatalf("滚轮没滚动编辑器：before=%.1f after=%.1f", before, model.docCode.Code.ScrollY())
	}
}
