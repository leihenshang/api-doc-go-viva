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
	m.openCollectionDialogs(c)
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
	m.applySettingsEdits()
}

// envDialog 是「环境变量」：左侧环境列表，右侧变量表；敏感值单独写 *.secrets.yml。
func (m *Model) envDialog(c ui.C) {
	theme := c.Theme()
	ui.ModalWith(c, &m.ui.envOpen, "环境变量", ui.ModalSpec{Width: 880, Height: 520}, func() {
		ui.Row(c).FillWidth().Height(400).Gap(16).Children(func() {
			ui.Column(c).Width(180).Gap(6).Children(func() {
				for _, name := range m.ui.envs {
					current := name
					active := current == m.ui.envSel
					row := ui.Row(c).FillWidth().PadX(10).PadY(5).Gap(6).Items(ui.Center).
						Radius(theme.Metrics.RadiusControl).
						Hover(ui.Look{}.Background(theme.Hover)).
						Hit(ui.ID(c), "环境 "+current)
					if active {
						row.Background(m.pal.active)
					}
					row.Children(func() {
						text := ui.Text(c, current).FontSize(12)
						if active {
							text.Color(theme.Primary)
						}
						ui.Spacer(c)
						if active {
							if ui.Button(c, "删除 "+current).Icon(IconTrash).IconOnly().Ghost().Tooltip("删除环境").Clicked() {
								m.ui.envPick = current
								m.ui.envDeleteOpen = true
							}
						}
					})
					if row.Clicked() && !active {
						c.After(func() { m.openEnvEditorFor(current) })
					}
				}
				if ui.Button(c, "新建环境").Icon(IconPlus).Outline().Dashed().Clicked() {
					m.ui.newEnvOpen = true
					m.ui.newEnvErr = ""
				}
				ui.Spacer(c)
			})
			ui.Divider(c)
			ui.Column(c).Grow(1).Gap(10).Children(func() {
				ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, "环境名").FontSize(11).Muted().Width(52)
					ui.TextField(c, &m.ui.envNameEdit, "环境名", "dev").Width(220)
				})
				ui.Text(c, "敏感值的明文保存在 environments/<环境>.secrets.yml（已加入 .gitignore）").
					FontSize(11).Muted()
				ui.Column(c).FillWidth().Pad(8).Gap(6).Radius(theme.Metrics.RadiusControl).
					Border(1, m.pal.border).Children(func() {
					ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
						ui.Text(c, "启用").FontSize(11).Muted().Width(36)
						ui.Text(c, "变量名").FontSize(11).Muted().Width(150)
						ui.Text(c, "值").FontSize(11).Muted().Grow(1)
						ui.Text(c, "敏感值").FontSize(11).Muted().Width(56)
						ui.Slot(c).Width(28)
					})
					for index, row := range m.ui.envRows {
						slot := index
						ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
							ui.Checkbox(c, &row.enabled, "")
							ui.TextField(c, &row.name, "变量名", "host").Width(150)
							ui.TextField(c, &row.value, "值", "http://127.0.0.1:8080").Grow(1)
							ui.Checkbox(c, &row.secret, "")
							if ui.Button(c, "删除变量").Icon(IconX).IconOnly().Ghost().Tooltip("删除变量").Clicked() {
								c.After(func() {
									m.ui.envRows = append(m.ui.envRows[:slot], m.ui.envRows[slot+1:]...)
								})
							}
						})
					}
				})
				ui.Row(c).FillWidth().Children(func() {
					if ui.Button(c, "添加变量").Icon(IconPlus).Ghost().Clicked() {
						m.ui.envRows = append(m.ui.envRows, &kvRow{enabled: true})
					}
				})
				if m.ui.envErr != "" {
					ui.Text(c, m.ui.envErr).FontSize(11).Color(theme.Danger)
				}
				ui.Spacer(c)
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envOpen = false
			}
			if ui.Button(c, "保存").Clicked() {
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

// openCollectionDialogs 是「打开集合」的三步：手输路径兜底、初始化确认、错误提示。
func (m *Model) openCollectionDialogs(c ui.C) {
	ui.Modal(c, &m.ui.pathOpen, "打开集合", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "输入集合目录的绝对路径（目录里应有 opencollection.yml）。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.pathText, "集合目录", "/home/you/collections/demo").FillWidth()
			if m.ui.openErr != "" {
				ui.Text(c, m.ui.openErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.pathOpen = false
			}
			if ui.Button(c, "打开").Clicked() {
				c.After(m.openPathInput)
			}
		})
	})

	ui.Modal(c, &m.ui.initOpen, "初始化集合", func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, m.ui.pendingDir).FontSize(11).Muted().Mono()
			ui.Text(c, "这个目录里没有 opencollection.yml。初始化会在目录里写一份清单，").FontSize(12)
			ui.Text(c, "目录里已有的 .yml 会被识别为本项目的接口。取消则不载入、不写盘。").FontSize(12)
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.initOpen = false
			}
			if ui.Button(c, "初始化并打开").Clicked() {
				c.After(m.confirmInit)
			}
		})
	})

	ui.Modal(c, &m.ui.envRenameOpen, "重命名环境", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "会同时重命名 <名称>.yml 与 <名称>.secrets.yml。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.envRenameName, "新环境名", "staging").FillWidth()
			if m.ui.envErr != "" {
				ui.Text(c, m.ui.envErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envRenameOpen = false
			}
			if ui.Button(c, "重命名环境").Clicked() {
				c.After(m.renameEnv)
			}
		})
	})

	ui.Modal(c, &m.ui.envDeleteOpen, "删除环境", func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "环境："+m.ui.envPick).FontSize(12).Bold()
			ui.Text(c, "主文件与密钥文件都会移进集合的 .trash/，不会直接删除。").FontSize(11).Muted()
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envDeleteOpen = false
			}
			if ui.Button(c, "删除环境").Destructive().Clicked() {
				c.After(m.deleteEnv)
			}
		})
	})
}
