package view

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestMermaidParsingSmoke(t *testing.T) {
	src := "前言\n\n```mermaid\nflowchart TD\n A[开始] --> B{判断}\n B -->|是| C[处理]\n```\n\n中间段落\n\n```mermaid\npie title 分布\n \"GET\" : 3\n \"POST\" : 1\n```\n\n结尾\n"
	segs := splitMermaidSegments(src)
	if len(segs) != 5 {
		t.Fatalf("应切成 5 段（文本/图/文本/图/文本），实际 %d", len(segs))
	}
	if segs[0].mermaid || !segs[1].mermaid || segs[2].mermaid || !segs[3].mermaid || segs[4].mermaid {
		t.Fatalf("段类型不对：%+v", segs)
	}
	flow := parseMermaid(segs[1].text)
	if flow.kind != mermaidFlow || len(flow.nodes) != 3 || len(flow.edges) != 2 {
		t.Fatalf("flowchart 解析：kind=%v nodes=%d edges=%d", flow.kind, len(flow.nodes), len(flow.edges))
	}
	if flow.nodes[1].label != "判断" {
		t.Fatalf("节点文字应取括号里的内容，实际 %q", flow.nodes[1].label)
	}
	pie := parseMermaid(segs[3].text)
	if pie.kind != mermaidPie || len(pie.slices) != 2 || pie.title != "分布" {
		t.Fatalf("pie 解析：kind=%v slices=%d title=%q", pie.kind, len(pie.slices), pie.title)
	}
	// 不支持的语法要有说明，而不是空白
	bad := parseMermaid("sequenceDiagram\n Alice->>Bob: hi\n")
	if bad.kind != mermaidUnsupported || bad.note == "" {
		t.Fatalf("不支持语法应带说明：kind=%v note=%q", bad.kind, bad.note)
	}
	// 没有 mermaid 块时不分段（保持原快路径）
	if segs := splitMermaidSegments("普通\n\n文本\n"); segs != nil {
		t.Fatalf("没有 mermaid 时不该切段：%+v", segs)
	}
}

// classDef / class / style / :::cls 四种写法都要落到节点样式上。
func TestMermaidClassDefAndStyle(t *testing.T) {
	src := `flowchart TD
    classDef warn fill:#fff3e0,stroke:#e65100,stroke-width:2px,color:#4e342e
    classDef dashed fill:#ffffff,stroke:#7b1fa2,stroke-dasharray:5 4
    A[一] --> B[二]
    class B warn
    style A fill:#e3f2fd
    C[三]:::dashed --> D[四]`
	d := parseMermaid(src)
	if d.kind != mermaidFlow {
		t.Fatalf("应识别为 flowchart，实际 %v", d.kind)
	}
	byLabel := map[string]mermaidNode{}
	for _, n := range d.nodes {
		byLabel[n.label] = n
	}
	b, ok := byLabel["二"]
	if !ok {
		t.Fatalf("没解析到节点「二」：%+v", d.nodes)
	}
	if !b.style.hasFill || !b.style.hasStroke || !b.style.hasText {
		t.Fatalf("class 没套上 fill/stroke/color：%+v", b.style)
	}
	if b.style.strokeWidth != 2 {
		t.Fatalf("stroke-width 应为 2，实际 %v", b.style.strokeWidth)
	}
	if want := gfx.RGBA(0xe6, 0x51, 0x00, 1); b.style.stroke != want {
		t.Fatalf("stroke 颜色不对：%+v want %+v", b.style.stroke, want)
	}
	// style 指令只改 fill，不该把其它项也清掉
	a, ok := byLabel["一"]
	if !ok {
		t.Fatalf("没解析到节点「一」")
	}
	if !a.style.hasFill || a.style.hasStroke {
		t.Fatalf("style 应只给 fill：%+v", a.style)
	}
	// :::cls 的虚线
	c, ok := byLabel["三"]
	if !ok {
		t.Fatalf("没解析到节点「三」")
	}
	if !c.style.dashed || c.className != "dashed" {
		t.Fatalf(":::dashed 没生效：className=%q dashed=%v", c.className, c.style.dashed)
	}
}

// smoothPath 曾经在 i==0 时直接取 pts[i-1]，Go 先求值再判断，一上来就是 -1 越界 panic；
// 这条用例守住三种点数（2 / 3 / 退化直线）。
func TestMermaidSmoothPath(t *testing.T) {
	cases := [][][2]float32{
		{{0, 0}, {10, 10}},
		{{0, 0}, {10, 0}, {20, 10}},
		{{0, 0}, {0, 10}},
		{{0, 0}, {10, 0}, {20, 0}, {30, 10}},
	}
	for i, pts := range cases {
		out := smoothPath(pts)
		if len(out) < len(pts) {
			t.Fatalf("case %d：采样后点数变少 %d -> %d", i, len(pts), len(out))
		}
		// 首末点必须落在原点上（否则线头会离节点有缝）
		if out[0] != pts[0] || out[len(out)-1] != pts[len(pts)-1] {
			t.Fatalf("case %d：端点漂了，首 %v 末 %v", i, out[0], out[len(out)-1])
		}
	}
}
