package view

import (
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// 文档源码区的 Markdown 工具栏（N14）。
//
// 插入位置来自文档自己的 `*text.Editor`（`Caret` / `Selection` / `ReplaceRange`），
// 不是靠 TextArea 的光标接口——文档源码区迁到 ui/codeeditor 之后这条就不卡上游了。
// 所有动作都走 `ReplaceRange`：它是一个 undo 单元，Ctrl+Z 能整步撤回。

// mdAction 描述一次工具栏动作。
type mdAction struct {
	// prefix / suffix 包住选区；没有选区时插成 prefix+suffix 一对。
	prefix, suffix string
	// perLine 为真时按行加前缀（引用 / 列表），忽略 suffix。
	perLine bool
	// block 是一段整块插入的正文（表格）；非空时忽略其它字段。
	block string
	// caretShift 是没有选区时插入后的光标位置（相对插入起点，单位 rune）；
	// -1 表示放到整段插入内容的末尾。
	caretShift int
}

// mdButtons 是工具栏的按钮顺序。label 同时是无障碍名与测试里的点击名，彼此不能重复。
var mdButtons = []struct {
	label  string
	tip    string
	action mdAction
}{
	{"B", "粗体：**文字**", mdAction{prefix: "**", suffix: "**", caretShift: 2}},
	{"I", "斜体：*文字*", mdAction{prefix: "*", suffix: "*", caretShift: 1}},
	{"`", "行内代码：`代码`", mdAction{prefix: "`", suffix: "`", caretShift: 1}},
	{"{ }", "代码块：``` 围起来", mdAction{prefix: "```\n", suffix: "\n```", caretShift: -1}},
	{"链接", "链接：[文字](url)", mdAction{prefix: "[", suffix: "](url)", caretShift: -1}},
	{"引用", "引用：每行前加「> 」", mdAction{perLine: true, prefix: "> "}},
	{"列表", "列表：每行前加「- 」", mdAction{perLine: true, prefix: "- "}},
	{"表格", "插入 3 行 2 列的表格骨架",
		mdAction{block: "| 列 1 | 列 2 |\n| --- | --- |\n| 值 | 值 |\n"}},
}

// pointerInDocCode 报告指针是否在文档编辑器矩形里。
//
// 用它决定要不要抄选区：在编辑器里按下（选词、拖选）后光标变化是用户改的；
// 在工具栏上按下时平台会把插入符挪到正文末尾，那种变化不能抄（见 docMarkdownTools）。
func (m *Model) pointerInDocCode() bool {
	if !m.hover.in || m.docCodeRect.Width <= 0 {
		return false
	}
	return m.hover.x >= m.docCodeRect.X && m.hover.x < m.docCodeRect.X+m.docCodeRect.Width &&
		m.hover.y >= m.docCodeRect.Y && m.hover.y < m.docCodeRect.Y+m.docCodeRect.Height
}

// docMarkdownTools 是源码栏顶上的一排插入按钮（只在双屏编辑态出现）。
//
// 按钮是普通的 ui.Button；焦点被拿走、平台改插入符这些副作用，由「什么时候抄选区」
// 来吸收（见 pointerInDocCode）：只有指针还在编辑器里时才抄，按按钮那一帧不抄。
func (m *Model) docMarkdownTools(c ui.C) {
	ui.Row(c).FillWidth().Gap(px(2)).Items(ui.Center).Children(func() {
		for _, button := range mdButtons {
			item := button
			if ui.Button(c, item.label).Ghost().XSmall().Tooltip(item.tip).Clicked() {
				action := item.action
				// 按 Button 会把焦点从编辑器拿走，平台随即把插入符挪到正文末尾；
				// 但快照在「指针还在编辑器里」时就抄好了（见 pointerInDocCode），
				// 那一帧的绘制不会再覆盖它，所以这里用到的仍是用户的目光位置。
				selection := m.docSel
				c.After(func() { m.applyMarkdown(action, selection) })
			}
		}
		ui.Spacer(c)
	})
}

// applyMarkdown 按当前光标 / 选区插入 Markdown 标记，然后写回真源并标记改动。
func (m *Model) applyMarkdown(action mdAction, selection text.Range) {
	doc := m.docCodeDocument()
	if doc == nil {
		return
	}
	switch {
	case action.block != "":
		doc.Insert(action.block)
	case action.perLine:
		m.prefixLines(doc, action.prefix, selection)
	default:
		if selection.Start != selection.End {
			// 有选区：包起来（选中内容原样留在中间）。
			chosen := string([]rune(doc.String())[selection.Start:selection.End])
			doc.ReplaceRange(selection, action.prefix+chosen+action.suffix)
		} else {
			caret := selection.Start
			inserted := action.prefix + action.suffix
			doc.ReplaceRange(text.Range{Start: caret, End: caret}, inserted)
			shift := action.caretShift
			if shift < 0 {
				shift = len([]rune(inserted))
			}
			at := caret + shift
			doc.SetSelection(text.Range{Start: at, End: at})
		}
	}
	m.finishMarkdownEdit()
}

// prefixLines 把选区覆盖到的每一行行首都加上前缀（引用 / 列表）。
// 没有选区时只处理光标所在的那一行。
func (m *Model) prefixLines(doc *text.Editor, prefix string, selection text.Range) {
	value := doc.String()
	start, end := lineStartAt(value, selection.Start), lineEndAt(value, selection.End)
	runes := []rune(value)
	lines := strings.Split(string(runes[start:end]), "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	doc.ReplaceRange(text.Range{Start: start, End: end}, strings.Join(lines, "\n"))
}

// finishMarkdownEdit 把编辑器里的正文写回 m.editor.text，并走脏标记 / 自动保存。
func (m *Model) finishMarkdownEdit() {
	m.syncDocTextFromCode()
	m.markEdited()
	m.invalidator.Invalidate()
}

// lineStartAt 返回 index 所在行的行首（rune 索引）。
func lineStartAt(value string, index int) int {
	runes := []rune(value)
	if index > len(runes) {
		index = len(runes)
	}
	for index > 0 && runes[index-1] != '\n' {
		index--
	}
	return index
}

// lineEndAt 返回 index 所在行末（rune 索引，不含换行符）。
func lineEndAt(value string, index int) int {
	runes := []rune(value)
	if index < 0 {
		index = 0
	}
	for index < len(runes) && runes[index] != '\n' {
		index++
	}
	return index
}
