package view

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui/codeeditor"
	"github.com/vivaui/viva/window"
)

// 文档源码区用 viva 的 codeeditor（行号栏 + 当前行高亮 + 选区都自带）。
//
// 正文真源仍是 m.editor.text：保存、草稿、自动保存、预览全读它，
// codeeditor 的 text.Editor 只是它的镜像。两边用 docMirror 对齐：
//
//   - m.editor.text 被别处改掉（恢复草稿、测试直接赋值、切换文档）→ 重建镜像；
//   - 用户在编辑器里改动 → 下一帧把镜像写回 m.editor.text。
//
// 这样既拿到底层编辑能力，又不用把保存/草稿那套逻辑搬到 text.Editor 上。

// docCodeDocument 保证镜像与 m.editor.text 一致，返回可交给编辑器的文本模型。
func (m *Model) docCodeDocument() *text.Editor {
	if m.docText == nil || m.docCodeFile != m.editor.file || m.editor.text != m.docMirror {
		m.docText = text.NewEditor(m.editor.text)
		// Markdown 不套代码块的括号配对 / 自动缩进，Tab 用空格。
		m.docText.SetCode(text.CodeOptions{TabWidth: 4})
		m.docMirror = m.editor.text
		m.docCodeFile = m.editor.file
		// 换文档时把组件的行缓存与滚动位置一起丢掉，否则新正文会沿用上一篇的布局。
		m.docCode = codeeditor.Editor{}
	}
	return m.docText
}

// syncDocTextFromCode 把编辑器里的正文写回真源（输入法组合中先不动）。
func (m *Model) syncDocTextFromCode() {
	if m.docText == nil {
		return
	}
	if _, composing := m.docText.Composition(); composing {
		return
	}
	got := m.docText.String()
	if got == m.editor.text {
		return
	}
	m.editor.text = got
	m.docMirror = got
}

// docCodeKey 是编辑器拿到焦点后的按键：编辑命令（Tab / 注释 / 移动行……）
// 交给 components，移动键按折行后的实际几何交给 components.CodeEditor.Move。
func (m *Model) docCodeKey(event gfx.KeyEvent) gfx.EventResult {
	if m.docText == nil {
		return gfx.EventResult{}
	}
	// Ctrl/Cmd+Z 被页面的「撤销请求编辑」抢走了（见 undo.go），
	// 编辑器有焦点时要先由自己处理，否则文档里的改动撤不回来。
	if primary := event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0; primary && !event.Release {
		switch strings.ToLower(event.Text) {
		case "z":
			if event.Modifiers&gfx.ModifierShift != 0 {
				if m.docText.Redo() {
					return gfx.EventResult{Handled: true, Repaint: true}
				}
			} else if m.docText.Undo() {
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		case "y":
			if m.docText.Redo() {
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
	}
	if command, ok := components.CodeEditorCommandForKey(event); ok && command.Apply(m.docText) {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
	if movement == components.TextareaMoveNone {
		return gfx.EventResult{}
	}
	value := m.docText.String()
	caret := m.docText.ByteOffset(m.docText.Caret())
	to := m.docCode.Code.Move(value, caret, movement)
	m.docText.MoveTo(m.docText.RuneIndex(to), extend)
	return gfx.EventResult{Handled: true, Repaint: true}
}

// docCodeInput 把平台的文本服务接到 text.Editor 上：打字、输入法组合、粘贴
// 都写进同一份模型；插入符与选区由 codeeditor 自己画，所以隐藏原生浮层。
func (m *Model) docCodeInput(window.TextInputContext) *window.TextInput {
	if m.docText == nil {
		return nil
	}
	return &window.TextInput{Editor: m.docText, HideNativeOverlays: true}
}
