package view

import (
	"github.com/vivaui/viva/ui"
)

// dialogs 声明三个弹窗：新建请求、重命名、设置。它们叠在整窗之上，最后声明。
func (m *Model) dialogs(c ui.C) {
	m.newRequestDialog(c)
	m.renameDialog(c)
	m.settingsDialog(c)
}

// newRequestDialog 是「新建请求」：请求名、方法、地址，创建后写进集合并被选中。
func (m *Model) newRequestDialog(c ui.C) {
	ui.Modal(c, &m.ui.newReqOpen, "新建请求", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "请求会写进集合目录，文件名取请求名；重名自动加 -2。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.newName, "请求名", "例如：获取用户列表").FillWidth()
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "方法").FontSize(11).Muted()
				ui.Select(c, &m.ui.newMethod, httpMethods, nil).Width(120)
			})
			ui.TextField(c, &m.ui.newURL, "地址", "{{host}}/api/users").FillWidth()
			if m.ui.newErr != "" {
				ui.Text(c, m.ui.newErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.newReqOpen = false
			}
			if ui.Button(c, "创建").Clicked() {
				c.After(m.createRequest)
			}
		})
	})
}

// renameDialog 是「重命名」：只改显示名，文件名不动（与客户端一致）。
func (m *Model) renameDialog(c ui.C) {
	ui.Modal(c, &m.ui.renameOpen, "重命名", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "只改显示名，文件名保持不变。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.renameName, "名称", "新的显示名").FillWidth()
			if m.ui.renameErr != "" {
				ui.Text(c, m.ui.renameErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.renameOpen = false
			}
			if ui.Button(c, "保存名称").Clicked() {
				c.After(m.renameEntry)
			}
		})
	})
}

// settingsDialog 是设置：外观（立即生效并落盘）、集合信息与输入法状态。
func (m *Model) settingsDialog(c ui.C) {
	ui.Modal(c, &m.ui.settingsOpen, "设置", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "外观").FontSize(12).Bold().Muted()
			ui.Choice(c, &m.ui.themePick, themeOptions, func(i int) string { return themeNames[i] })
			ui.Divider(c)
			ui.Text(c, "集合").FontSize(12).Bold().Muted()
			ui.Text(c, m.ui.collectionName).FontSize(13)
			ui.Text(c, m.ui.dirLine).FontSize(11).Muted().Mono()
			ui.Text(c, m.ui.envLine).FontSize(11).Muted()
			ui.Divider(c)
			ui.Text(c, "输入法").FontSize(12).Bold().Muted()
			ui.Text(c, m.ui.imeDetail).FontSize(11).Muted()
			ui.Text(c, "Viva 的 Linux 后端只支持 IBus；复制粘贴中文可用（Ctrl+C / Ctrl+V）。").FontSize(11).Muted()
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "完成").Clicked() {
				m.ui.settingsOpen = false
			}
		})
	})
	// Choice 在声明时就写回了 themePick：这里跟随切换，保证点一下立即生效。
	// 只在面板开着时同步，否则会拿旧 themePick 把工具条的切换撤销掉。
	if m.ui.settingsOpen && (m.ui.themePick == 1) != m.isLight() {
		m.setTheme(m.ui.themePick == 1)
	}
}
