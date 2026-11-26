package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 变量悬停：指针停在输入框里的 {{变量}} 上时，显示它的取值。
//
// 提示为什么能跟着文字走：不自绘、也不改框架，用两条现成能力拼出来——
//
//	mark := d.Canvas.Mark()
//	d.Canvas.HitLabel(fieldID, 变量矩形, "", false)  // 标签热区：按下按标签语义交回输入框
//	d.Canvas.HintSince(mark, fieldID, 提示文字)       // 给这条热区补 Hint，提示仍由框架画
//
// 提示的锚点就是热区的矩形，所以落在变量那一小段上；按下仍是输入框的按下，
// 光标定位与打字都不受影响。输入框自己还挂一条 Tooltip：HasTooltip 认的是节点而不是
// 热区，不挂的话 paintTooltip 直接返回；但那条提示永远不显示——指针在框里别的位置时，
// 我们压一条不带 Hint 的标签热区把它盖掉（同一个 ID 取最上层那条热区）。
//
// 三处边界：
//   - 布局在绘制期才定：声明当场 Box.Rect() 还是零，所以几何一律在 Slot.Draw 里算；
//   - ui.TextArea 会折行，逐字坐标算不准，只保留整条框的提示；
//   - 聚焦且文字超宽的输入框，平台会横向滚动而 canvas 上的文本不动，这时不声明热区。
//
// 依赖的框架行为由 hoverprobe_test.go 与 hovertarget_test.go 守着。

// varTipLimit 是一条提示里最多列几个变量：再多会盖住半屏，也几乎见不到。
const varTipLimit = 4

// varField 是本帧登记的一个「可能含变量的输入框」。
type varField struct {
	id   gfx.ID
	text string
	tip  string
}

// varHover 记指针位置，以及本帧哪些输入框含变量——指针移动时据此决定要不要重绘。
type varHover struct {
	x, y   float32
	in     bool
	active bool
	zones  []gfx.Rect
	fields []varField
}

// trackPointer 记下指针位置。只有进出「含变量的输入框」才重绘：
// 别让每一次鼠标移动都把整页重画一遍；在框里移动也要重绘，因为提示要从一个变量换到下一个。
func (m *Model) trackPointer(event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerMove, gfx.PointerDown, gfx.PointerUp:
		m.hover.x, m.hover.y, m.hover.in = event.X, event.Y, true
		// 在编辑器里按下：等下一帧再抄光标（这一帧 placeCaret 还没落点）。
		if event.Kind == gfx.PointerDown && m.pointerInDocCode() {
			m.docSelPending = true
			m.invalidator.Invalidate()
		}
	case gfx.PointerExit:
		m.hover.in = false
	default:
		return gfx.EventResult{}
	}
	inside := m.hover.in && insideAnyRect(m.hover.zones, m.hover.x, m.hover.y)
	if inside || inside != m.hover.active {
		m.hover.active = inside
		m.invalidator.Invalidate()
	}
	return gfx.EventResult{}
}

func insideAnyRect(rects []gfx.Rect, x, y float32) bool {
	for _, rect := range rects {
		if x >= rect.X && x < rect.X+rect.Width && y >= rect.Y && y < rect.Y+rect.Height {
			return true
		}
	}
	return false
}

// varTip 把若干段文本里的 {{变量}} 拼成悬停提示，没有变量时返回空串。
func (m *Model) varTip(texts ...string) string {
	names := send.VarNames(texts...)
	if len(names) == 0 {
		return ""
	}
	var tip strings.Builder
	for index, name := range names {
		if index == varTipLimit {
			tip.WriteString(" · …")
			break
		}
		if index > 0 {
			tip.WriteString(" · ")
		}
		tip.WriteString(name)
		tip.WriteString(" = ")
		tip.WriteString(m.varValue(name))
	}
	return tip.String()
}

// varValue 是提示里一个变量的取值：内置变量写明来源，没定义的同发送时一样如实说。
func (m *Model) varValue(name string) string {
	if strings.HasPrefix(name, "$") {
		return "发送时生成"
	}
	value, ok := m.vars[name]
	if !ok {
		return "未定义（发送时保留原文）"
	}
	if value == "" {
		return "（空）"
	}
	return value
}

// varTipBox 给可能含 {{变量}} 的控件挂一条整框提示（折行的 TextArea 用这个）。
func (m *Model) varTipBox(box ui.Box, texts ...string) ui.Box {
	if tip := m.varTip(texts...); tip != "" {
		return box.Tooltip(tip)
	}
	return box
}

// varTipField 给单行输入框挂提示：提示只在指针落到某个 {{变量}} 上时出现
// （由 page.go 末尾那一个 Slot 声明热区）。
func (m *Model) varTipField(field ui.Box, text string) ui.Box {
	tip := m.varTip(text)
	if tip == "" {
		return field
	}
	m.hover.fields = append(m.hover.fields, varField{id: field.N().ID, text: text, tip: tip})
	// 这条 Tooltip 只为满足 HasTooltip（它认节点不认热区），显示与否由热区决定。
	return field.Tooltip(tip)
}

// varHotzones 是本帧最后声明的那一个 Slot：布局完成后才动手，给指针底下的变量声明热区。
func (m *Model) varHotzones(c ui.C) {
	if len(m.hover.fields) == 0 {
		return
	}
	ui.Slot(c).Absolute().Left(0).Top(0).Size(1, 1).Draw(m.paintVarHotzones)
}

// paintVarHotzones 逐个登记过的输入框量位置：指针落在某个 {{变量}} 上时才弹提示，
// 落在框里的其他地方一律压一条不带 Hint 的标签热区，把整框那条兜底提示挡掉——
// paintTooltip 取的是同一个 ID 最上层那条热区，空 Hint 就等于「这条不弹」。
func (m *Model) paintVarHotzones(d *ui.DrawContext) {
	canvas := d.Canvas
	for _, field := range m.hover.fields {
		box, ok := hitRect(canvas, field.id)
		if !ok {
			continue
		}
		// 供指针移动判断；切片容量跨帧复用。
		m.hover.zones = append(m.hover.zones, box)
		if !m.hover.in || m.hover.x < box.X || m.hover.x >= box.X+box.Width ||
			m.hover.y < box.Y || m.hover.y >= box.Y+box.Height {
			continue
		}
		if hot, ok := m.variableRect(canvas, field, box); ok {
			mark := canvas.Mark()
			canvas.HitLabel(field.id, hot, "", false)
			canvas.HintSince(mark, field.id, field.tip)
			continue
		}
		// 不在变量上：1×1 的无声热区，只为了盖住字段自己的那条提示。
		// 用标签语义声明，按下仍然按字段的点击处理，光标定位不受影响。
		canvas.HitLabel(field.id, gfx.Rect{X: m.hover.x, Y: m.hover.y, Width: 1, Height: 1}, "", false)
	}
}

// variableRect 是指针底下那个 {{变量}} 在屏幕上的矩形；指针不在变量上返回 false。
func (m *Model) variableRect(canvas *gfx.Canvas, field varField, box gfx.Rect) (gfx.Rect, bool) {
	origin, style, ok := paintedFieldText(canvas, box, field.text)
	if !ok {
		return gfx.Rect{}, false
	}
	// 聚焦且文字超宽：平台在横向滚动，canvas 上的文本却留在原位，算出来会漂。
	if canvas.Focused() == field.id && canvas.TextWidthStyle(field.text, style) > origin.Width {
		return gfx.Rect{}, false
	}
	if m.hover.y < origin.Y || m.hover.y >= origin.Y+origin.Height {
		return gfx.Rect{}, false
	}
	for _, placeholder := range send.Placeholders(field.text) {
		x0 := origin.X + canvas.TextWidthStyle(field.text[:placeholder.Start], style)
		x1 := x0 + canvas.TextWidthStyle(field.text[placeholder.Start:placeholder.End], style)
		if m.hover.x >= x0 && m.hover.x < x1 {
			return gfx.Rect{X: x0, Y: origin.Y, Width: x1 - x0, Height: origin.Height}, true
		}
	}
	return gfx.Rect{}, false
}

// hitRect 是某个控件这一帧的命中矩形（标签热区不算）。
func hitRect(canvas *gfx.Canvas, id gfx.ID) (gfx.Rect, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id && !hit.IsLabel && !hit.Block {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// paintedFieldText 找输入框画在 canvas 上的那行文字：原点、内容宽与字号都从画面上读，
// 不按内边距去猜。
func paintedFieldText(canvas *gfx.Canvas, box gfx.Rect, value string) (gfx.Rect, gfx.TextStyle, bool) {
	if value == "" {
		return gfx.Rect{}, gfx.TextStyle{}, false
	}
	for _, painted := range canvas.Texts() {
		if painted.Value != value {
			continue
		}
		if painted.Bounds.X < box.X || painted.Bounds.X > box.X+box.Width {
			continue
		}
		if painted.Bounds.Y < box.Y-4 || painted.Bounds.Y > box.Y+box.Height {
			continue
		}
		return painted.Bounds, painted.Style, true
	}
	return gfx.Rect{}, gfx.TextStyle{}, false
}
