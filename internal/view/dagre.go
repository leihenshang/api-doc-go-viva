package view

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/dop251/goja"
)

// 布局外包给 dagre（mermaid 自己用的布局库，96KB、纯 JS、无 DOM）：
// 用项目已有的 goja 跑它，拿回节点中心坐标与每条边的折点。
//
// 为什么这么做见 doc/mermaid.md 第八节：mmdc 那套要跑整个 Chromium，
// 而渲染里最难的「布局」其实只是纯计算，一个小 JS 包就够。
//
//go:embed assets/dagre.min.js
var dagreSource string

// dagreFunction 收一个图的 JSON，吐节点中心与边折点。
// multigraph 打开：同一对节点之间的多条边靠 name 区分。
const dagreFunction = `function(spec){
	var g = new dagre.graphlib.Graph({multigraph: true, compound: true});
	g.setGraph({rankdir: spec.rankdir, nodesep: spec.nodesep, ranksep: spec.ranksep, marginx: 0, marginy: 0});
	g.setDefaultEdgeLabel(function(){ return {}; });
	(spec.clusters || []).forEach(function(id){ g.setNode(id, {}); });
	spec.nodes.forEach(function(n){
		g.setNode(n.id, {width: n.w, height: n.h});
		if (n.cluster) { g.setParent(n.id, n.cluster); }
	});
	spec.edges.forEach(function(e, i){ g.setEdge(e.from, e.to, {}, String(i)); });
	dagre.layout(g);
	var out = {nodes: [], edges: [], clusters: []};
	spec.nodes.forEach(function(n){
		var d = g.node(n.id);
		out.nodes.push({id: n.id, x: d ? d.x : 0, y: d ? d.y : 0});
	});
	spec.edges.forEach(function(e, i){
		var d = g.edge({v: e.from, w: e.to, name: String(i)});
		out.edges.push({points: (d && d.points) ? d.points.map(function(p){ return {x: p.x, y: p.y}; }) : []});
	});
	(spec.clusters || []).forEach(function(id){
		var d = g.node(id);
		out.clusters.push({x: d ? d.x : 0, y: d ? d.y : 0, w: d ? d.width : 0, h: d ? d.height : 0});
	});
	return JSON.stringify(out);
}`

var (
	dagreOnce sync.Once
	dagreVM   *goja.Runtime
	dagreErr  error
)

// dagreRuntime 只建一次：编译 96KB JS 约 20ms，之后每次 layout 复用它
// （dagre 每次都建新的 graph 对象，VM 本身没有跨次状态）。
func dagreRuntime() (*goja.Runtime, error) {
	dagreOnce.Do(func() {
		vm := goja.New()
		if _, err := vm.RunString(dagreSource); err != nil {
			dagreErr = err
			return
		}
		dagreVM = vm
	})
	return dagreVM, dagreErr
}

type dagreNodeIn struct {
	ID      string  `json:"id"`
	Cluster string  `json:"cluster,omitempty"`
	Width   float32 `json:"w"`
	Height  float32 `json:"h"`
}

type dagreEdgeIn struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type dagreSpec struct {
	Rankdir  string        `json:"rankdir"`
	Nodesep  float64       `json:"nodesep"`
	Ranksep  float64       `json:"ranksep"`
	Nodes    []dagreNodeIn `json:"nodes"`
	Edges    []dagreEdgeIn `json:"edges"`
	Clusters []string      `json:"clusters,omitempty"`
}

type dagrePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type dagreOut struct {
	Nodes []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"nodes"`
	Edges []struct {
		Points []dagrePoint `json:"points"`
	} `json:"edges"`
	Clusters []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"clusters"`
}

// runDagre 排一遍，返回每个节点的中心坐标与每条边的折点（顺序与入参一致）。
func runDagre(spec dagreSpec) (*dagreOut, error) {
	vm, err := dagreRuntime()
	if err != nil {
		dagreDebug("runtime: %v", err)
		return nil, err
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	// 用全局变量传 JSON 再 JSON.parse：直接拼字符串字面量容易在转义上踩坑
	// （实测 spec 会变成 undefined，报 forEach of undefined）。
	if err := vm.Set("__mermaidSpec", string(raw)); err != nil {
		return nil, err
	}
	result, err := vm.RunString("(" + dagreFunction + ")(JSON.parse(__mermaidSpec))")
	if err != nil {
		dagreDebug("run: %v", err)
		return nil, err
	}
	text, ok := result.Export().(string)
	if !ok || text == "" {
		return nil, errors.New("dagre 没有返回结果")
	}
	var out dagreOut
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		dagreDebug("unmarshal: %v (raw=%s)", err, text[:minInt(len(text), 200)])
		return nil, err
	}
	if len(out.Nodes) != len(spec.Nodes) {
		return nil, errors.New("dagre 返回的节点数不对")
	}
	return &out, nil
}

// dagreDebug 只在 VIVA_DEBUG_MERMAID 开着时打点，方便定位「dagre 起不来」的原因。
func dagreDebug(format string, args ...any) {
	if os.Getenv("VIVA_DEBUG_MERMAID") != "" {
		fmt.Fprintf(os.Stderr, "[dagre] "+format+"\n", args...)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
