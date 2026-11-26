package view

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// splitDrag 是一次分隔条拖动：viva 的 ui.Splitter 会在中间画一条 1px 的描边线，
// 而请求 / 响应之间的那条竖线常驻时显得多余。这里用一块透明的 12px 区自己处理
// 拖动，平时不画线，指针移上来（或正在拖）才浮出一条提示线——能看出这里可以拖，
// 又不挡视线。
//
// 拖动走 ui.DragState（与页签拖拽同一套），它会捕获指针：指针离开这 12px
// 之后仍然继续收到位置，不像 Canvas.PointerOn 那样拖两下就断。
type splitDrag struct {
	active       bool
	startPointer float32
	startPos     float32
}

// splitHandle 是自绘的竖向分隔条（只在左右分栏时用）。
func (m *Model) splitHandle(c ui.C, state *ui.SplitterState, minFirst, minSecond float32) {
	bounds := c.Bounds()
	id := ui.ID(c)
	handle := ui.Row(c).Width(px(12)).FillHeight().
		Hit(id, "拖动调整请求与响应的宽度").
		DragSource(&m.dragSplit)

	handle.Draw(func(d *ui.DrawContext) {
		if d.Canvas == nil {
			return
		}
		// 平时不画；指针进来或正在拖才浮出提示，告诉用户这里可以拖。
		inside := insideAnyRect([]gfx.Rect{d.Rect}, m.hover.x, m.hover.y)
		if !m.dragSplit.Dragging() && !inside {
			return
		}
		line := gfx.Rect{X: d.Rect.X + d.Rect.Width/2 - 0.5, Y: d.Rect.Y + px(8),
			Width: 1, Height: d.Rect.Height - px(16)}
		d.Canvas.Rounded(line, 0.5, d.Theme.Border)
		// 中间一个小把手：两根短横线，看得出是抓握点。
		knob := gfx.Rect{X: d.Rect.X + d.Rect.Width/2 - 2, Y: d.Rect.Y + d.Rect.Height/2 - 12,
			Width: 4, Height: 24}
		d.Canvas.Rounded(knob, 2, d.Theme.Border)
	})

	if !m.dragSplit.Dragging() || m.dragSplit.Source() != id {
		m.splitDrag.active = false
		return
	}
	at := m.dragSplit.At()
	if !m.splitDrag.active {
		m.splitDrag = splitDrag{active: true, startPointer: at.X, startPos: state.Pos}
		// 拖动开始的这一帧只记起点；补一帧，下一帧才能算出位移。
		m.invalidator.Invalidate()
		return
	}
	next := m.splitDrag.startPos + at.X - m.splitDrag.startPointer
	// 右侧至少留 minSecond：页面右缘减去分隔条右缘，就是响应列还能让出多少。
	right := bounds.X + bounds.Width - handle.Rect().Right()
	if maxPos := state.Pos + right - minSecond; next > maxPos {
		next = maxPos
	}
	if next < minFirst {
		next = minFirst
	}
	if next != state.Pos {
		state.Pos = next
		m.invalidator.Invalidate()
	}
}

// splitHandleH 是自绘的横向分隔条（上下分栏时用）：平时不画线，
// 指针移上来或正在拖才浮出提示线与中间把手。
func (m *Model) splitHandleH(c ui.C, state *ui.SplitterState, minFirst, minSecond float32) {
	bounds := c.Bounds()
	id := ui.ID(c)
	handle := ui.Row(c).FillWidth().Height(px(12)).
		Hit(id, "拖动调整请求与响应的高度").
		DragSource(&m.dragSplit)

	handle.Draw(func(d *ui.DrawContext) {
		if d.Canvas == nil {
			return
		}
		inside := insideAnyRect([]gfx.Rect{d.Rect}, m.hover.x, m.hover.y)
		if !m.dragSplit.Dragging() && !inside {
			return
		}
		line := gfx.Rect{X: d.Rect.X + px(8), Y: d.Rect.Y + d.Rect.Height/2 - 0.5,
			Width: d.Rect.Width - px(16), Height: 1}
		d.Canvas.Rounded(line, 0.5, d.Theme.Border)
		knob := gfx.Rect{X: d.Rect.X + d.Rect.Width/2 - 12, Y: d.Rect.Y + d.Rect.Height/2 - 2,
			Width: 24, Height: 4}
		d.Canvas.Rounded(knob, 2, d.Theme.Border)
	})

	if !m.dragSplit.Dragging() || m.dragSplit.Source() != id {
		m.splitDrag.active = false
		return
	}
	at := m.dragSplit.At()
	if !m.splitDrag.active {
		m.splitDrag = splitDrag{active: true, startPointer: at.Y, startPos: state.Pos}
		m.invalidator.Invalidate()
		return
	}
	next := m.splitDrag.startPos + at.Y - m.splitDrag.startPointer
	bottom := bounds.Y + bounds.Height - handle.Rect().Bottom()
	if maxPos := state.Pos + bottom - minSecond; next > maxPos {
		next = maxPos
	}
	if next < minFirst {
		next = minFirst
	}
	if next != state.Pos {
		state.Pos = next
		m.invalidator.Invalidate()
	}
}
