package view

import (
	"github.com/vivaui/viva/ui"
)

// dialogs 声明三个弹窗：新建请求、重命名、设置。它们叠在整窗之上，最后声明。
func (m *Model) dialogs(c ui.C) {
	m.newRequestDialog(c)
	m.renameDialog(c)
	m.settingsDialog(c)
	m.envDialog(c)
	m.newEnvDialog(c)
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
			ui.Text(c, "请求").FontSize(12).Bold().Muted()
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "默认超时（秒）").FontSize(11).Muted()
				ui.TextField(c, &m.ui.timeoutText, "默认超时", "20").Width(90).Changed()
				ui.Spacer(c)
				ui.Switch(c, &m.ui.follow, "跟随重定向").Changed()
			})
			ui.Divider(c)
			ui.Text(c, "环境").FontSize(12).Bold().Muted()
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Select(c, &m.ui.envPick, m.ui.envs, nil).Width(170)
				if ui.Button(c, "编辑变量").Clicked() {
					c.After(m.openEnvEditor)
				}
				if ui.Button(c, "新建环境").Clicked() {
					m.ui.newEnvOpen = true
					m.ui.newEnvErr = ""
				}
			})
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
	if !m.ui.settingsOpen {
		return
	}
	// Choice 在声明时就写回了 themePick：这里跟随切换，保证点一下立即生效。
	// 只在面板开着时同步，否则会拿旧 themePick 把工具条的切换撤销掉。
	if (m.ui.themePick == 1) != m.isLight() {
		m.setTheme(m.ui.themePick == 1)
	}
	if m.ui.envPick != m.envName {
		m.applyEnv(m.ui.envPick)
	}
	m.applySettingsEdits()
}

// envDialog 编辑环境里的普通变量；密钥变量只显示数量（值由客户端管理）。
func (m *Model) envDialog(c ui.C) {
	ui.Modal(c, &m.ui.envOpen, "编辑环境变量", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "环境："+m.ui.envPick).FontSize(12).Bold()
			if m.ui.envSecretLine != "" {
				ui.Text(c, m.ui.envSecretLine).FontSize(11).Muted()
			}
			for index, row := range m.ui.envRows {
				slot := index
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.Checkbox(c, &row.enabled, "")
					ui.TextField(c, &row.name, "名称", "host").Width(160)
					ui.TextField(c, &row.value, "值", "http://127.0.0.1:8080").Grow(1)
					if ui.Button(c, "删除").Icon(IconTrash).IconOnly().Clicked() {
						m.ui.envRows = append(m.ui.envRows[:slot], m.ui.envRows[slot+1:]...)
					}
				})
			}
			ui.Row(c).FillWidth().Children(func() {
				if ui.Button(c, "添加变量").Icon(IconPlus).Clicked() {
					m.ui.envRows = append(m.ui.envRows, &kvRow{enabled: true})
				}
			})
			if m.ui.envErr != "" {
				ui.Text(c, m.ui.envErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envOpen = false
			}
			if ui.Button(c, "保存变量").Clicked() {
				c.After(m.saveEnv)
			}
		})
	})
}

// newEnvDialog 新建一个空环境（同名拒绝，不覆盖）。
func (m *Model) newEnvDialog(c ui.C) {
	ui.Modal(c, &m.ui.newEnvOpen, "新建环境", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "环境是 environments/<名称>.yml，与客户端同一个位置。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.newEnvName, "环境名", "例如：staging").FillWidth()
			if m.ui.newEnvErr != "" {
				ui.Text(c, m.ui.newEnvErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.newEnvOpen = false
			}
			if ui.Button(c, "创建环境").Clicked() {
				c.After(m.createEnv)
			}
		})
	})
}
