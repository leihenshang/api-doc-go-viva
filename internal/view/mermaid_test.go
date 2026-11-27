package view

import "testing"

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
