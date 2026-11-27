package view

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func probeCanvas() *gfx.Canvas {
	var cv gfx.Canvas
	cv.SetMeasurer(func(v string, size, weight float32) float32 {
		w := float32(0)
		for _, r := range v {
			if r > 0x2e80 {
				w += size
			} else {
				w += size * 0.55
			}
		}
		return w
	})
	return &cv
}

// 长标签要折行，subgraph 要成组并算出包围盒。
func TestDagreFlowWrapAndSubgraph(t *testing.T) {
	cv := probeCanvas()
	src := `flowchart TD
    subgraph 请求阶段
        A[读取配置文件] --> B[解析请求参数并校验必填项以及类型是否合法]
    end
    subgraph 响应阶段
        C[组装响应体] --> D[写回磁盘并返回成功与耗时统计信息]
    end
    B --> C`
	d := parseMermaid(src)
	layoutMermaid(d, cv, 700)
	if len(d.clusters) != 2 {
		t.Fatalf("应有 2 个 subgraph，实际 %d", len(d.clusters))
	}
	for _, c := range d.clusters {
		t.Logf("cluster %q x=%.0f y=%.0f w=%.0f h=%.0f", c.title, c.x, c.y, c.w, c.h)
		if c.w <= 0 || c.h <= 0 {
			t.Fatalf("cluster %q 没有包围盒", c.title)
		}
	}
	if len(d.routes) == 0 || len(d.routes[0]) < 2 {
		t.Fatalf("边折点缺失")
	}
	for _, n := range d.nodes {
		t.Logf("node %q lines=%d w=%.0f h=%.0f cluster=%q at %.0f,%.0f",
			n.label, len(n.lines), n.w, n.h, n.cluster, n.x, n.y)
	}
	// 长标签必须折成多行，且节点高度随之变高
	long := false
	for _, n := range d.nodes {
		if len(n.lines) > 1 && n.h > mermaidNodeH {
			long = true
		}
	}
	if !long {
		t.Fatalf("长标签没有折行：%+v", d.nodes)
	}
	// 每个节点都该落在自己的 cluster 里（按中心点判断）
	for _, n := range d.nodes {
		if n.cluster == "" {
			t.Fatalf("节点 %q 没有归属", n.label)
		}
		cx, cy := n.x+n.w/2, n.y+n.h/2
		found := false
		for _, c := range d.clusters {
			if cx >= c.x && cx <= c.x+c.w && cy >= c.y && cy <= c.y+c.h {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("节点 %q 的中心 (%.0f,%.0f) 不在任何 cluster 里：%+v", n.label, cx, cy, d.clusters)
		}
	}
}
