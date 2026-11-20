package view

import (
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// dialogs 声明三个弹窗：新建请求、重命名、设置。它们叠在整窗之上，最后声明。
func (m *Model) dialogs(c ui.C) {
	m.commandPalette(c)
	m.trashDialog(c)
	m.curlImportDialog(c)
	m.newRequestDialog(c)
	m.newFolderDialog(c)
	m.deleteFolderDialog(c)
	m.codeDialog(c)
	m.renameDialog(c)
	m.historyDialog(c)
	m.importDialog(c)
	m.mockDialog(c)
	m.runnerDialog(c)
	m.runRulesDialog(c)
	m.settingsDialog(c)
	m.mcpDialog(c)
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

// newFolderDialog 是「新建分组」：只填名字，落在右键的那个分组里（或集合根）。
func (m *Model) newFolderDialog(c ui.C) {
	ui.Modal(c, &m.ui.folderOpen, "新建分组", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "分组会建在当前层级下；重名自动加 -2。").FontSize(11).Muted()
			ui.TextField(c, &m.ui.folderName, "分组名", "例如：用户").FillWidth()
			if m.ui.folderErr != "" {
				ui.Text(c, m.ui.folderErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.folderOpen = false
			}
			if ui.Button(c, "创建").Clicked() {
				c.After(m.createFolder)
			}
		})
	})
}

// codeDialog 展示当前请求的代码片段：cURL / fetch / axios / Go / Python。
func (m *Model) codeDialog(c ui.C) {
	ui.ModalWith(c, &m.codeOpen, "代码生成", ui.ModalSpec{Width: 760, Height: 520}, func() {
		ui.Column(c).FillWidth().Height(380).Gap(10).Children(func() {
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "语言").FontSize(11).Muted()
				choice := ui.Choice(c, &m.codeLang, codeLangs, func(i int) string { return codeLangNames[i] })
				if choice.Changed() {
					m.refreshCode()
				}
				ui.Spacer(c)
				ui.Text(c, "按当前草稿生成，未保存的修改也会带上（变量保持 {{name}} 原样）。").
					FontSize(11).Muted()
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Paragraph(c, m.codeText, 0).Mono().FontSize(11)
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "复制代码").Icon(IconCopy).Clicked() {
				c.After(func() { m.copyValue(m.codeText) })
			}
			if ui.Button(c, "完成").Clicked() {
				m.codeOpen = false
			}
		})
	})
}

// deleteFolderDialog 确认删除分组：整棵子树进回收站，之后可从回收站恢复。
func (m *Model) deleteFolderDialog(c ui.C) {
	ui.Modal(c, &m.ui.folderDeleteOpen, "删除分组", func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "「"+m.ui.folderDeleteName+"」及里面的请求 / 文档会一起移进回收站。").FontSize(12)
			ui.Text(c, "需要时可从「回收站」恢复（回到集合根）。").FontSize(11).Muted()
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.folderDeleteOpen = false
			}
			if ui.Button(c, "删除").Destructive().Clicked() {
				c.After(m.deleteFolder)
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

// settingsDialog 是设置：左侧分区导航（外观 / 网络与安全 / 本地数据 / 关于），
// 右侧是当前分区的表单，尺寸与布局对齐参考图 15-17。
func (m *Model) settingsDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.settingsOpen, "设置", ui.ModalSpec{Width: 780, Height: 520}, func() {
		ui.Row(c).FillWidth().Height(420).Gap(16).Children(func() {
			ui.Column(c).Width(160).Gap(4).Children(func() {
				for index, name := range settingsSections {
					section := index
					row := ui.Row(c).FillWidth().PadX(10).PadY(5).Gap(6).Items(ui.Center).
						Radius(c.Theme().Metrics.RadiusControl).
						Hover(ui.Look{}.Background(c.Theme().Hover)).
						Hit(ui.ID(c), "设置分区 "+name)
					if m.ui.settingsSection == section {
						row.Background(m.pal.active)
					}
					row.Children(func() {
						ui.Text(c, name).FontSize(12)
					})
					if row.Clicked() {
						m.ui.settingsSection = section
					}
				}
			})
			ui.Divider(c)
			ui.Column(c).Grow(1).FillWidth().Items(ui.Start).Gap(10).Children(func() {
				switch m.ui.settingsSection {
				case 1:
					m.settingsNetwork(c)
				case 2:
					m.settingsLocal(c)
				case 3:
					m.settingsAbout(c)
				default:
					m.settingsAppearance(c)
				}
			})
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
	if (m.ui.themePick == 1) != m.isLight() {
		m.setTheme(m.ui.themePick == 1)
	}
	m.applySettingsEdits()
}

// settingsSections 是设置左侧的分区顺序（只列软件设置本身）。
var settingsSections = []string{"外观", "网络与安全", "本地数据", "关于"}

// formLabelWidth 是设置表单里标签列的宽度：各分区共用，右侧控件才对得齐。
const formLabelWidth = 90

// formRowWidth 是设置表单行的宽度：每行同宽，标签列才会在同一条竖线上。
const formRowWidth = 360

// formLabel 是设置表单左侧的标签：固定 90px 的列，文字靠右，右缘贴着控件。
// 用一个定宽的 Row 兜住（而不是给 Text 设 Width + TextEnd：那样 TextEnd 会把
// 文本矩形整体挪位，不同长度的标签反而对不齐）。
func formLabel(c ui.C, text string) {
	ui.Row(c).Width(formLabelWidth).Justify(ui.End).Children(func() {
		ui.Text(c, text).FontSize(11).Muted()
	})
}

// settingsAppearance 是外观分区：主题立即生效并落盘。
func (m *Model) settingsAppearance(c ui.C) {
	ui.Text(c, "外观").FontSize(12).Bold().Muted()
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "主题")
		ui.Choice(c, &m.ui.themePick, themeOptions, func(i int) string { return themeNames[i] })
	})
	ui.Text(c, "切换后立即生效，并写入本地偏好；Markdown 预览的配色一起切换。").
		FontSize(11).Muted()
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "请求 / 响应分栏")
		ui.Choice(c, &m.ui.splitSide, splitOptions, func(i int) string { return splitNames[i] })
	})
	ui.Text(c, "「左右」适合宽屏对照；拖动中间的分隔条可调整大小，切换后立即生效。").
		FontSize(11).Muted()
}

// settingsNetwork 是网络与安全分区：默认超时与重定向策略。
func (m *Model) settingsNetwork(c ui.C) {
	ui.Text(c, "网络与安全").FontSize(12).Bold().Muted()
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "默认超时（秒）")
		ui.TextField(c, &m.ui.timeoutText, "默认超时", "20").Width(90).Changed()
		ui.Spacer(c)
	})
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "跟随重定向")
		ui.Switch(c, &m.ui.follow, "跟随重定向").Changed()
		ui.Spacer(c)
	})
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "忽略证书校验")
		ui.Switch(c, &m.ui.insecure, "忽略证书校验").Changed()
		ui.Spacer(c)
	})
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "代理地址")
		ui.TextField(c, &m.ui.proxyText, "代理地址", "http://127.0.0.1:7890").Grow(1).Changed()
	})
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "客户端证书")
		ui.TextField(c, &m.ui.clientCertText, "客户端证书", "client.crt（PEM）").Grow(1).Changed()
	})
	ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
		formLabel(c, "客户端私钥")
		ui.TextField(c, &m.ui.clientKeyText, "客户端私钥", "client.key（PEM）").Grow(1).Changed()
	})
	ui.Text(c, "「忽略证书校验」只用于自签 / 内网证书调试；请求文件里的 "+
		"settings.timeout / followRedirects / insecureSsl 显式设置时优先于这里。"+
		"代理留空即直连（支持 http / https / socks5）；证书与私钥都填才启用双向 TLS。").FontSize(11).Muted()
}

// settingsLocal 是本地数据分区：集合路径、配置文件与当前环境。
func (m *Model) settingsLocal(c ui.C) {
	ui.Text(c, "本地数据").FontSize(12).Bold().Muted()
	ui.Text(c, m.ui.dirLine).FontSize(11).Muted().Mono()
	if dir, err := settings.Dir(); err == nil {
		ui.Text(c, "偏好文件："+dir).FontSize(11).Muted().Mono()
	}
	ui.Text(c, m.ui.envLine).FontSize(11).Muted()
	ui.Text(c, "集合是纯文本真相源，本机偏好（主题 / 超时 / 上次环境）不写进集合。").
		FontSize(11).Muted()
	if ui.Button(c, "打开回收站").Icon(IconTrash).Secondary().Clicked() {
		c.After(m.openTrash)
	}
}

// historyDialog 是「请求历史」：每次真实发送都记一条，点一行回到那条请求。
// 记录只在本机（history.jsonl），不写进集合。
func (m *Model) historyDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.historyOpen, "请求历史", ui.ModalSpec{Width: 760, Height: 500}, func() {
		ui.Column(c).FillWidth().Height(400).Gap(6).Children(func() {
			ui.TextField(c, &m.ui.historyQuery, "搜索历史", "按地址或请求名过滤…").FillWidth()
			needle := strings.ToLower(strings.TrimSpace(m.ui.historyQuery))
			shown := 0
			for _, record := range m.ui.historyRows {
				if needle != "" && !strings.Contains(strings.ToLower(record.URL+" "+record.Name), needle) {
					continue
				}
				shown++
				item := record
				status := "—"
				if item.Status > 0 {
					status = strconv.Itoa(item.Status)
				}
				meta := formatDuration(item.Duration)
				if item.Size > 0 {
					meta += " · " + humanBytes(item.Size)
				}
				row := ui.Row(c).FillWidth().PadX(8).PadY(4).Gap(8).Items(ui.Center).
					Radius(c.Theme().Metrics.RadiusControl).
					Hover(ui.Look{}.Background(c.Theme().Hover)).
					Hit(ui.ID(c), "历史 "+item.URL).
					Tooltip("点这一行回到那条请求（不在当前集合时复制地址）")
				row.Children(func() {
					ui.Text(c, item.Time.Format("01-02 15:04")).FontSize(10).Mono().Muted().Width(76)
					ui.Text(c, item.Method).FontSize(10).Mono().Bold().Width(48).
						Color(methodTint(item.Method, m.isLight()))
					tint := c.Theme().Success
					if item.Err != "" || item.Status >= 400 {
						tint = c.Theme().Danger
					}
					ui.Text(c, status).FontSize(10).Mono().Color(tint).Width(34)
					ui.Text(c, item.URL).FontSize(11).Grow(1).Ellipsis()
					ui.Text(c, meta).FontSize(10).Mono().Muted().Width(100)
				})
				if row.Clicked() {
					c.After(func() { m.reopenHistory(item) })
				}
			}
			if shown == 0 {
				ui.Text(c, "还没有记录：发送一次请求就会记在这里。").FontSize(11).Muted()
			}
			if m.ui.historyNote != "" {
				ui.Text(c, m.ui.historyNote).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "清空历史").Icon(IconTrash).Outline().Clicked() {
				c.After(m.clearHistory)
			}
			if ui.Button(c, "关闭").Clicked() {
				m.ui.historyOpen = false
			}
		})
	})
}

// formatDuration 把毫秒写成「12 ms」或「1.2 s」。
func formatDuration(ms int64) string {
	if ms >= 1000 {
		return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + " s"
	}
	return strconv.FormatInt(ms, 10) + " ms"
}

// mcpDialog 是内嵌 MCP 服务的**独立**设置弹窗：与软件设置分开，没有左侧分区菜单
// （对齐 Wails 版：MCP 是状态栏入口自带的面板，不属于软件设置）。
func (m *Model) mcpDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.mcpOpen, "MCP 服务", ui.ModalSpec{Width: 680, Height: 560}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, "把当前打开的集合以工具的形式提供给 AI（Claude Desktop / Cursor 等 MCP 客户端）。").
				FontSize(11).Muted()
			ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
				formLabel(c, "启用服务")
				ui.Switch(c, &m.ui.mcpEnabled, "启用 MCP 服务").Changed()
			})
			ui.Row(c).Width(formRowWidth).Gap(8).Items(ui.Center).Children(func() {
				formLabel(c, "监听地址")
				ui.TextField(c, &m.ui.mcpAddr, "监听地址", settings.DefaultMCPAddr).Width(200).Changed()
			})
			switch {
			case m.mcp.running:
				ui.Text(c, "运行中："+m.mcp.addr).FontSize(11).Mono().Color(c.Theme().Success)
			case m.mcp.err != "":
				ui.Text(c, "启动失败："+m.mcp.err).FontSize(11).Color(c.Theme().Danger)
			default:
				ui.Text(c, "未启用").FontSize(11).Muted()
			}
			m.mcpAllowList(c)
			if token := m.ui.mcpToken; token != "" {
				ui.Text(c, "访问令牌：").FontSize(11).Muted()
				ui.Text(c, token).FontSize(11).Mono().Ellipsis()
			}
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "客户端连接："+mcpURL(m.ui.mcpAddr)).
					FontSize(11).Muted().Ellipsis()
				ui.Spacer(c)
				if ui.Button(c, "复制配置").Icon(IconCopy).Outline().Small().
					Tooltip("复制 MCP 客户端配置（含监听地址与令牌）").Clicked() {
					m.copyValue(m.mcpConfigJSON())
				}
			})
			ui.Text(c, "可用工具：list_workspaces / list_requests / get_request / get_doc / send_request。").
				FontSize(11).Muted()
			ui.Text(c, "只暴露上面勾选的集合；地址留空或只写端口时按回环 127.0.0.1 监听。").
				FontSize(11).Muted()
			if m.copyNote != "" {
				ui.Text(c, m.copyNote).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "完成").Clicked() {
				m.ui.mcpOpen = false
			}
		})
	})
	if !m.ui.mcpOpen {
		return
	}
	// 弹窗开着时把改动落到偏好（开关 / 地址变化即重启服务）。
	m.applyMCPEdits()
}

// mcpAllowList 是 MCP 弹窗里的「可访问的工作区」：勾选 = 授权 AI 读这个集合，
// 右侧「可写」决定写类工具能否落盘（当前工具集还没有写工具，先随配置存下来）。
func (m *Model) mcpAllowList(c ui.C) {
	ui.Text(c, "可访问的工作区").FontSize(11).Muted()
	if len(m.ui.mcpAllow) == 0 {
		ui.Text(c, "（还没有打开的集合：先在侧栏打开一个目录）").FontSize(11).Muted()
		return
	}
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		for index := range m.ui.mcpAllow {
			row := &m.ui.mcpAllow[index]
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Checkbox(c, &row.Allowed, row.Name).Changed()
				ui.Text(c, row.Dir).FontSize(10).Mono().Muted().Ellipsis()
				ui.Spacer(c)
				writable := ui.Checkbox(c, &row.Writable, "可写")
				if !row.Allowed {
					writable.Disabled(true)
				}
				writable.Changed()
			})
		}
	})
	ui.Text(c, "「可写」留给将来的写类工具；当前工具集是只读 + 发送请求。").
		FontSize(11).Muted()
}

// settingsAbout 是「关于」：版本与输入法现状。
func (m *Model) settingsAbout(c ui.C) {
	ui.Text(c, "关于").FontSize(12).Bold().Muted()
	ui.Text(c, "api-doc-go "+appVersion).FontSize(13)
	ui.Divider(c)
	ui.Text(c, "输入法").FontSize(12).Bold().Muted()
	ui.Text(c, m.ui.imeDetail).FontSize(11).Muted()
	ui.Text(c, "Viva 的 Linux 后端只支持 IBus；复制粘贴中文可用（Ctrl+C / Ctrl+V）。").
		FontSize(11).Muted()
}

// runRulesDialog 编辑一条请求的运行规则：提取（写进变量袋）与断言（判定成败）。
// 规则存进请求文件的顶层 run: 键，随集合走，不塞进本机偏好。
func (m *Model) runRulesDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.rulesOpen, "运行规则 · "+m.ui.rulesName, ui.ModalSpec{Width: 820, Height: 580}, func() {
		ui.Column(c).FillWidth().Height(480).Gap(6).Children(func() {
			ui.Text(c, "规则写在请求文件的 run: 键里；提取到的变量供本次运行后面的请求用 {{名字}} 引用。").
				FontSize(11).Muted()

			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "提取").FontSize(11).Bold().Muted()
				ui.Spacer(c)
				if ui.Button(c, "添加提取").Icon(IconPlus).Ghost().Small().Clicked() {
					m.ui.ruleExtracts = append(m.ui.ruleExtracts, runner.Extract{From: "json", Path: "$.data.token"})
				}
			})
			for index := range m.ui.ruleExtracts {
				row := &m.ui.ruleExtracts[index]
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.TextField(c, &row.Name, "变量名", "token").Width(140)
					ui.Select(c, &row.From, runExtractSources, nil).Width(110)
					ui.TextField(c, &row.Path, "路径", "$.data.token（header 时填头名）").Grow(1)
					if ui.Button(c, "删除提取").Icon(IconTrash).Outline().IconOnly().Clicked() {
						m.ui.ruleExtracts = append(m.ui.ruleExtracts[:index], m.ui.ruleExtracts[index+1:]...)
					}
				})
			}
			if len(m.ui.ruleExtracts) == 0 {
				ui.Text(c, "还没有提取规则。").FontSize(11).Muted()
			}

			ui.Divider(c)
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "断言").FontSize(11).Bold().Muted()
				ui.Spacer(c)
				if ui.Button(c, "添加断言").Icon(IconPlus).Ghost().Small().Clicked() {
					m.ui.ruleAsserts = append(m.ui.ruleAsserts, runner.Assert{Kind: "status", Op: "equals", Want: "200"})
				}
			})
			for index := range m.ui.ruleAsserts {
				row := &m.ui.ruleAsserts[index]
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.Select(c, &row.Kind, runAssertKinds, nil).Width(110)
					ui.TextField(c, &row.Path, "路径", "$.code（status / time 留空）").Width(200)
					ui.Select(c, &row.Op, runAssertOps, nil).Width(110)
					ui.TextField(c, &row.Want, "期望值", "200").Grow(1)
					if ui.Button(c, "删除断言").Icon(IconTrash).Outline().IconOnly().Clicked() {
						m.ui.ruleAsserts = append(m.ui.ruleAsserts[:index], m.ui.ruleAsserts[index+1:]...)
					}
				})
			}
			if len(m.ui.ruleAsserts) == 0 {
				ui.Text(c, "还没有断言规则。").FontSize(11).Muted()
			}
			if m.ui.rulesNote != "" {
				ui.Text(c, m.ui.rulesNote).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.rulesOpen = false
			}
			if ui.Button(c, "保存").Clicked() {
				c.After(m.saveRunRules)
			}
		})
	})
}

// runExtractSources / runAssertKinds / runAssertOps 是规则弹窗里的下拉项。
var (
	runExtractSources = []string{"json", "header", "status"}
	runAssertKinds    = []string{"status", "json", "header", "time"}
	runAssertOps      = []string{"equals", "contains", "exists", "lt"}
)

// runnerDialog 是集合运行器：勾一批请求顺序跑，右边是逐条结果。
func (m *Model) runnerDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.runOpen, "运行集合", ui.ModalSpec{Width: 820, Height: 620}, func() {
		ui.Column(c).FillWidth().Height(520).Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "顺序执行勾选的请求；状态码 ≥ 400 或连不上算失败。").FontSize(11).Muted()
				ui.Spacer(c)
				if ui.Button(c, "全选").Ghost().Small().Clicked() {
					c.After(func() { m.selectAllRuns(true) })
				}
				if ui.Button(c, "全不选").Ghost().Small().Clicked() {
					c.After(func() { m.selectAllRuns(false) })
				}
				ui.Switch(c, &m.run.StopOnFail, "遇到失败即停")
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					if len(m.run.Rows) == 0 {
						ui.Text(c, "当前集合里没有 http 请求。").FontSize(11).Muted()
					}
					for index := range m.run.Rows {
						item := &m.run.Rows[index]
						ui.Row(c).FillWidth().Gap(8).PadY(2).Items(ui.Center).Children(func() {
							ui.Checkbox(c, &item.Selected, "")
							ui.Text(c, runMethodText(item.Method)).FontSize(10).Mono().Bold().Width(52).
								Color(methodTint(item.Method, m.isLight()))
							ui.Text(c, item.Name).FontSize(11).Grow(1).Ellipsis()
							ui.Text(c, item.URL).FontSize(10).Mono().Muted().Grow(1).Ellipsis()
							if ui.Button(c, "规则 "+item.Name).Ghost().XSmall().Icon(IconSliders).Clicked() {
								file := item.File
								c.After(func() { m.openRunRules(file) })
							}
						})
					}
				})
			})
			ui.Divider(c)
			ui.Text(c, "结果").FontSize(11).Muted()
			if len(m.run.Results) == 0 {
				ui.Text(c, "还没运行。").FontSize(11).Muted()
			}
			for _, result := range m.run.Results {
				entry := result
				ui.Row(c).FillWidth().Gap(8).PadY(2).Items(ui.Center).Children(func() {
					tint := c.Theme().Success
					status := "错误"
					if entry.Err != "" {
						tint = c.Theme().Danger
					} else {
						status = strconv.Itoa(entry.Status)
						if entry.Failed {
							tint = c.Theme().Danger
						}
					}
					ui.Text(c, status).FontSize(10).Mono().Color(tint).Width(44)
					ui.Text(c, entry.Name).FontSize(11).Width(180).Ellipsis()
					ui.Text(c, formatDuration(entry.Duration.Milliseconds())).FontSize(10).Mono().Muted().Width(70)
					detail := entry.URL
					if entry.Err != "" {
						detail = entry.Err
					}
					ui.Text(c, detail).FontSize(10).Mono().Muted().Grow(1).Ellipsis()
				})
				for _, failure := range entry.Failures {
					ui.Text(c, "　✗ "+failure).FontSize(10).Color(c.Theme().Danger)
				}
				if len(entry.Extracted) > 0 {
					ui.Text(c, "　提取 "+strings.Join(runner.SortedVars(entry.Extracted), "；")).
						FontSize(10).Mono().Muted()
				}
			}
			if m.run.Note != "" {
				ui.Text(c, m.run.Note).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.runOpen = false
			}
			if m.run.Running {
				if ui.Button(c, "停止").Icon(IconX).Outline().Clicked() {
					c.After(m.stopRun)
				}
				return
			}
			if ui.Button(c, "运行").Clicked() {
				c.After(m.startRun)
			}
		})
	})
}

// mockDialog 是本地 Mock 服务：开关、监听地址与当前注册的路由表。
// 桩只回示例数据，不转发、不执行脚本，用于前端联调。
func (m *Model) mockDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.mockOpen, "本地 Mock 服务", ui.ModalSpec{Width: 760, Height: 560}, func() {
		ui.Column(c).FillWidth().Height(460).Gap(8).Children(func() {
			ui.Text(c, "按当前打开集合里的请求假回复：方法与路径取自请求文件，"+
				"响应体按请求体字段表生成示例值（没字段表就回 {\"mock\":true}）。").FontSize(11).Muted()
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				formLabel(c, "监听地址")
				ui.TextField(c, &m.ui.mockAddrText, "监听地址", settings.DefaultMockAddr).Width(220)
				ui.Spacer(c)
			})
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				formLabel(c, "启用服务")
				ui.Switch(c, &m.ui.mockOn, "启用本地桩服务")
				ui.Spacer(c)
			})
			if m.mock.err != "" {
				ui.Text(c, "启动失败："+m.mock.err).FontSize(11).Color(c.Theme().Danger)
			} else if m.mock.running {
				ui.Text(c, "正在监听 http://"+m.mock.addr+"　路由 "+strconv.Itoa(len(m.mock.routes))+" 条").
					FontSize(11).Muted().Mono()
			}
			ui.Divider(c)
			ui.Text(c, "路由").FontSize(11).Muted()
			if len(m.mock.routes) == 0 {
				ui.Text(c, "没有路由：先打开集合（或在上面启用服务）。").FontSize(11).Muted()
			}
			for _, route := range m.mock.routes {
				item := route
				ui.Row(c).FillWidth().Gap(10).PadY(3).Items(ui.Center).Children(func() {
					ui.Text(c, item.Method).FontSize(10).Mono().Bold().Width(52).
						Color(methodTint(item.Method, m.isLight()))
					ui.Text(c, item.Path).FontSize(11).Mono().Grow(1).Ellipsis()
					ui.Text(c, item.Name).FontSize(10).Muted().Width(160).Ellipsis()
				})
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.mockOpen = false
			}
			if ui.Button(c, "应用").Clicked() {
				c.After(m.applyMockEdits)
			}
		})
	})
}

// importDialog 是「导入集合」：Postman 导出的 JSON，或另一个 OpenCollection 集合目录。
// 两种来源落到同一个落点分组，导完就重扫，同名一律加 -2。
func (m *Model) importDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.importOpen, "导入集合", ui.ModalSpec{Width: 720, Height: 520}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
				ui.Text(c, "来源").FontSize(11).Muted().Width(48)
				ui.Choice(c, &m.ui.importMode, importModes, func(mode int) string {
					return importModeNames[mode]
				})
				ui.Spacer(c)
				ui.Text(c, "导入到").FontSize(11).Muted().Width(48)
				ui.Select(c, &m.ui.importFolder, m.ui.importFolders, nil).Width(200)
			})
			if m.ui.importMode == 1 {
				ui.Text(c, "另一个 OpenCollection 集合：分组照层级复制，请求换成新 uid，文档进 docs/。").FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					ui.TextField(c, &m.ui.importDir, "集合目录", "/path/to/collection").Grow(1)
					if ui.Button(c, "选择目录…").Icon(IconFolderOpen).Outline().Clicked() {
						c.After(m.pickImportDir)
					}
				})
			} else {
				ui.Text(c, "Postman v2 集合：方法 / 地址 / 查询参数 / 请求头 / raw·urlencoded·formdata body / "+
					"bearer·basic·apikey 认证会转过来；脚本与断言忽略。").FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
					if ui.Button(c, "选择 JSON 文件…").Icon(IconFilePlus).Outline().Clicked() {
						c.After(m.pickImportFile)
					}
					if ui.Button(c, "从剪贴板粘贴").Icon(IconCopy).Ghost().Clicked() {
						text, err := app.ClipboardText()
						if err != nil {
							m.ui.importErr = err.Error()
						} else {
							m.ui.importText = text
							m.ui.importErr = ""
						}
					}
					ui.Spacer(c)
				})
				ui.TextArea(c, &m.ui.importText, "Postman 集合 JSON", `{"info": {...}, "item": [...]}`).
					FillWidth().Height(220)
			}
			if m.ui.importErr != "" {
				ui.Text(c, m.ui.importErr).FontSize(11).Color(c.Theme().Danger)
			}
			if m.ui.importNote != "" {
				ui.Text(c, m.ui.importNote).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.importOpen = false
			}
			if ui.Button(c, "导入").Clicked() {
				c.After(m.runImport)
			}
		})
	})
}

// importModes / importModeNames 是导入来源的两个选项。
var (
	importModes     = []int{0, 1}
	importModeNames = []string{"Postman 集合", "OpenCollection 集合"}
)

// curlImportDialog 是「导入 cURL」：粘贴命令，解析成一条新请求（参考 19）。
func (m *Model) curlImportDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.curlOpen, "导入 cURL", ui.ModalSpec{Width: 720, Height: 460}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "支持 Chrome / Firefox / Postman 的 Copy as cURL；-d/-F/-H/-u/-b/-A/-k/-m 会映射到请求。").
				FontSize(11).Muted()
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Text(c, "导入到").FontSize(11).Muted().Width(48)
				ui.Select(c, &m.ui.curlFolder, m.ui.curlFolders, nil).Width(200)
				ui.Text(c, "请求名").FontSize(11).Muted().Width(48)
				ui.TextField(c, &m.ui.curlName, "请求名", "留空则用 URL 末段").Grow(1)
			})
			ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
				if ui.Button(c, "从剪贴板粘贴").Icon(IconCopy).Ghost().Clicked() {
					text, err := app.ClipboardText()
					if err != nil {
						m.ui.curlErr = err.Error()
					} else {
						m.ui.curlText = text
						m.ui.curlErr = ""
					}
				}
				ui.Text(c, "命令行把整段 curl 复制过来即可。").FontSize(11).Muted()
			})
			ui.TextArea(c, &m.ui.curlText, "curl 命令", "curl 'https://api.example.com/users'").
				FillWidth().Height(210)
			if m.ui.curlErr != "" {
				ui.Text(c, m.ui.curlErr).FontSize(11).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.curlOpen = false
			}
			if ui.Button(c, "导入").Clicked() {
				c.After(m.importCurl)
			}
		})
	})
}

// trashDialog 是回收站：列出 .trash 里的文件，可逐个恢复（参考 22）。
func (m *Model) trashDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.trashOpen, "回收站", ui.ModalSpec{Width: 640, Height: 420}, func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "恢复的文件会回到集合根；同名时自动补「-恢复2」后缀，不覆盖。").
				FontSize(11).Muted()
			if len(m.ui.trashRows) == 0 {
				ui.Text(c, "回收站是空的").Muted()
				return
			}
			for _, item := range m.ui.trashRows {
				trashFile := item.File
				ui.Row(c).FillWidth().PadY(4).Gap(8).Items(ui.Center).Children(func() {
					ui.Text(c, item.Name).FontSize(12).Grow(1).Ellipsis()
					ui.Text(c, item.File).FontSize(10).Mono().Muted().Ellipsis()
					if ui.Button(c, "恢复 "+item.Name).Small().Clicked() {
						c.After(func() { m.restoreTrash(trashFile) })
					}
				})
			}
			if m.ui.trashMsg != "" {
				ui.Text(c, m.ui.trashMsg).FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.trashOpen = false
			}
		})
	})
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
