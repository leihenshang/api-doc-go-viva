package view

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/card"
)

// overviewPage 是固定「集合」页：四个数字卡 + 常用动作，尺寸对齐参考图。
func (m *Model) overviewPage(c ui.C) {
	theme := c.Theme()
	ui.Column(c).Fill().Pad(px(24)).Gap(px(16)).Children(func() {
		ui.Text(c, m.col.Name).FontSize(fs(24)).Bold()
		ui.Row(c).FillWidth().Gap(px(12)).Children(func() {
			m.statCard(c, "目录", m.col.Dir)
			m.statCard(c, "请求", strconv.Itoa(len(m.col.Entries)))
			m.statCard(c, "变量环境", strconv.Itoa(len(m.ui.envs)))
			m.statCard(c, "存储格式", "纯文本 YAML")
		})
		// 动作行：主操作「新建请求」固定，其余是「图标 + 文字」按钮，放不下的收进「⋯」。
		// 没有用 ui/overflowlist —— 它把每个按钮硬编码成 IconOnly，而这一页要的是图标带文字。
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			if ui.Button(c, "新建请求").Icon(IconPlus).Weight(gfx.WeightRegular).Clicked() {
				m.openNewRequest()
			}
			m.overviewActions(c)
		})
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			for _, hint := range overviewHints {
				ui.Text(c, hint).FontSize(fs(10)).Mono().Muted().
					PadX(8).PadY(px(4)).Radius(theme.Metrics.RadiusControl).
					Background(theme.Muted)
			}
		})
		ui.Text(c, "提示：左侧树里点请求会在标签中打开；未保存的临时请求可用标签栏右侧「+」新建。").
			FontSize(11).Muted().Color(theme.MutedForeground)
		ui.Spacer(c)
	})
}

// overviewAction 是概览页的一个次要动作：图标 + 文字，附带执行函数。
type overviewAction struct {
	label string
	icon  gfx.Icon
	run   func(m *Model, c ui.C)
}

// overviewActions 是「新建请求」右侧的次要动作，顺序就是显示顺序。
var overviewActions = []overviewAction{
	{"新建文档", IconFilePlus, func(m *Model, c ui.C) { c.After(m.createDoc) }},
	{"打开集合", IconFolderOpen, func(m *Model, c ui.C) { c.After(m.chooseCollection) }},
	{"公共参数", IconSliders, func(m *Model, c ui.C) { c.After(m.openDefaults) }},
	{"设置", IconGear, func(m *Model, c ui.C) { m.openSettings() }},
	{"导出 Markdown", IconImport, func(m *Model, c ui.C) { c.After(func() { m.openExport("md") }) }},
	{"导出 HTML", IconGlobe, func(m *Model, c ui.C) { c.After(func() { m.openExport("html") }) }},
}

// overviewVisibleCount 返回行内放得下几个次要动作（放不下时给「⋯」留位置）。
// 抽成纯函数是为了能单测：宽度估算依赖 canvas，显示数量不该依赖渲染。
func overviewVisibleCount(avail, gap float32, measure func(overviewAction) float32) int {
	n := len(overviewActions)
	if avail <= 0 || n == 0 {
		return 0
	}
	total := func(count int) float32 {
		var w float32
		for i := 0; i < count; i++ {
			w += measure(overviewActions[i])
		}
		if count > 1 {
			w += gap * float32(count-1)
		}
		return w
	}
	moreWidth := measure(overviewAction{label: "更多"}) + gap
	for k := n; k >= 1; k-- {
		need := total(k)
		if k < n {
			need += moreWidth
		}
		if need <= avail {
			return k
		}
	}
	return 0
}

// overviewActions 画次要动作：按当前宽度决定行内放几个，其余进「⋯」菜单。
// 宽度用 canvas 量文字 + 图标 + 内边距估算，不用 overflowlist 的「每个都当方形图标」假设。
func (m *Model) overviewActions(c ui.C) {
	canvas := c.Canvas()
	if canvas == nil || len(overviewActions) == 0 {
		return
	}
	measure := func(action overviewAction) float32 {
		// Button 的实际宽度：水平内边距 + 图标 + 图标与文字的间距 + 文字宽。
		// 这几个常量比看起来的要大一点 —— 估小了会让动作行越过内容区右边界
		// （TestMCPContentKeepsClearOfScrollbar 就是量这个的）。
		return 36 + 16 + 8 + canvas.TextWidth(action.label, fs(13))
	}
	gap := px(8)
	// 可用宽度直接算出来：概览页两边的 Pad(24) +「新建请求」占的那一段。
	// 不用 c.AtLeast —— 它读的是上一帧布局，静态页面不重绘时会一直停在初始的「放不下」。
	avail := m.paneWidth(c) - 2*px(24) - measure(overviewAction{label: "新建请求"}) - gap
	shown := overviewVisibleCount(avail, gap, measure)
	for i := 0; i < shown; i++ {
		action := overviewActions[i]
		if ui.Button(c, action.label).Icon(action.icon).Secondary().Weight(gfx.WeightRegular).
			Tooltip(action.label).Clicked() {
			action.run(m, c)
		}
	}
	if shown >= len(overviewActions) {
		return
	}
	// 溢出菜单：剩下的一律列进「⋯」，名字完整可读。
	more := ui.Button(c, "更多动作").Icon(IconEllipsis).Secondary().Ghost().IconOnly().Tooltip("更多动作")
	if more.Clicked() {
		c.After(func() { m.overviewMoreOpen = !m.overviewMoreOpen })
	}
	if !m.overviewMoreOpen {
		return
	}
	ui.Menu(c, more, &m.overviewMoreOpen, func(menu *ui.MenuItems) {
		for i := shown; i < len(overviewActions); i++ {
			action := overviewActions[i]
			if menu.Item(action.label).Icon(action.icon).Clicked() {
				action.run(m, c)
			}
		}
	})
}

// overviewHints 是概览页的快捷键提示（包级切片，避免每帧分配）。
var overviewHints = []string{"Ctrl+K 命令面板", "Ctrl+S 保存", "Ctrl+Enter 发送"}

// statCard 是一张数字卡：标签作卡片标题、值作正文。
//
// v0.6.9 起用页面级的 `ui/card`（原来是 components.Card 的 canvas 钩子 + 手写内边距）：
// Small 是紧凑尺寸，卡片的底色、圆角、内边距与阴影都跟主题走，不用再自己对一遍。
func (m *Model) statCard(c ui.C, label, value string) {
	card.View(c, card.Spec{Title: label, Size: card.Small}, func() {
		ui.Text(c, value).FontSize(fs(13)).Ellipsis().MaxLines(1)
	}).Width(0).Grow(1)
}
