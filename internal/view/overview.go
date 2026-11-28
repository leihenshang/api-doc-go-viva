package view

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/card"
	"github.com/vivaui/viva/ui/overflowlist"
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
		// 主操作固定在最左，其余随宽度收进「⋯」（v0.6.10 的 ui/overflowlist）：
		// 七个按钮在窄窗口里会挤成一排省略号，折叠后放不下的自动进菜单。
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			if ui.Button(c, "新建请求").Icon(IconPlus).Weight(gfx.WeightRegular).Clicked() {
				m.openNewRequest()
			}
			if m.overviewMore == nil {
				m.overviewMore = overflowlist.New()
			}
			m.overviewMore.View(c, "更多动作", overflowlist.Spec{
				Items: overviewActions, Min: 2, Gap: px(8), MoreLabel: "更多动作",
			}).Grow(1)
			if index, ok := m.overviewMore.Clicked(); ok {
				switch index {
				case 0:
					c.After(m.createDoc)
				case 1:
					c.After(m.chooseCollection)
				case 2:
					c.After(m.openDefaults)
				case 3:
					m.openSettings()
				case 4:
					c.After(func() { m.openExport("md") })
				case 5:
					c.After(func() { m.openExport("html") })
				}
			}
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

// overviewActions 是概览页「更多动作」里的条目，顺序必须与 overviewPage 里的
// switch index 一一对应（0 新建文档 / 1 打开集合 / 2 公共参数 / 3 设置 / 4 导出 MD / 5 导出 HTML）。
var overviewActions = []overflowlist.Item{
	{Label: "新建文档", Icon: IconFilePlus},
	{Label: "打开集合", Icon: IconFolderOpen},
	{Label: "公共参数", Icon: IconSliders},
	{Label: "设置", Icon: IconGear},
	{Label: "导出 Markdown", Icon: IconImport},
	{Label: "导出 HTML", Icon: IconGlobe},
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
