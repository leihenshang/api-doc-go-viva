package view

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// splitDrag 是一次分隔条拖动：viva 的 ui.Splitter 会在中间画一条 1px 的描边线，
// 而请求 / 响应之间的那条竖线在左右分栏里显得多余。这里用一块透明的 6px 区
// 自己处理指针，拖动手感与 Splitter 一致，但不画线。
type splitDrag struct {
	active       bool
	startPointer float32
	startPos     float32
}

// splitHandle 是自绘的竖向分隔条（只在左右分栏时用）。
//
// avail 是右侧还能让出多少（页面右缘 − 手柄右缘），用它算 Pos 的上限，
// 保证响应列不会被拖没；下限用 minFirst。
func (m *Model) splitHandle(c ui.C, state *ui.SplitterState, minFirst, minSecond float32) {
	bounds := c.Bounds()
	box := ui.Slot(c).Width(px(6)).FillHeight()
	id := ui.ID(c)
	box.Hit(id, "拖动调整请求与响应的宽度")
	box.Draw(func(d *ui.DrawContext) {
		ev, ok := d.Canvas.PointerOn(id)
		if !ok {
			return
		}
		switch ev.Kind {
		case gfx.PointerDown:
			m.splitDrag = splitDrag{active: true, startPointer: ev.X, startPos: state.Pos}
			d.Canvas.Consume()
		case gfx.PointerMove:
			if !m.splitDrag.active {
				return
			}
			next := m.splitDrag.startPos + ev.X - m.splitDrag.startPointer
			right := bounds.X + bounds.Width - (d.Rect.X + d.Rect.Width)
			if maxPos := state.Pos + right - minSecond; next > maxPos {
				next = maxPos
			}
			if next < minFirst {
				next = minFirst
			}
			state.Pos = next
			m.invalidator.Invalidate()
			d.Canvas.Consume()
		case gfx.PointerUp:
			m.splitDrag.active = false
			d.Canvas.Consume()
		}
	})
}
