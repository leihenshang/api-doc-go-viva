package view

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

// 这是 mermaid 渲染的**实验实现**（方案 E 的第一版，未提交）：
// 只认 flowchart / graph 与 pie，用 canvas 的公开绘制指令 + 自己算的布局画出来。
// 其他语法不渲染，调用方退回原来的代码块显示。
//
// 为什么这么做见 doc/mermaid.md：viva 的 MarkdownView 没有节点级回调，
// 唯一能让图拿到自己高度的路，是把预览切成「文本段 + mermaid 段」。

const (
	// mermaidSegmentGap 是切段后段与段之间的间距，取正文行距的一半左右。
	mermaidSegmentGap float32 = 10
	mermaidNodePadX   float32 = 14
	mermaidNodeH      float32 = 30
	mermaidLayerGap   float32 = 38
	mermaidLaneGap    float32 = 20
	// mermaidMaxNodeWidth 是节点宽度上限：超过就折行，别让一个长标签把图撑爆。
	mermaidMaxNodeWidth float32 = 180
)

// mermaidPalette 是饼图的扇区颜色（sRGB 字节，位图与图例共用）。
var mermaidPalette = [][3]byte{
	{0x2f, 0x7d, 0x5f},
	{0x3a, 0x86, 0xc8},
	{0xd0, 0x8a, 0x2e},
	{0xc4, 0x53, 0x4a},
	{0x7a, 0x62, 0xc0},
	{0x88, 0x88, 0x8e},
}

type mermaidKind int

const (
	mermaidUnsupported mermaidKind = iota
	mermaidFlow
	mermaidPie
)

// mermaidDiagram 是一张排好版的图：布局只在源码 / 宽度 / 主题变化时算一次。
type mermaidDiagram struct {
	kind        mermaidKind
	title       string
	leftToRight bool
	note        string // 不支持时的原因（画一行灰字，而不是静默空白）

	nodes []mermaidNode
	edges []mermaidEdge

	backEdges []bool // 与 edges 一一对应：true 表示回边（往上绕）
	// routes 是 dagre 给的边折点（相对图原点）；为空表示走了手写布局。
	routes [][][]float32

	clusters []mermaidCluster

	slices []mermaidSlice
	pie    gfx.Bitmap // 饼图预算的位图（逐像素画太贵，画一次贴上去）

	width, height float32
	pieTop        float32 // 饼图上方给 title 留的高度
}

// mermaidShape 是节点外形：mermaid 用 `[..]` / `(..)` / `([..])` / `{..}` 区分。
type mermaidShape int

const (
	shapeRect mermaidShape = iota
	shapeRound
	shapeStadium
	shapeDiamond
)

// nodeStyle 是 classDef / style 解出来的节点外观（只覆盖给了值的项）。
type nodeStyle struct {
	fill, stroke, text gfx.Color
	hasFill            bool
	hasStroke          bool
	hasText            bool
	strokeWidth        float32
	dashed             bool
}

// merge 把 src 里给了值的项盖到自己身上（cls 先、style 后，后者优先）。
func (s *nodeStyle) merge(src nodeStyle) {
	if src.hasFill {
		s.fill, s.hasFill = src.fill, true
	}
	if src.hasStroke {
		s.stroke, s.hasStroke = src.stroke, true
	}
	if src.hasText {
		s.text, s.hasText = src.text, true
	}
	if src.strokeWidth > 0 {
		s.strokeWidth = src.strokeWidth
	}
	if src.dashed {
		s.dashed = true
	}
}

type mermaidNode struct {
	label       string
	id          string // 源码里的原始 id（classDef / style 靠它匹配）
	style       nodeStyle
	className   string   // 从 `A[x]:::cls` 或 `class A cls` 来
	lines       []string // 换行后的显示行（layout 时按宽度断行）
	cluster     string   // 所属 subgraph 的 id，空表示不在任何分组里
	shape       mermaidShape
	depth, lane int
	x, y, w, h  float32
}

// mermaidCluster 是一个 subgraph：dagre 会把它的子节点包起来并算出包围盒。
type mermaidCluster struct {
	id, title  string
	x, y, w, h float32
}

type mermaidEdge struct {
	from, to int
	label    string
}

type mermaidSlice struct {
	label string
	value float32
	color gfx.Color
}

// splitMermaidSegments 把一段 Markdown 按 mermaid 块切开。
// 没有 mermaid 块时返回 nil，调用方继续走原来的单 Document 快路径。
func splitMermaidSegments(src string) []mermaidSegment {
	if !strings.Contains(src, "```") {
		return nil
	}
	lines := strings.SplitAfter(src, "\n")
	var out []mermaidSegment
	var text strings.Builder
	inFence := false
	var fence strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") {
				lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
				if strings.EqualFold(lang, "mermaid") {
					if text.Len() > 0 {
						out = append(out, mermaidSegment{text: text.String()})
						text.Reset()
					}
					inFence = true
					fence.Reset()
					continue
				}
			}
			text.WriteString(line)
			continue
		}
		// 在 mermaid 块里：``` 收尾
		if strings.HasPrefix(trimmed, "```") {
			inFence = false
			out = append(out, mermaidSegment{mermaid: true, text: fence.String()})
			continue
		}
		fence.WriteString(line)
	}
	if inFence {
		// 没收尾的块当普通文本（和 viva 的解析器一样「没闭合就按原样」）。
		text.WriteString("```mermaid\n")
		text.WriteString(fence.String())
	}
	if text.Len() > 0 {
		out = append(out, mermaidSegment{text: text.String()})
	}
	// 只有一个文本段 = 其实没有 mermaid 块。
	if len(out) == 1 && !out[0].mermaid {
		return nil
	}
	return out
}

// docSegmentView 是一段排好版的预览内容：diagram 非空表示这段是 mermaid。
type docSegmentView struct {
	text    string
	doc     markdown.Document
	view    components.MarkdownView
	height  float32
	diagram *mermaidDiagram
}

// mermaidSegment 是切出来的一段：文本段交给 MarkdownView，mermaid 段自己画。
type mermaidSegment struct {
	mermaid bool
	text    string
}

// parseMermaid 只认两类图，够验证链路。
func parseMermaid(src string) *mermaidDiagram {
	d := &mermaidDiagram{kind: mermaidUnsupported}
	lines := strings.Split(src, "\n")
	header := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		header = line
		break
	}
	lower := strings.ToLower(header)
	switch {
	case strings.HasPrefix(lower, "flowchart"), strings.HasPrefix(lower, "graph"):
		d.kind = mermaidFlow
		fields := strings.Fields(lower)
		for _, f := range fields[1:] {
			if f == "lr" || f == "rl" {
				d.leftToRight = true
			}
		}
		parseFlowchart(d, src)
	case strings.HasPrefix(lower, "pie"):
		d.kind = mermaidPie
		parsePie(d, src)
	default:
		d.note = "暂不支持：" + firstWord(header)
	}
	return d
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

// parseFlowchart 认这几种写法：
//
//	A[文字] --> B[文字]
//	A(文字) --- B{文字}
//	A --> B; B --> C
//	A -->|说明| B
func parseFlowchart(d *mermaidDiagram, src string) {
	index := map[string]int{}
	// subgraph 可以嵌套：栈顶就是当前分组。
	// classDef / class / style 解析结果，最后统一套到节点上（顺序：classDef → style）。
	classDefs := map[string]nodeStyle{}
	classAssign := map[string]string{}
	directStyle := map[string]nodeStyle{}
	var clusterStack []string
	currentCluster := func() string {
		if len(clusterStack) == 0 {
			return ""
		}
		return clusterStack[len(clusterStack)-1]
	}
	add := func(ref string) int {
		id, label, shape, className := parseNodeRef(ref)
		if id == "" {
			return -1
		}
		if i, ok := index[id]; ok {
			if n := &d.nodes[i]; label != "" && n.label == id {
				n.label = label
				n.shape = shape
			}
			if n := &d.nodes[i]; n.cluster == "" {
				n.cluster = currentCluster()
			}
			if className != "" {
				n := &d.nodes[i]
				n.className = className
			}
			return i
		}
		index[id] = len(d.nodes)
		d.nodes = append(d.nodes, mermaidNode{
			id: id, label: label, shape: shape, cluster: currentCluster(), className: className,
		})
		return len(d.nodes) - 1
	}
	if len(d.nodes) > 0 {
		// 从空开始
		d.nodes = d.nodes[:0]
	}
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "flowchart"), strings.HasPrefix(lower, "graph"):
			continue
		case strings.HasPrefix(lower, "classdef"):
			// classDef highlight fill:#f9f,stroke:#333,stroke-width:2px,color:#fff
			rest := strings.TrimSpace(line[len("classDef"):])
			name, props, ok := strings.Cut(rest, " ")
			if ok {
				classDefs[strings.TrimSpace(name)] = parseMermaidStyle(props)
			}
			continue
		case strings.HasPrefix(lower, "class "):
			// class A,B highlight
			rest := strings.TrimSpace(line[len("class"):])
			ids, name, ok := strings.Cut(rest, " ")
			if ok {
				for _, id := range strings.Split(ids, ",") {
					if id = strings.TrimSpace(id); id != "" {
						classAssign[id] = strings.TrimSpace(name)
					}
				}
			}
			continue
		case strings.HasPrefix(lower, "style "):
			// style C fill:#bbf,stroke:#f66
			rest := strings.TrimSpace(line[len("style"):])
			id, props, ok := strings.Cut(rest, " ")
			if ok {
				directStyle[strings.TrimSpace(id)] = parseMermaidStyle(props)
			}
			continue
		case strings.HasPrefix(lower, "subgraph"):
			id, title := splitSubgraph(strings.TrimSpace(line[len("subgraph"):]))
			if id != "" {
				d.clusters = append(d.clusters, mermaidCluster{id: id, title: title})
				clusterStack = append(clusterStack, id)
			}
			continue
		case lower == "end":
			if len(clusterStack) > 0 {
				clusterStack = clusterStack[:len(clusterStack)-1]
			}
			continue
		}
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// 一行可能是链式：A --> B -->|是| C（mermaid 里很常见），逐段拆开两两建边。
			prevID, pending := -1, ""
			rest := part
			for {
				left, right, _, ok := splitArrow(rest)
				if !ok {
					if id := add(strings.TrimSpace(rest)); id >= 0 && prevID >= 0 {
						d.edges = append(d.edges, mermaidEdge{from: prevID, to: id, label: pending})
					}
					break
				}
				leftID := add(strings.TrimSpace(left))
				label := ""
				if strings.HasPrefix(right, "|") {
					if end := strings.Index(right[1:], "|"); end >= 0 {
						label = strings.TrimSpace(right[1 : 1+end])
						right = strings.TrimSpace(right[2+end:])
					}
				}
				if prevID >= 0 && leftID >= 0 {
					d.edges = append(d.edges, mermaidEdge{from: prevID, to: leftID, label: pending})
				}
				prevID = leftID
				pending = label
				rest = right
			}
		}
	}
	// 样式按 id 套到节点：classDef 先、显式 style 后（后者优先）。
	for i := range d.nodes {
		n := &d.nodes[i]
		cls := n.className
		if assigned, ok := classAssign[n.id]; ok {
			cls = assigned
		}
		if def, ok := classDefs[cls]; ok {
			n.style.merge(def)
		}
		if st, ok := directStyle[n.id]; ok {
			n.style.merge(st)
		}
	}
}

// parseMermaidStyle 解析 `fill:#f9f,stroke:#333,stroke-width:2px,color:#fff,stroke-dasharray:5 5`。
func parseMermaidStyle(props string) nodeStyle {
	var st nodeStyle
	for _, kv := range strings.Split(props, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(kv), ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "fill":
			if c, ok := parseMermaidColor(val); ok {
				st.fill, st.hasFill = c, true
			}
		case "stroke":
			if c, ok := parseMermaidColor(val); ok {
				st.stroke, st.hasStroke = c, true
			}
		case "color":
			if c, ok := parseMermaidColor(val); ok {
				st.text, st.hasText = c, true
			}
		case "stroke-width":
			var w float32
			if _, err := fmt.Sscanf(strings.TrimSuffix(val, "px"), "%f", &w); err == nil && w > 0 {
				st.strokeWidth = w
			}
		case "stroke-dasharray":
			if val != "" && val != "0" && !strings.EqualFold(val, "none") {
				st.dashed = true
			}
		}
	}
	return st
}

// mermaidColorNames 是 mermaid 图里最常出现的几个命名色（够覆盖手工写的文档）。
var mermaidColorNames = map[string][3]byte{
	"red": {255, 0, 0}, "green": {0, 128, 0}, "blue": {0, 0, 255},
	"orange": {255, 165, 0}, "yellow": {255, 255, 0}, "purple": {128, 0, 128},
	"pink": {255, 192, 203}, "gray": {128, 128, 128}, "grey": {128, 128, 128},
	"black": {0, 0, 0}, "white": {255, 255, 255}, "cyan": {0, 255, 255},
	"magenta": {255, 0, 255}, "lime": {0, 255, 0}, "teal": {0, 128, 128},
	"navy": {0, 0, 128}, "silver": {192, 192, 192}, "maroon": {128, 0, 0},
	"lightblue": {173, 216, 230}, "lightgreen": {144, 238, 144},
	"lightyellow": {255, 255, 224}, "lightgray": {211, 211, 211}, "lightgrey": {211, 211, 211},
}

// parseMermaidColor 只认 #rgb / #rrggbb / 少量命名色；rgb() 之类一律当没给。
func parseMermaidColor(value string) (gfx.Color, bool) {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" || strings.EqualFold(v, "none") || strings.EqualFold(v, "transparent") {
		return gfx.Color{}, false
	}
	if strings.HasPrefix(v, "#") {
		hex := v[1:]
		switch len(hex) {
		case 3:
			r, ok1 := hexNibble(hex[0])
			g, ok2 := hexNibble(hex[1])
			b, ok3 := hexNibble(hex[2])
			if ok1 && ok2 && ok3 {
				return gfx.RGBA(r*17, g*17, b*17, 1), true
			}
		case 6:
			var r, g, b uint8
			if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err == nil {
				return gfx.RGBA(r, g, b, 1), true
			}
		}
		return gfx.Color{}, false
	}
	if rgb, ok := mermaidColorNames[v]; ok {
		return gfx.RGBA(rgb[0], rgb[1], rgb[2], 1), true
	}
	return gfx.Color{}, false
}

func hexNibble(ch byte) (uint8, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0', true
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10, true
	}
	return 0, false
}

// splitSubgraph 拆 `subgraph 名称` 与 `subgraph id [标题]` 两种写法。
func splitSubgraph(rest string) (id, title string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	if open := strings.Index(rest, "["); open >= 0 {
		id = strings.TrimSpace(rest[:open])
		title = strings.Trim(strings.TrimSpace(rest[open:]), "[]")
		title = strings.Trim(title, `"'`)
	} else {
		title = strings.Trim(rest, `"'`)
		id = title
	}
	if id == "" {
		id = title
	}
	return id, title
}

// splitArrow 找到第一条连线的分隔位置（--> / --- / -.-> 都当普通边）。
func splitArrow(s string) (left, right, arrow string, ok bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case '-':
			if depth != 0 {
				continue
			}
			j := i
			for j < len(s) && (s[j] == '-' || s[j] == '.' || s[j] == '>') {
				j++
			}
			if j-i >= 2 {
				return strings.TrimSpace(s[:i]), strings.TrimSpace(s[j:]), s[i:j], true
			}
		}
	}
	return "", "", "", false
}

// parseNodeRef 拆 A[文字] / A(文字) / A([文字]) / A{文字} / A，返回 id、显示文字与外形。
func parseNodeRef(s string) (id, label string, shape mermaidShape, className string) {
	shape = shapeRect
	s = strings.TrimSpace(s)
	// 行尾的 :::className（inline class）先摘掉
	if i := strings.Index(s, ":::"); i >= 0 {
		className = strings.TrimSpace(s[i+3:])
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return "", "", shape, className
	}
	cut := len(s)
	for i := 0; i < len(s); i++ {
		if strings.ContainsRune("[({\"'", rune(s[i])) {
			cut = i
			break
		}
	}
	id = strings.TrimSpace(s[:cut])
	label = id
	rest := strings.TrimSpace(s[cut:])
	switch {
	case strings.HasPrefix(rest, "(["):
		shape = shapeStadium
	case strings.HasPrefix(rest, "(("):
		shape = shapeStadium
	case strings.HasPrefix(rest, "{"):
		shape = shapeDiamond
	case strings.HasPrefix(rest, "("):
		shape = shapeRound
	}
	if rest != "" {
		label = strings.Trim(rest, "[](){}")
		label = strings.TrimSpace(label)
		label = strings.Trim(label, `"'`)
		label = strings.ReplaceAll(label, `<br/>`, " ")
		label = strings.ReplaceAll(label, "<br>", " ")
		label = strings.TrimSpace(label)
		if label == "" {
			label = id
		}
	}
	if id == "" && label != "" {
		id = label
	}
	return id, label, shape, className
}

func parsePie(d *mermaidDiagram, src string) {
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "pie") {
			if i := strings.Index(line, "title"); i >= 0 {
				d.title = strings.TrimSpace(line[i+len("title"):])
			}
			continue
		}
		if strings.HasPrefix(lower, "title") {
			d.title = strings.TrimSpace(line[len("title"):])
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		label := strings.Trim(strings.TrimSpace(line[:colon]), `"`)
		value, err := strconv.ParseFloat(strings.TrimSpace(line[colon+1:]), 64)
		if err != nil || value <= 0 {
			continue
		}
		d.slices = append(d.slices, mermaidSlice{label: label, value: float32(value)})
	}
	if len(d.slices) == 0 {
		d.kind = mermaidUnsupported
		d.note = "pie 里没有可用的数据行"
	}
}

// layoutMermaid 算尺寸与位置；只用到 canvas 量文字宽度（与正文同一套字体度量）。
func layoutMermaid(d *mermaidDiagram, canvas *gfx.Canvas, width float32) {
	if d == nil {
		return
	}
	switch d.kind {
	case mermaidFlow:
		layoutFlow(d, canvas, width)
	case mermaidPie:
		layoutPie(d, width)
	default:
		d.height = 44
		if d.width == 0 {
			d.width = width
		}
	}
}

// measureFlowNodes 先按文字量出每个节点的宽高：dagre 需要真实尺寸才能排。
func measureFlowNodes(d *mermaidDiagram, canvas *gfx.Canvas, width float32) {
	const size float32 = 12
	const lineHeight float32 = size * 1.35
	maxWidth := minF(width-8, mermaidMaxNodeWidth)
	if maxWidth < 80 {
		maxWidth = 80
	}
	for i := range d.nodes {
		n := &d.nodes[i]
		pad := 2 * mermaidNodePadX
		labelRoom := maxWidth - pad - 6
		if n.shape == shapeDiamond {
			// 菱形的斜边占掉两侧空间，中间能放字的只有六成左右；高度也不能太扁，
			// 否则尖角看不出来（之前 h 固定 30+10，宽 140 的菱形被压成 3.5:1，像圆角矩形）。
			pad += 34
			labelRoom = labelRoom * 0.8
		}
		// 先按最大宽度断行，再取最长的一行做节点宽度：长标签不再被裁，而是折成多行。
		n.lines = wrapLabel(canvas, n.label, labelRoom, size)
		w := float32(24)
		for _, line := range n.lines {
			if tw := canvas.TextWidth(line, size); tw > w {
				w = tw
			}
		}
		n.w = w + pad + 6
		if n.w > maxWidth {
			n.w = maxWidth
		}
		n.h = float32(len(n.lines))*lineHeight + 14
		if n.h < mermaidNodeH {
			n.h = mermaidNodeH
		}
		if n.shape == shapeDiamond {
			// 高度跟着宽度走：宽高比约 1.8:1，尖角才立得起来
			if want := n.w * 0.55; want > n.h {
				n.h = want
			}
			if n.h < 52 {
				n.h = 52
			}
		}
	}
}

// wrapLabel 按像素宽度把标签断成多行：逐字累加宽度，超了就换行。
// 逐字量宽会调 TextWidth n 次（标签一般十几个字，够快）。
func wrapLabel(canvas *gfx.Canvas, text string, maxWidth, size float32) []string {
	if maxWidth <= 0 || text == "" {
		return []string{text}
	}
	var lines []string
	var cur strings.Builder
	curWidth := float32(0)
	for _, r := range text {
		piece := string(r)
		w := canvas.TextWidth(piece, size)
		if curWidth+w > maxWidth && cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curWidth = 0
			if piece == " " {
				continue
			}
		}
		cur.WriteString(piece)
		curWidth += w
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		lines = []string{text}
	}
	return lines
}

// layoutFlow 先让 dagre 排（交叉最小化、多方向、边路由都是它的活）；
// dagre 起不来才回退到手写的分层排布。
func layoutFlow(d *mermaidDiagram, canvas *gfx.Canvas, width float32) {
	if len(d.nodes) == 0 {
		d.height = 44
		d.width = width
		d.note = "flowchart 里没有节点"
		return
	}
	measureFlowNodes(d, canvas, width)
	if layoutFlowDagre(d, width) {
		return
	}
	layoutFlowManual(d, width)
}

// layoutFlowDagre 用 goja 跑 dagre（见 dagre.go），拿回中心坐标与边折点。
func layoutFlowDagre(d *mermaidDiagram, width float32) bool {
	spec := dagreSpec{
		Rankdir: "TB",
		Nodesep: 24,
		Ranksep: 38,
		Nodes:   make([]dagreNodeIn, len(d.nodes)),
		Edges:   make([]dagreEdgeIn, len(d.edges)),
	}
	if d.leftToRight {
		spec.Rankdir = "LR"
	}
	for i := range d.nodes {
		spec.Nodes[i] = dagreNodeIn{
			ID:      strconv.Itoa(i),
			Cluster: d.nodes[i].cluster,
			Width:   d.nodes[i].w,
			Height:  d.nodes[i].h,
		}
	}
	for i, e := range d.edges {
		spec.Edges[i] = dagreEdgeIn{From: strconv.Itoa(e.from), To: strconv.Itoa(e.to)}
	}
	for i := range d.clusters {
		spec.Clusters = append(spec.Clusters, d.clusters[i].id)
	}
	out, err := runDagre(spec)
	if err != nil {
		return false
	}
	// dagre 给的是中心坐标，内部统一用左上角。
	for i := range d.nodes {
		d.nodes[i].x = float32(out.Nodes[i].X) - d.nodes[i].w/2
		d.nodes[i].y = float32(out.Nodes[i].Y) - d.nodes[i].h/2
		d.nodes[i].depth = 0
		d.nodes[i].lane = 0
	}
	d.routes = make([][][]float32, len(d.edges))
	minX, minY := float32(0), float32(0)
	maxX, maxY := float32(0), float32(0)
	first := true
	for i := range d.nodes {
		n := &d.nodes[i]
		if first || n.x < minX {
			minX = n.x
		}
		if first || n.y < minY {
			minY = n.y
		}
		if first || n.x+n.w > maxX {
			maxX = n.x + n.w
		}
		if first || n.y+n.h > maxY {
			maxY = n.y + n.h
		}
		first = false
	}
	for i := range d.edges {
		pts := out.Edges[i].Points
		route := make([][]float32, 0, len(pts))
		for _, p := range pts {
			px, py := float32(p.X), float32(p.Y)
			if px < minX {
				minX = px
			}
			if px > maxX {
				maxX = px
			}
			if py < minY {
				minY = py
			}
			if py > maxY {
				maxY = py
			}
			route = append(route, []float32{px, py})
		}
		d.routes[i] = route
	}
	// dagre 给 cluster 的 x/y 是它的左上角，width/height 是包围盒尺寸。
	for i := range d.clusters {
		if i >= len(out.Clusters) {
			break
		}
		c := out.Clusters[i]
		// dagre 里所有节点（含 cluster）的 x/y 都是中心点，这里换算成左上角。
		d.clusters[i].w = float32(c.W)
		d.clusters[i].h = float32(c.H)
		d.clusters[i].x = float32(c.X) - d.clusters[i].w/2
		d.clusters[i].y = float32(c.Y) - d.clusters[i].h/2
		if d.clusters[i].x < minX {
			minX = d.clusters[i].x
		}
		if d.clusters[i].y < minY {
			minY = d.clusters[i].y
		}
		if d.clusters[i].x+d.clusters[i].w > maxX {
			maxX = d.clusters[i].x + d.clusters[i].w
		}
		if d.clusters[i].y+d.clusters[i].h > maxY {
			maxY = d.clusters[i].y + d.clusters[i].h
		}
	}
	const pad float32 = 12
	shift := func(v, lo float32) float32 { return v - lo + pad }
	for i := range d.nodes {
		d.nodes[i].x = shift(d.nodes[i].x, minX)
		d.nodes[i].y = shift(d.nodes[i].y, minY)
	}
	for i := range d.routes {
		for j := range d.routes[i] {
			d.routes[i][j][0] = shift(d.routes[i][j][0], minX)
			d.routes[i][j][1] = shift(d.routes[i][j][1], minY)
		}
	}
	for i := range d.clusters {
		d.clusters[i].x = shift(d.clusters[i].x, minX)
		d.clusters[i].y = shift(d.clusters[i].y, minY)
	}
	d.width = maxX - minX + 2*pad
	d.height = maxY - minY + 2*pad
	if d.width > width-8 && d.width > 0 {
		scale := (width - 8) / d.width
		for i := range d.nodes {
			n := &d.nodes[i]
			n.x *= scale
			n.y *= scale
			n.w *= scale
			n.h *= scale
		}
		for i := range d.routes {
			for j := range d.routes[i] {
				d.routes[i][j][0] *= scale
				d.routes[i][j][1] *= scale
			}
		}
		for i := range d.clusters {
			d.clusters[i].x *= scale
			d.clusters[i].y *= scale
			d.clusters[i].w *= scale
			d.clusters[i].h *= scale
		}
		d.width *= scale
		d.height *= scale
	}
	return true
}

// layoutFlowManual 是 dagre 不可用时的回退：最长路径分层 + 每层居中。
func layoutFlowManual(d *mermaidDiagram, width float32) {
	// 分层：先 DFS 找出回边（retry --> send 这种）并忽略它们，再做拓扑排序（Kahn）。
	// 不这么做的话：松弛法会把层数一轮轮顶高；而「环上的节点统一扔到最深层」会把
	// 环下游的所有节点压成同一层（实测：6 个节点挤在一排）。
	back := make([]bool, len(d.edges))
	{
		color := make([]uint8, len(d.nodes))
		var visit func(u int)
		visit = func(u int) {
			color[u] = 1
			for i, e := range d.edges {
				if e.from != u {
					continue
				}
				switch color[e.to] {
				case 0:
					visit(e.to)
				case 1:
					back[i] = true // 指向「正在访问」的节点 = 回边
				}
			}
			color[u] = 2
		}
		for i := range d.nodes {
			if color[i] == 0 {
				visit(i)
			}
		}
	}
	for i := range d.nodes {
		d.nodes[i].depth = 0
	}
	indeg := make([]int, len(d.nodes))
	for i, e := range d.edges {
		if back[i] {
			continue
		}
		indeg[e.to]++
	}
	queue := make([]int, 0, len(d.nodes))
	for i := range d.nodes {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	done := 0
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		done++
		for index, e := range d.edges {
			if e.from != i || back[index] {
				continue
			}
			if want := d.nodes[i].depth + 1; want > d.nodes[e.to].depth {
				d.nodes[e.to].depth = want
			}
			indeg[e.to]--
			if indeg[e.to] == 0 {
				queue = append(queue, e.to)
			}
		}
	}
	// 环上的节点（拓扑排不进去）挂在当前最深处之后，别让它们叠在 0 层。
	if done < len(d.nodes) {
		deepest := 0
		for i := range d.nodes {
			if d.nodes[i].depth > deepest {
				deepest = d.nodes[i].depth
			}
		}
		for i := range d.nodes {
			if indeg[i] > 0 {
				d.nodes[i].depth = deepest + 1
			}
		}
	}
	layers := 0
	for i := range d.nodes {
		if d.nodes[i].depth+1 > layers {
			layers = d.nodes[i].depth + 1
		}
	}
	// 每层排一行，宽度累加后整体居中。
	laneCount := make([]int, layers)
	for i := range d.nodes {
		n := &d.nodes[i]
		n.lane = laneCount[n.depth]
		laneCount[n.depth]++
	}
	rowWidth := make([]float32, layers)
	for i := range d.nodes {
		n := &d.nodes[i]
		rowWidth[n.depth] += n.w
		if n.lane > 0 {
			rowWidth[n.depth] += mermaidLaneGap
		}
	}
	axis := float32(0)
	for _, w := range rowWidth {
		if w > axis {
			axis = w
		}
	}
	if d.leftToRight {
		// LR 用同样的分层，把 x/y 对调：整体按行排成列。
		cursorX := float32(0)
		for depth := 0; depth < layers; depth++ {
			cursorY := float32(0)
			for i := range d.nodes {
				n := &d.nodes[i]
				if n.depth != depth {
					continue
				}
				n.x = cursorX
				n.y = cursorY
				cursorY += n.h + mermaidLaneGap
			}
			cursorX += axis + 60
		}
	} else {
		for i := range d.nodes {
			n := &d.nodes[i]
			n.y = float32(n.depth) * (mermaidNodeH + mermaidLayerGap)
		}
		rowY := make([]float32, layers)
		for depth := 0; depth < layers; depth++ {
			cursor := float32(0)
			if axis > rowWidth[depth] {
				cursor = (axis - rowWidth[depth]) / 2
			}
			rowY[depth] = cursor
		}
		for depth := 0; depth < layers; depth++ {
			cursor := rowY[depth]
			for i := range d.nodes {
				n := &d.nodes[i]
				if n.depth != depth {
					continue
				}
				n.x = cursor
				cursor += n.w + mermaidLaneGap
			}
		}
	}
	// 包围盒
	minX, minY := float32(0), float32(0)
	maxX, maxY := float32(0), float32(0)
	for i := range d.nodes {
		n := &d.nodes[i]
		if i == 0 || n.x < minX {
			minX = n.x
		}
		if i == 0 || n.y < minY {
			minY = n.y
		}
		if n.x+n.w > maxX {
			maxX = n.x + n.w
		}
		if n.y+n.h > maxY {
			maxY = n.y + n.h
		}
	}
	for i := range d.nodes {
		d.nodes[i].x -= minX
		d.nodes[i].y -= minY
	}
	// 左右各留 14：右边是跨层边/回边的绕行通道，左边只是对称留白。
	const detourPad float32 = 14
	for i := range d.nodes {
		d.nodes[i].x += detourPad
	}
	d.width = maxX - minX + 2*detourPad
	d.height = maxY - minY
	d.backEdges = back
	// 太宽就等比缩一下（实验版：只缩小，不做换行）
	if d.width > width-8 && d.width > 0 {
		scale := (width - 8) / d.width
		for i := range d.nodes {
			n := &d.nodes[i]
			n.x *= scale
			n.y *= scale
			n.w *= scale
			n.h *= scale
		}
		d.width *= scale
		d.height *= scale
	}
}

func layoutPie(d *mermaidDiagram, width float32) {
	diameter := float32(160)
	if half := (width - 8) * 0.55; half < diameter {
		diameter = half
	}
	if diameter < 80 {
		diameter = 80
	}
	d.pie = pieBitmap(d, int(diameter))
	d.width = diameter + 132
	if d.width > width {
		d.width = width
	}
	if d.title != "" {
		d.pieTop = 24
	}
	d.height = diameter + d.pieTop
}

// pieBitmap 把饼画成位图：逐像素比逐次 Fill 便宜得多，绘制期只贴一次。
func pieBitmap(d *mermaidDiagram, size int) gfx.Bitmap {
	if size < 8 || len(d.slices) == 0 {
		return gfx.Bitmap{}
	}
	total := float32(0)
	for _, s := range d.slices {
		total += s.value
	}
	if total <= 0 {
		return gfx.Bitmap{}
	}
	cum := make([]float32, len(d.slices))
	acc := float32(0)
	for i, s := range d.slices {
		acc += s.value / total * 2 * math.Pi
		cum[i] = acc
	}
	stride := size * 4
	pix := make([]byte, stride*size)
	c := float32(size) / 2
	r := c - 1
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float32(x)+0.5-c, float32(y)+0.5-c
			if dx*dx+dy*dy > r*r {
				continue
			}
			angle := math.Atan2(float64(dy), float64(dx))
			if angle < -math.Pi/2 {
				angle += 2 * math.Pi
			}
			angle += math.Pi / 2 // 从 12 点开始顺时针
			if angle >= 2*math.Pi {
				angle -= 2 * math.Pi
			}
			idx := len(d.slices) - 1
			for i := range cum {
				if float32(angle) <= cum[i] {
					idx = i
					break
				}
			}
			col := mermaidPalette[idx%len(mermaidPalette)]
			o := y*stride + x*4
			pix[o+0] = col[2]
			pix[o+1] = col[1]
			pix[o+2] = col[0]
			pix[o+3] = 0xff
		}
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(d.title))
	return gfx.Bitmap{Pix: pix, Width: size, Height: size, Stride: stride, ID: h.Sum64() | 1, Version: 1}
}

// paintMermaid 把排好的图画到 canvas 上（原点 x,y）。
func paintMermaid(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, theme *ui.Theme) {
	if d == nil {
		return
	}
	switch d.kind {
	case mermaidFlow:
		paintFlow(canvas, d, x, y, theme)
	case mermaidPie:
		paintPie(canvas, d, x, y, theme)
	default:
		canvas.Label(d.note, gfx.Rect{X: x, Y: y + 12, Width: d.width, Height: 20}, gfx.TextStyle{
			Size: 12, Color: theme.MutedForeground,
		})
	}
}

// flowArrow 是一个待画的箭头（节点之后补画，免得被节点边缘盖住）。
// 用方向向量而不是四选一的枚举：dagre 的折线末段可能是斜的，枚举画不准。
type flowArrow struct {
	x, y       float32
	dirX, dirY float32
}

func paintFlow(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, theme *ui.Theme) {
	// 先画连线，节点盖在上面；箭头留到节点之后补（否则被节点边缘盖住）。
	linkColor := theme.MutedForeground
	var arrows []flowArrow
	outSeq := make(map[int]int, len(d.nodes))

	if len(d.clusters) > 0 {
		paintClusters(canvas, d, x, y, theme)
	}
	if len(d.routes) == len(d.edges) && len(d.edges) > 0 {
		// dagre 给了每条边的折点：直接照着画，不用自己算绕行。
		paintFlowRoutes(canvas, d, x, y, linkColor, &arrows, outSeq)
	} else {
		paintFlowEdgesManual(canvas, d, x, y, linkColor, &arrows, outSeq)
	}

	for _, n := range d.nodes {
		bounds := gfx.Rect{X: x + n.x, Y: y + n.y, Width: n.w, Height: n.h}
		// classDef / style 给了值就盖掉主题色，没给的仍跟随明暗主题。
		fill, border, textColor := theme.Card, theme.Border, theme.Foreground
		width := float32(1)
		if n.style.hasFill {
			fill = n.style.fill
		}
		if n.style.hasStroke {
			border = n.style.stroke
		}
		if n.style.hasText {
			textColor = n.style.text
		}
		if n.style.strokeWidth > 0 {
			width = n.style.strokeWidth
		}
		radius := float32(4)
		switch n.shape {
		case shapeStadium:
			radius = bounds.Height / 2
		case shapeRound:
			radius = 10
		}
		if n.style.dashed && n.shape != shapeDiamond {
			canvas.Rounded(bounds, radius, fill)
			dashedRect(canvas, bounds, radius, border)
		} else {
			switch n.shape {
			case shapeDiamond:
				paintDiamond(canvas, bounds, border, fill)
			case shapeStadium:
				canvas.RoundedBorder(bounds, bounds.Height/2, width, border, fill)
			case shapeRound:
				canvas.RoundedBorder(bounds, 10, width, border, fill)
			default:
				canvas.RoundedBorder(bounds, 4, width, border, fill)
			}
		}
		paintNodeLabel(canvas, n, bounds, textColor)
	}
	for _, a := range arrows {
		arrowHead(canvas, a.x, a.y, a.dirX, a.dirY, linkColor)
	}
}

// paintClusters 画 subgraph：淡底 + 虚线框 + 左上角标题。
func paintClusters(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, theme *ui.Theme) {
	for _, c := range d.clusters {
		if c.w <= 0 || c.h <= 0 {
			continue
		}
		// dagre 的包围盒是「内容」的，标题要占一行，所以框往上扩 18px。
		const titleSpace float32 = 18
		bounds := gfx.Rect{X: x + c.x, Y: y + c.y - titleSpace, Width: c.w, Height: c.h + titleSpace}
		canvas.Rounded(bounds, 8, theme.Hover)
		dashedRect(canvas, bounds, 8, theme.Border)
		if c.title != "" {
			canvas.Label(c.title, gfx.Rect{X: bounds.X + 10, Y: bounds.Y + 2, Width: bounds.Width - 20, Height: 16},
				gfx.TextStyle{Size: 11, Color: theme.MutedForeground})
		}
	}
}

// dashedRect 画一圈虚线框（canvas 没有虚线描边，按 4px 一段自己拼）。
func dashedRect(canvas *gfx.Canvas, bounds gfx.Rect, radius float32, color gfx.Color) {
	const dash, gap = 4, 3
	_ = radius
	top, bottom := bounds.Y, bounds.Y+bounds.Height-1
	left, right := bounds.X, bounds.X+bounds.Width-1
	for x := left; x < right; x += dash + gap {
		w := minF(dash, right-x)
		canvas.Fill(gfx.Rect{X: x, Y: top, Width: w, Height: 1}, color)
		canvas.Fill(gfx.Rect{X: x, Y: bottom, Width: w, Height: 1}, color)
	}
	for y := top; y < bottom; y += dash + gap {
		h := minF(dash, bottom-y)
		canvas.Fill(gfx.Rect{X: left, Y: y, Width: 1, Height: h}, color)
		canvas.Fill(gfx.Rect{X: right, Y: y, Width: 1, Height: h}, color)
	}
}

// paintNodeLabel 把（可能多行的）标签画在节点正中。
func paintNodeLabel(canvas *gfx.Canvas, n mermaidNode, bounds gfx.Rect, textColor gfx.Color) {
	const size float32 = 12
	const lineHeight float32 = size * 1.35
	lines := n.lines
	if len(lines) == 0 {
		lines = []string{n.label}
	}
	total := lineHeight * float32(len(lines))
	y := bounds.Y + (bounds.Height-total)/2
	for _, line := range lines {
		canvas.LabelCenter(line, gfx.Rect{X: bounds.X - 4, Y: y, Width: bounds.Width + 8, Height: lineHeight},
			gfx.TextStyle{Size: size, Color: textColor})
		y += lineHeight
	}
}

// paintFlowRoutes 按 dagre 的折点画边：段内水平/垂直走整段矩形，斜段才逐像素补。
func paintFlowRoutes(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, linkColor gfx.Color,
	arrows *[]flowArrow, outSeq map[int]int) {
	for i, e := range d.edges {
		route := d.routes[i]
		if len(route) < 2 {
			continue
		}
		pts := make([][2]float32, len(route))
		for j := range route {
			pts[j] = [2]float32{x + route[j][0], y + route[j][1]}
		}
		// dagre 给的连接点落在节点边界的任意位置，箭头因此会偏在一侧；
		// 先把两端吸附到「节点边的中心」（偏差太大就保留 dagre 的走线）。
		pts = snapRouteEnds(d, e.from, e.to, pts, x, y)
		// dagre 给的 points 是贝塞尔的控制点，直接连折线就是硬邦邦的斜直线；
		// 过一遍 Catmull-Rom 采成密集点再画，拐角才是圆的（对应 mermaid 的 curve: basis）。
		smooth := smoothPath(pts)
		last, prev := smooth[len(smooth)-1], smooth[len(smooth)-2]
		dx, dy := last[0]-prev[0], last[1]-prev[1]
		*arrows = append(*arrows, flowArrow{x: last[0], y: last[1], dirX: dx, dirY: dy})
		// 线画到箭头根部就停：否则 1px 的线会从箭头中间穿出来，看着像没接上。
		strokePath(canvas, trimTail(smooth, 6), linkColor)
		if e.label != "" {
			// 标签放在路径 38% 处旁边：贴第一段会压到节点，放中点又会落在拐弯上。
			mx, my := pointAlong(smooth, 0.5)
			lx, ly := mx+10, my-20
			if d.leftToRight {
				lx, ly = mx-12, my-20
			}
			canvas.Label(e.label, gfx.Rect{X: lx + float32(outSeq[e.from])*18, Y: ly, Width: 60, Height: 16}, gfx.TextStyle{
				Size: 10, Color: linkColor,
			})
			outSeq[e.from]++
		}
	}
}

// paintFlowEdgesManual 是 dagre 不可用时的旧算法（中点折线 + 右侧绕行通道）。
func paintFlowEdgesManual(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, linkColor gfx.Color,
	arrows *[]flowArrow, outSeq map[int]int) {
	channelX := x + d.width - 7
	for index, e := range d.edges {
		from, to := d.nodes[e.from], d.nodes[e.to]
		isBack := index < len(d.backEdges) && d.backEdges[index]
		// 跨多层的正向边与回边都绕到右侧通道，否则会横穿中间层的节点。
		detour := !d.leftToRight && (isBack || to.depth-from.depth > 1)

		if d.leftToRight {
			sx, sy := x+from.x+from.w, y+from.y+from.h/2
			ex, ey := x+to.x, y+to.y+to.h/2
			mx := (sx + ex) / 2
			canvas.Fill(gfx.Rect{X: sx, Y: sy - 0.5, Width: mx - sx, Height: 1}, linkColor)
			canvas.Fill(gfx.Rect{X: mx - 0.5, Y: minF(sy, ey), Width: 1, Height: maxF(sy, ey) - minF(sy, ey)}, linkColor)
			canvas.Fill(gfx.Rect{X: mx, Y: ey - 0.5, Width: ex - mx, Height: 1}, linkColor)
			*arrows = append(*arrows, flowArrow{x: ex - 2, y: ey, dirX: 1})
			if e.label != "" {
				lx, ly := sx+8, sy-18+float32(outSeq[e.from])*14
				outSeq[e.from]++
				canvas.Label(e.label, gfx.Rect{X: lx, Y: ly, Width: 64, Height: 16}, gfx.TextStyle{
					Size: 10, Color: linkColor,
				})
			}
			continue
		}

		if detour {
			if isBack {
				// 回边：从源顶部出去，向上绕到右侧通道，再从目标底部进入（箭头朝上）。
				sx := x + from.x + from.w/2
				syTop := y + from.y
				ex := x + to.x + to.w/2
				toBottom := y + to.y + to.h
				canvas.Fill(gfx.Rect{X: sx - 0.5, Y: syTop - 12, Width: 1, Height: 12}, linkColor)
				canvas.Fill(gfx.Rect{X: sx, Y: syTop - 12.5, Width: channelX - sx, Height: 1}, linkColor)
				canvas.Fill(gfx.Rect{X: channelX - 0.5, Y: toBottom + 12, Width: 1, Height: syTop - 12 - (toBottom + 12)}, linkColor)
				canvas.Fill(gfx.Rect{X: ex, Y: toBottom + 12.5, Width: channelX - ex, Height: 1}, linkColor)
				canvas.Fill(gfx.Rect{X: ex - 0.5, Y: toBottom + 2, Width: 1, Height: 10}, linkColor)
				*arrows = append(*arrows, flowArrow{x: ex, y: toBottom + 2, dirY: -1})
			} else {
				// 跨层正向边：源底 → 下折 → 右侧通道 → 目标上方 → 进节点（箭头朝下）。
				sx := x + from.x + from.w/2
				sy := y + from.y + from.h
				ex := x + to.x + to.w/2
				ey := y + to.y
				canvas.Fill(gfx.Rect{X: sx - 0.5, Y: sy, Width: 1, Height: 12}, linkColor)
				canvas.Fill(gfx.Rect{X: sx, Y: sy + 11.5, Width: channelX - sx, Height: 1}, linkColor)
				canvas.Fill(gfx.Rect{X: channelX - 0.5, Y: ey - 12, Width: 1, Height: sy + 12 - (ey - 12)}, linkColor)
				canvas.Fill(gfx.Rect{X: ex, Y: ey - 12.5, Width: channelX - ex, Height: 1}, linkColor)
				canvas.Fill(gfx.Rect{X: ex - 0.5, Y: ey - 12, Width: 1, Height: 12}, linkColor)
				*arrows = append(*arrows, flowArrow{x: ex, y: ey - 2, dirY: 1})
			}
			if e.label != "" {
				lx := channelX - 22
				ly := y + (from.y+from.h+to.y)/2
				if isBack {
					ly = y + (to.y+to.h+from.y)/2
				}
				canvas.Label(e.label, gfx.Rect{X: lx, Y: ly, Width: 20, Height: 16}, gfx.TextStyle{
					Size: 10, Color: linkColor,
				})
			}
			continue
		}

		sx, sy := x+from.x+from.w/2, y+from.y+from.h
		ex, ey := x+to.x+to.w/2, y+to.y
		my := (sy + ey) / 2
		canvas.Fill(gfx.Rect{X: sx - 0.5, Y: sy, Width: 1, Height: my - sy}, linkColor)
		canvas.Fill(gfx.Rect{X: minF(sx, ex), Y: my - 0.5, Width: maxF(sx, ex) - minF(sx, ex), Height: 1}, linkColor)
		canvas.Fill(gfx.Rect{X: ex - 0.5, Y: my, Width: 1, Height: ey - my}, linkColor)
		*arrows = append(*arrows, flowArrow{x: ex, y: ey - 2, dirY: 1})
		if e.label != "" {
			// 标签贴源节点的出线处；同一源的多条出边按顺序横向错开，避免叠在一起。
			lx, ly := sx+7+float32(outSeq[e.from])*26, sy+8
			outSeq[e.from]++
			canvas.Label(e.label, gfx.Rect{X: lx, Y: ly, Width: 64, Height: 16}, gfx.TextStyle{
				Size: 10, Color: linkColor,
			})
		}
	}
}

// paintDiamond 用逐行扫描画菱形：canvas 没有多边形填充，
// 每行画一条随高度收窄的矩形，再补两端 1px 描边。
func paintDiamond(canvas *gfx.Canvas, bounds gfx.Rect, border, fill gfx.Color) {
	cx := bounds.X + bounds.Width/2
	hw := bounds.Width / 2
	hh := bounds.Height / 2
	for row := 0; row < int(bounds.Height); row++ {
		t := (float32(row) + 0.5 - hh) / hh
		if t < 0 {
			t = -t
		}
		half := hw * (1 - t)
		if half < 0.75 {
			half = 0.75 // 顶点/底点保底，别让尖角整行被丢掉
		}
		y := bounds.Y + float32(row)
		canvas.Fill(gfx.Rect{X: cx - half, Y: y, Width: half * 2, Height: 1}, fill)
		// 描边用 1.6 宽（重叠覆盖）：1px 在非整数 DPR 下会被摊薄到几乎看不见。
		canvas.Fill(gfx.Rect{X: cx - half, Y: y, Width: 1.6, Height: 1}, border)
		canvas.Fill(gfx.Rect{X: cx + half - 1.6, Y: y, Width: 1.6, Height: 1}, border)
	}
}

// paintPie 画饼图：位图 + 标题 + 右侧图例（带百分比）。
func paintPie(canvas *gfx.Canvas, d *mermaidDiagram, x, y float32, theme *ui.Theme) {
	top := y + d.pieTop
	if d.pie.Width > 0 {
		canvas.DrawBitmap(d.pie, gfx.Rect{X: x, Y: top, Width: float32(d.pie.Width), Height: float32(d.pie.Height)})
	}
	if d.title != "" {
		canvas.Label(d.title, gfx.Rect{X: x, Y: y + 2, Width: d.width, Height: 18}, gfx.TextStyle{
			Size: 12, Color: theme.Foreground,
		})
	}
	total := float32(0)
	for _, s := range d.slices {
		total += s.value
	}
	lx := x + float32(d.pie.Width) + 16
	ly := top + 4
	for i, s := range d.slices {
		swatch := mermaidPalette[i%len(mermaidPalette)]
		canvas.Rounded(gfx.Rect{X: lx, Y: ly + 4, Width: 10, Height: 10}, 2,
			gfx.RGBA(swatch[0], swatch[1], swatch[2], 1))
		pct := "0%"
		if total > 0 {
			pct = strconv.Itoa(int(math.Round(float64(s.value/total*100)))) + "%"
		}
		canvas.Label(s.label+"  "+pct, gfx.Rect{X: lx + 16, Y: ly, Width: 120, Height: 18}, gfx.TextStyle{
			Size: 11, Color: theme.Foreground,
		})
		ly += 20
	}
}

// snapRouteEnds 把路由两端吸附到源节点出口、目标节点入口的中心。
// dagre 给的是「边与节点边界的交点」，可能在边的偏侧；差值超过 28px 时说明
// 这条边是绕着走的，保留原样更安全。
func snapRouteEnds(d *mermaidDiagram, from, to int, pts [][2]float32, x, y float32) [][2]float32 {
	if len(pts) < 2 || from >= len(d.nodes) || to >= len(d.nodes) {
		return pts
	}
	const maxShift float32 = 28
	src, dst := d.nodes[from], d.nodes[to]
	var outEntry, inEntry [2]float32
	if d.leftToRight {
		outEntry = [2]float32{x + src.x + src.w, y + src.y + src.h/2}
		inEntry = [2]float32{x + dst.x, y + dst.y + dst.h/2}
	} else {
		outEntry = [2]float32{x + src.x + src.w/2, y + src.y + src.h}
		inEntry = [2]float32{x + dst.x + dst.w/2, y + dst.y}
	}
	out := make([][2]float32, len(pts))
	copy(out, pts)
	if absF(out[0][0]-outEntry[0]) <= maxShift && absF(out[0][1]-outEntry[1]) <= maxShift {
		out[0] = outEntry
	}
	last := len(out) - 1
	if absF(out[last][0]-inEntry[0]) <= maxShift && absF(out[last][1]-inEntry[1]) <= maxShift {
		out[last] = inEntry
	}
	return out
}

// pointAlong 返回路径上按长度比例 frac 处的点（用于给边标签找位置）。
func pointAlong(pts [][2]float32, frac float32) (float32, float32) {
	if len(pts) == 0 {
		return 0, 0
	}
	total := float32(0)
	for i := 1; i < len(pts); i++ {
		total += float32(math.Hypot(float64(pts[i][0]-pts[i-1][0]), float64(pts[i][1]-pts[i-1][1])))
	}
	if total <= 0 {
		return pts[0][0], pts[0][1]
	}
	target := total * frac
	walked := float32(0)
	for i := 1; i < len(pts); i++ {
		dx := pts[i][0] - pts[i-1][0]
		dy := pts[i][1] - pts[i-1][1]
		seg := float32(math.Hypot(float64(dx), float64(dy)))
		if walked+seg >= target && seg > 0.0001 {
			t := (target - walked) / seg
			return pts[i-1][0] + dx*t, pts[i-1][1] + dy*t
		}
		walked += seg
	}
	return pts[len(pts)-1][0], pts[len(pts)-1][1]
}

// trimTail 把路径末端沿最后一段方向回退 dist 像素（给箭头留出位置）。
func trimTail(pts [][2]float32, dist float32) [][2]float32 {
	n := len(pts)
	if n < 2 {
		return pts
	}
	tip, prev := pts[n-1], pts[n-2]
	dx, dy := tip[0]-prev[0], tip[1]-prev[1]
	length := float32(math.Hypot(float64(dx), float64(dy)))
	if length < 0.001 {
		return pts
	}
	dx, dy = dx/length, dy/length
	out := make([][2]float32, n)
	copy(out, pts)
	out[n-1] = [2]float32{tip[0] - dx*dist, tip[1] - dy*dist}
	return out
}

// smoothPath 用 Catmull-Rom 样条把折线采成密集点（每条线段按 3px 一个采样）。
// 端点复制自身，保证起点 / 终点仍落在节点边界上。
func smoothPath(pts [][2]float32) [][2]float32 {
	n := len(pts)
	if n < 3 {
		return pts
	}
	out := make([][2]float32, 0, n*8)
	for i := 0; i < n-1; i++ {
		// 端点用自身的复制，注意不能写 pts[i-1] 再判断 i==0 —— Go 会先求值再判断，直接 -1 越界。
		p0 := pts[0]
		if i > 0 {
			p0 = pts[i-1]
		}
		p1 := pts[i]
		p2 := pts[i+1]
		p3 := pts[n-1]
		if i+2 < n {
			p3 = pts[i+2]
		}
		dx, dy := p2[0]-p1[0], p2[1]-p1[1]
		length := float32(math.Hypot(float64(dx), float64(dy)))
		steps := int(length/1.5) + 1
		for s := 0; s < steps; s++ {
			t := float32(s) / float32(steps)
			out = append(out, catmullRom(p0, p1, p2, p3, t))
		}
	}
	out = append(out, pts[n-1])
	return out
}

// catmullRom 是张力 0.5 的标准 Catmull-Rom 插值（过 p1、p2）。
func catmullRom(p0, p1, p2, p3 [2]float32, t float32) [2]float32 {
	t2, t3 := t*t, t*t*t
	f := func(a, b, c, d float32) float32 {
		return 0.5 * (2*b + (-a+c)*t + (2*a-5*b+4*c-d)*t2 + (-a+3*b-3*c+d)*t3)
	}
	return [2]float32{f(p0[0], p1[0], p2[0], p3[0]), f(p0[1], p1[1], p2[1], p3[1])}
}

// strokePath 把采样路径画成连续线。
//
// 做法：沿路径每 1px 一步，每步落一个 1.6×1.6 的小方块 —— 相邻步必然重叠 0.6px，
// 无论 DPR 是不是整数都不会留缝。之前试过三种「精确」画法（1×1 逐像素、按整数列、
// 整段矩形），在 2560 物理宽 / 1089 逻辑宽的窗口里都会出现隔行丢弃的条纹，看着就是虚线；
// 用重叠覆盖是唯一稳的。代价是线视觉上略粗一点点。
func strokePath(canvas *gfx.Canvas, pts [][2]float32, color gfx.Color) {
	if len(pts) < 2 {
		return
	}
	for i := 1; i < len(pts); i++ {
		x0, y0 := pts[i-1][0], pts[i-1][1]
		x1, y1 := pts[i][0], pts[i][1]
		dx, dy := x1-x0, y1-y0
		length := float32(math.Hypot(float64(dx), float64(dy)))
		steps := int(length) + 1
		for step := 0; step <= steps; step++ {
			t := float32(step) / float32(steps)
			canvas.Fill(gfx.Rect{
				X: x0 + dx*t - 0.5, Y: y0 + dy*t - 0.5,
				Width: 1.6, Height: 1.6,
			}, color)
		}
	}
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// drawLine 画一条任意方向的 1px 线：水平/垂直走整段矩形，斜段逐像素（Bresenham 简化版）。
func drawLine(canvas *gfx.Canvas, x0, y0, x1, y1 float32, color gfx.Color) {
	dx, dy := x1-x0, y1-y0
	ax, ay := dx, dy
	if ax < 0 {
		ax = -ax
	}
	if ay < 0 {
		ay = -ay
	}
	if ax < 0.5 {
		canvas.Fill(gfx.Rect{X: x0 - 0.5, Y: minF(y0, y1), Width: 1, Height: ay}, color)
		return
	}
	if ay < 0.5 {
		canvas.Fill(gfx.Rect{X: minF(x0, x1), Y: y0 - 0.5, Width: ax, Height: 1}, color)
		return
	}
	steps := int(ax)
	if ay > ax {
		steps = int(ay)
	}
	if steps <= 0 {
		return
	}
	for i := 0; i <= steps; i++ {
		t := float32(i) / float32(steps)
		canvas.Fill(gfx.Rect{X: x0 + dx*t, Y: y0 + dy*t, Width: 1, Height: 1}, color)
	}
}

// arrowHead 在 (tipX,tipY) 画一个指向 (dirX,dirY) 的三角箭头。
//
// 之前按「上/下/左/右」四个枚举画，结果三处几何全反了（尖端画成了钝端，
// LR 的图看上去箭头朝左）。改成用方向向量算：尖端在 tip，沿 -dir 展开，
// 宽度从 1 涨到 7 —— 方向对不对由调用方给的向量决定，不会再有镜像错误。
func arrowHead(canvas *gfx.Canvas, tipX, tipY, dirX, dirY float32, color gfx.Color) {
	length := float32(math.Hypot(float64(dirX), float64(dirY)))
	if length < 0.001 {
		return
	}
	dirX /= length
	dirY /= length
	const size float32 = 7
	if dirX == 0 || dirY == 0 {
		// 轴对齐：一层一个矩形，便宜。
		for step := 0; step < 7; step++ {
			t := float32(step) / 6
			cx := tipX - dirX*t*size
			cy := tipY - dirY*t*size
			w := 1 + t*6
			if dirY != 0 {
				canvas.Fill(gfx.Rect{X: cx - w/2, Y: cy, Width: w, Height: 1}, color)
			} else {
				canvas.Fill(gfx.Rect{X: cx, Y: cy - w/2, Width: 1, Height: w}, color)
			}
		}
		return
	}
	// 斜向：沿垂直方向逐点铺（箭头小，几十次 Fill 可以接受）。
	perpX, perpY := -dirY, dirX
	for step := 0; step <= 7; step++ {
		t := float32(step) / 7
		cx := tipX - dirX*t*size
		cy := tipY - dirY*t*size
		half := t * size / 2
		for k := -half; k <= half; k += 0.5 {
			canvas.Fill(gfx.Rect{X: cx + perpX*k, Y: cy + perpY*k, Width: 1, Height: 1}, color)
		}
	}
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
