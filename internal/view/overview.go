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
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			if ui.Button(c, "新建请求").Icon(IconPlus).Weight(gfx.WeightRegular).Clicked() {
				m.openNewRequest()
			}
			if ui.Button(c, "新建文档").Icon(IconFilePlus).Secondary().Weight(gfx.WeightRegular).Clicked() {
				c.After(m.createDoc)
			}
			if ui.Button(c, "打开集合").Icon(IconFolderOpen).Secondary().Weight(gfx.WeightRegular).Clicked() {
				c.After(m.chooseCollection)
			}
			if ui.Button(c, "公共参数").Icon(IconGear).Secondary().Weight(gfx.WeightRegular).
				Tooltip("集合级默认请求头与认证").Clicked() {
				c.After(m.openDefaults)
			}
			if ui.Button(c, "设置").Icon(IconGear).Secondary().Weight(gfx.WeightRegular).Clicked() {
				m.openSettings()
			}
		})
		// 导出接口文档：菜单里也有（顶部「⋯」），但集合详情是「我要把这套接口交出去」时
		// 最先看的地方，入口不该藏起来。
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			if ui.Button(c, "导出 Markdown").Icon(IconImport).Secondary().Weight(gfx.WeightRegular).
				Tooltip("把整套集合导出成一份 Markdown 接口文档，可勾选范围").Clicked() {
				c.After(func() { m.openExport("md") })
			}
			if ui.Button(c, "导出 HTML").Icon(IconImport).Secondary().Weight(gfx.WeightRegular).
				Tooltip("把整套集合导出成单文件 HTML 接口文档，可勾选范围").Clicked() {
				c.After(func() { m.openExport("html") })
			}
			ui.Text(c, "导出前可勾选要包含的分组与请求").FontSize(fs(11)).Muted()
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
