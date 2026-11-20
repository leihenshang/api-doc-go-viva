package view

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 响应正文的「可选中」视图（JSON 与原始报文共用）。
//
// Viva 的文字画在 canvas 上，没有浏览器的原生选区，所以这里自绘：鼠标按下记锚点、
// 拖动扩选区、Ctrl+C 把选中文本写进剪贴板（不在松手时自动复制）。
// 选中底色画在文字之前，因此不需要叠加层；JSON 的折叠箭头也由这里直接处理。
const (
	selGutter = 18 // 左侧折叠箭头列宽（原始报文没有箭头，为 0）
	selPad    = 2  // 箭头与正文之间的空隙
	selLineH  = 18 // 每行高度（固定，坐标换算才准）
	selFont   = 11 // 字号
)

// selPos 是选区端点：可见行序号 + 行内字符（rune）列。
type selPos struct {
	line int
	col  int
}

// selView 是可选中正文的状态。几何与可见行在每次绘制时刷新，事件里只做坐标换算。
type selView struct {
	pressing bool
	has      bool
	anchor   selPos
	head     selPos

	plain bool // 原始报文（不折行、无箭头、单色）；否则是 JSON 树
	// 可见行（原始报文会把超宽的行按可用宽度切开，继续行记在 cont 里）
	vis  []string
	cont []bool
	// JSON 模式：可见行 → m.tables.json 的行号
	lineIndex []int

	x, y, charW float32 // 正文左上角与单字宽度（绘制时写入）
}

// reset 在切换视图（JSON ↔ 原始）时清掉旧选区。
func (sel *selView) reset(plain bool) {
	sel.pressing, sel.has = false, false
	sel.anchor, sel.head = selPos{}, selPos{}
	sel.plain = plain
	sel.vis, sel.cont, sel.lineIndex = nil, nil, nil
}

// jsonBody 把响应 JSON 画成可选中的文本区（整块一个命中区，高度 = 行数 × 行高）。
func (m *Model) jsonBody(c ui.C) {
	sel := &m.jsonSel
	if sel.plain || sel.vis == nil {
		sel.reset(false)
	}
	id := ui.ID(c)
	sel.buildJSON(m)
	box := ui.Slot(c).FillWidth().Height(float32(len(sel.vis))*selLineH).
		Hit(id, "响应正文").
		Tooltip("拖动选中，Ctrl+C 复制")
	box.Draw(func(d *ui.DrawContext) { m.paintJSON(c, d, id) })
}

// rawBody 把原始响应正文画成可选中的文本区。
func (m *Model) rawBody(c ui.C) {
	sel := &m.jsonSel
	if !sel.plain {
		sel.reset(true)
	}
	id := ui.ID(c)
	sel.buildRaw(m.resp.Body)
	box := ui.Slot(c).FillWidth().Height(float32(len(sel.vis))*selLineH).
		Hit(id, "响应正文").
		Tooltip("拖动选中，Ctrl+C 复制")
	box.Draw(func(d *ui.DrawContext) { m.paintRaw(c, d, id) })
}

// buildJSON 重建 JSON 的可见行（折叠后不出现的行不进来）。
func (sel *selView) buildJSON(m *Model) {
	sel.vis = sel.vis[:0]
	sel.cont = sel.cont[:0]
	sel.lineIndex = sel.lineIndex[:0]
	hiddenUntil := -1
	for index, line := range m.tables.json {
		if index < hiddenUntil {
			continue
		}
		sel.vis = append(sel.vis, jsonLineText(line))
		sel.cont = append(sel.cont, false)
		sel.lineIndex = append(sel.lineIndex, index)
		if end, foldable := m.tables.jsonFolds[index]; foldable && m.tables.jsonFolded[index] {
			hiddenUntil = end + 1
		}
	}
	sel.clamp()
}

// buildRaw 按 \n 切原始正文；超宽的行在绘制时再切（见 paintRaw）。
func (sel *selView) buildRaw(body string) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	sel.vis = sel.vis[:0]
	sel.cont = sel.cont[:0]
	for _, line := range lines {
		sel.vis = append(sel.vis, strings.TrimRight(line, "\r"))
		sel.cont = append(sel.cont, false)
	}
	sel.clamp()
}

// clamp 把选区端点夹回可见范围（折叠或换视图后行数会变）。
func (sel *selView) clamp() {
	sel.anchor.line = clampInt(sel.anchor.line, 0, max(len(sel.vis)-1, 0))
	sel.head.line = clampInt(sel.head.line, 0, max(len(sel.vis)-1, 0))
}

// wrapRaw 按可用宽度把原始报文的长行切开。续行在 cont 里标 true，复制时不插换行。
// 切点按真实字宽二分（中文是双宽，按字数切会溢出）。
func (sel *selView) wrapRaw(canvas *gfx.Canvas, width float32) {
	if width <= 8 {
		return
	}
	vis := make([]string, 0, len(sel.vis))
	cont := make([]bool, 0, len(sel.vis))
	for _, line := range sel.vis {
		runes := []rune(line)
		if len(runes) == 0 || canvas.TextWidthStyle(line, measureStyle) <= width {
			vis = append(vis, line)
			cont = append(cont, false)
			continue
		}
		for start := 0; start < len(runes); {
			lo, hi := start, len(runes)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if canvas.TextWidthStyle(string(runes[start:mid]), measureStyle) <= width {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			if lo <= start {
				lo = start + 1 // 至少放一个字符，避免原地打转
			}
			vis = append(vis, string(runes[start:lo]))
			cont = append(cont, start > 0)
			start = lo
		}
	}
	sel.vis, sel.cont = vis, cont
	sel.clamp()
}

// paintJSON 画选区底色、折叠箭头与彩色文本，并处理指针与 Ctrl+C。
func (m *Model) paintJSON(c ui.C, d *ui.DrawContext, id gfx.ID) {
	sel := &m.jsonSel
	sel.x = d.Rect.X + selGutter + selPad
	sel.y = d.Rect.Y
	sel.charW = max(d.Canvas.TextCaretStyle("0", gfx.TextStyle{Size: selFont, Mono: true}, 1), 1)

	m.handlePointer(c, d, id, true)
	m.copyOnShortcut(c, sel)
	m.paintSelection(d)

	for row, text := range sel.vis {
		top := d.Rect.Y + float32(row)*selLineH
		source := sel.lineIndex[row]
		if m.isFoldable(source) {
			arrow := "▾"
			if m.tables.jsonFolded[source] {
				arrow = "▸"
			}
			d.Canvas.Label(arrow, gfx.Rect{X: d.Rect.X, Y: top, Width: selGutter, Height: selLineH},
				gfx.TextStyle{Size: selFont, Color: m.theme.MutedForeground})
		}
		x := sel.x
		for _, span := range m.tables.json[source] {
			style := gfx.TextStyle{Size: selFont, Mono: true, Color: m.jsonTint(span.kind)}
			w := d.Canvas.TextWidthStyle(span.text, style)
			d.Canvas.Label(span.text, gfx.Rect{X: x, Y: top, Width: w, Height: selLineH}, style)
			x += w
		}
		if m.isFoldable(source) && m.tables.jsonFolded[source] {
			d.Canvas.Label("…", gfx.Rect{X: x, Y: top, Width: 24, Height: selLineH},
				gfx.TextStyle{Size: selFont, Mono: true, Color: m.theme.MutedForeground})
		}
		_ = text
	}
}

// paintRaw 画原始报文：单色等宽，超宽行先按可用宽度切开再画。
func (m *Model) paintRaw(c ui.C, d *ui.DrawContext, id gfx.ID) {
	sel := &m.jsonSel
	sel.x = d.Rect.X
	sel.y = d.Rect.Y
	sel.charW = max(d.Canvas.TextCaretStyle("0", gfx.TextStyle{Size: selFont, Mono: true}, 1), 1)
	sel.wrapRaw(d.Canvas, d.Rect.Width-4)

	m.handlePointer(c, d, id, false)
	m.copyOnShortcut(c, sel)
	m.paintSelection(d)

	style := gfx.TextStyle{Size: selFont, Mono: true, Color: m.theme.Foreground}
	for row, text := range sel.vis {
		top := d.Rect.Y + float32(row)*selLineH
		d.Canvas.Label(text, gfx.Rect{X: sel.x, Y: top, Width: d.Rect.Width, Height: selLineH}, style)
	}
}

// copyOnShortcut 处理 Ctrl+C / Cmd+C：有选区才复制。
func (m *Model) copyOnShortcut(c ui.C, sel *selView) {
	if !sel.has {
		return
	}
	if ui.Shortcut(c, ui.Primary, "c") {
		m.copyValue(sel.selectedText())
	}
}

// handlePointer 处理按下 / 拖动 / 松手：只在松手时结束选区，复制交给 Ctrl+C。
// withArrows 为真时（JSON），左gutter 的箭头列点击 = 折叠 / 展开。
func (m *Model) handlePointer(c ui.C, d *ui.DrawContext, id gfx.ID, withArrows bool) {
	sel := &m.jsonSel
	ev, ok := d.Canvas.PointerOn(id)
	if !ok {
		return
	}
	at := sel.posAt(d.Canvas, ev.X, ev.Y)
	switch ev.Kind {
	case gfx.PointerDown:
		if withArrows && ev.X < sel.x-selPad {
			if row := sel.rowAt(ev.Y); row >= 0 && m.isFoldable(sel.lineIndex[row]) {
				index := sel.lineIndex[row]
				c.After(func() { m.toggleJSONFold(index) })
				d.Canvas.Consume()
			}
			return
		}
		sel.pressing, sel.has = true, false
		sel.anchor, sel.head = at, at
		// 拿下这次按下：既不让它退化成点击，也让拖动期间逐帧重绘。
		d.Canvas.Consume()
	case gfx.PointerMove:
		if !sel.pressing {
			return
		}
		if at != sel.head {
			sel.head = at
			sel.has = sel.anchor != sel.head
		}
		// 关键：Consume 会请求重绘，否则选区要等下一次别的事件才更新，看起来「不跟手」。
		d.Canvas.Consume()
	case gfx.PointerUp:
		if !sel.pressing {
			return
		}
		sel.pressing = false
		sel.has = sel.anchor != sel.head
		d.Canvas.Consume()
	}
}

// isFoldable 报告某行能不能折叠（有对应的收尾行）。
func (m *Model) isFoldable(index int) bool {
	_, ok := m.tables.jsonFolds[index]
	return ok
}

// rowAt 把 y 换成可见行号（不在行内返回 -1）。
func (sel *selView) rowAt(y float32) int {
	if sel.charW <= 0 || len(sel.vis) == 0 {
		return -1
	}
	row := int((y - sel.y) / selLineH)
	if row < 0 || row >= len(sel.vis) {
		return -1
	}
	return row
}

// posAt 把页面坐标换成（行，字符列）。
// 列不用「等宽 × 字数」估算：中文/emoji 在等宽字体里是双宽，估出来的位置会越拖越偏。
// 这里按实际前缀宽度二分，中文行也能贴住指针。
func (sel *selView) posAt(canvas *gfx.Canvas, x, y float32) selPos {
	row := sel.rowAt(y)
	if row < 0 {
		row = clampInt(int((y-sel.y)/selLineH), 0, max(len(sel.vis)-1, 0))
	}
	line := sel.vis[row]
	runes := len([]rune(line))
	target := x - sel.x
	if target <= 0 || runes == 0 {
		return selPos{line: row}
	}
	if target >= canvas.TextWidthStyle(line, measureStyle) {
		return selPos{line: row, col: runes}
	}
	lo, hi := 0, runes
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if prefixWidth(canvas, line, mid) <= target {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return selPos{line: row, col: lo}
}

// measureStyle 是正文的度量样式：宽度只跟字号与等宽与否有关，颜色无所谓。
var measureStyle = gfx.TextStyle{Size: selFont, Mono: true}

// prefixWidth 是 line 前 col 个字符的像素宽度。
func prefixWidth(canvas *gfx.Canvas, line string, col int) float32 {
	runes := []rune(line)
	if col <= 0 {
		return 0
	}
	if col >= len(runes) {
		return canvas.TextWidthStyle(line, measureStyle)
	}
	return canvas.TextWidthStyle(string(runes[:col]), measureStyle)
}

// selectedText 取当前选区的文本：换行只在真正的行之间插，软折行的续行直接相接。
func (sel *selView) selectedText() string {
	if !sel.has {
		return ""
	}
	start, end := sel.anchor, sel.head
	if start.line > end.line || (start.line == end.line && start.col > end.col) {
		start, end = end, start
	}
	var b strings.Builder
	for line := start.line; line <= end.line && line < len(sel.vis); line++ {
		runes := []rune(sel.vis[line])
		from, to := 0, len(runes)
		if line == start.line {
			from = clampInt(start.col, 0, len(runes))
		}
		if line == end.line {
			to = clampInt(end.col, 0, len(runes))
		}
		if from > to {
			from = to
		}
		if line > start.line && !sel.cont[line] {
			b.WriteString("\n")
		}
		b.WriteString(string(runes[from:to]))
	}
	return b.String()
}

// paintSelection 在文字之前画选区底色。
func (m *Model) paintSelection(d *ui.DrawContext) {
	sel := &m.jsonSel
	if !sel.has {
		return
	}
	start, end := sel.anchor, sel.head
	if start.line > end.line || (start.line == end.line && start.col > end.col) {
		start, end = end, start
	}
	for line := start.line; line <= end.line && line < len(sel.vis); line++ {
		text := sel.vis[line]
		runes := len([]rune(text))
		from, to := 0, runes
		if line == start.line {
			from = clampInt(start.col, 0, runes)
		}
		if line == end.line {
			to = clampInt(end.col, 0, runes)
		}
		if from > to {
			from = to
		}
		// 底色按真实字形宽度算（中文是双宽），空行 / 行尾给一小段，视觉上不断开。
		x0 := sel.x + prefixWidth(d.Canvas, text, from)
		x1 := sel.x + prefixWidth(d.Canvas, text, to)
		width := x1 - x0
		if width <= 0 {
			width = sel.charW
		}
		top := sel.y + float32(line)*selLineH
		d.Canvas.Rounded(gfx.Rect{
			X: x0, Y: top + 1, Width: width, Height: selLineH - 3,
		}, 2, m.theme.Selection)
	}
}

// clampInt 把 v 夹到 [lo, hi]。
func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
