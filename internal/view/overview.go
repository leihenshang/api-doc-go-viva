package view

import (
	"strconv"

	"github.com/vivaui/viva/ui"
)

// overviewPage 是固定「集合」页：四个数字卡 + 常用动作，尺寸对齐参考图。
func (m *Model) overviewPage(c ui.C) {
	theme := c.Theme()
	ui.Column(c).Fill().Pad(24).Gap(16).Children(func() {
		ui.Text(c, m.col.Name).FontSize(24).Bold()
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			m.statCard(c, "目录", m.col.Dir)
			m.statCard(c, "请求", strconv.Itoa(len(m.col.Entries)))
			m.statCard(c, "变量环境", strconv.Itoa(len(m.ui.envs)))
			m.statCard(c, "存储格式", "纯文本 YAML")
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if ui.Button(c, "新建请求").Icon(IconPlus).Clicked() {
				m.openNewRequest()
			}
			if ui.Button(c, "新建文档").Icon(IconFilePlus).Secondary().Clicked() {
				c.After(m.createDoc)
			}
			if ui.Button(c, "打开集合").Icon(IconFolderOpen).Secondary().Clicked() {
				c.After(m.chooseCollection)
			}
			if ui.Button(c, "公共参数").Icon(IconSliders).Secondary().
				Tooltip("集合级默认请求头与认证").Clicked() {
				c.After(m.openDefaults)
			}
			if ui.Button(c, "设置").Icon(IconSliders).Secondary().Clicked() {
				m.openSettings()
			}
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			for _, hint := range overviewHints {
				ui.Text(c, hint).FontSize(10).Mono().Muted().
					PadX(8).PadY(4).Radius(theme.Metrics.RadiusControl).
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

// statCard 是一张数字卡：标签在上、值在下，卡片底用组件目录的 Card。
func (m *Model) statCard(c ui.C, label, value string) {
	ui.Column(c).Width(0).Grow(1).Height(64).Pad(12).Gap(6).Draw(cardDraw).Children(func() {
		ui.Text(c, label).FontSize(11).Muted()
		ui.Text(c, value).FontSize(13).Ellipsis().MaxLines(1)
	})
}
