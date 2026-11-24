package view

import "github.com/vivaui/viva/ui"

// editorTextHeight 估算 Markdown 源码在编辑框里要占的高度。
//
// viva 的 TextArea 不画滚动条（只保证光标所在行可见），要把长文档的滚动条交给
// 外层 Scroll，就得先给它一个「比可视区高」的确定高度：这里按字符宽度估行数，
// 再乘行高。估多一两行只影响滚动条比例，不影响编辑。
func (m *Model) editorTextHeight(c ui.C, width float32) float32 {
	size := c.Theme().Metrics.FontSizeM
	rows := editorRowCount(m.editor.text, width, size)
	height := float32(rows)*(size+6) + px(16)
	if min := px(160); height < min {
		height = min
	}
	return height
}

// editorRowCount 是折行后的行数：全角按字号宽、半角按 0.55 字号宽，按可用宽度累加。
func editorRowCount(text string, width, size float32) int {
	if width <= 0 {
		return 1
	}
	rows, line := 1, float32(0)
	for _, r := range text {
		if r == '\n' {
			rows++
			line = 0
			continue
		}
		w := size * 0.55
		if r > 0x2E7F {
			w = size
		}
		line += w
		for line > width {
			rows++
			line -= width
		}
	}
	return rows
}
