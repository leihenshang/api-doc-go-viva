package view

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
)

// openDocForEditing 打开一篇文档并保证处在双屏编辑态（工具栏只在源码栏上方出现）。
func openDocForEditing(t *testing.T, model *Model, driver *app.Driver) {
	t.Helper()
	waitForControl(t, driver, "MD 测试文档")
	click(t, driver, "MD 测试文档")
	pump(t, driver)
	if !model.ui.docSplit {
		click(t, driver, "编辑")
		pump(t, driver)
	}
	if model.docText == nil {
		t.Fatalf("双屏编辑态应有 text.Editor：%v", painted(driver))
	}
}

// TestMarkdownToolbarInserts 工具栏插到光标处：没有选区插一对标记、有选区就包裹、
// 引用 / 列表按行加前缀。
func TestMarkdownToolbarInserts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := writeCollection(t, "http://example.invalid")
	// 文档条目是 docs/ 下的 .md（front matter + 正文），writeCollection 不带，这里补一篇。
	writeFile(t, filepath.Join(dir, "docs", "测试文档.md"),
		"---\nuid: doc-viva\nseq: 1\n---\n\n正文第一段\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	openDocForEditing(t, model, driver)

	// 选一段文本。真实路径是「在编辑器里拖选（指针在编辑器内）」或键盘移动，
	// 这里直接把快照写进去，避免依赖鼠标几何。
	selectDoc := func(selection text.Range) {
		model.docText.SetSelection(selection)
		model.docSel = selection
		pump(t, driver)
	}

	// 1) 无选区：插入一对 **，光标停在中间。
	model.editor.text = "第一行\n第二行"
	pump(t, driver)
	selectDoc(text.Range{Start: 0, End: 0})
	click(t, driver, "B")
	pump(t, driver)
	if got := model.editor.text; got != "****第一行\n第二行" {
		t.Fatalf("粗体应插到光标处：%q", got)
	}
	if model.docText.Caret() != 2 {
		t.Fatalf("光标应落在两个 ** 之间：%d", model.docText.Caret())
	}

	// 2) 有选区：包裹选中文字（第二行是 rune 4..7）。
	model.editor.text = "第一行\n第二行"
	pump(t, driver)
	selectDoc(text.Range{Start: 4, End: 7})
	click(t, driver, "B")
	pump(t, driver)
	if got := model.editor.text; got != "第一行\n**第二行**" {
		t.Fatalf("粗体应包裹选中文字：%q", got)
	}

	// 3) 引用：选区覆盖两行，每行前加「> 」。
	model.editor.text = "甲\n乙"
	pump(t, driver)
	selectDoc(text.Range{Start: 0, End: 3})
	click(t, driver, "引用")
	pump(t, driver)
	if got := model.editor.text; got != "> 甲\n> 乙" {
		t.Fatalf("引用应按行加前缀：%q", got)
	}

	// 4) 列表：同样按行加「- 」。
	model.editor.text = "甲\n乙"
	pump(t, driver)
	selectDoc(text.Range{Start: 0, End: 3})
	click(t, driver, "列表")
	pump(t, driver)
	if got := model.editor.text; got != "- 甲\n- 乙" {
		t.Fatalf("列表应按行加前缀：%q", got)
	}

	// 5) 表格：插一段骨架（无选区，插在光标处）。
	model.editor.text = ""
	pump(t, driver)
	selectDoc(text.Range{Start: 0, End: 0})
	click(t, driver, "表格")
	pump(t, driver)
	if !strings.HasPrefix(model.editor.text, "| 列 1 | 列 2 |") {
		t.Fatalf("表格骨架没插入：%q", model.editor.text)
	}
	if !strings.Contains(model.editor.text, "| --- | --- |") {
		t.Fatalf("表格骨架缺分隔行：%q", model.editor.text)
	}
}
