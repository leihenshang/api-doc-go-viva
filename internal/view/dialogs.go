package view

import (
	"strconv"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/history"
	appmcp "github.com/leihenshang/api-doc-go-viva/internal/mcp"

	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/secrets"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/leihenshang/api-doc-go-viva/internal/updater"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/descriptions"
	"github.com/vivaui/viva/ui/popconfirm"
	"github.com/vivaui/viva/ui/validate"
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
	m.defaultsDialog(c)
	m.historyDialog(c)
	m.cookieDialog(c)
	m.reqSettingsDialog(c)
	m.shortcutsDialog(c)
	m.importDialog(c)
	m.mockDialog(c)
	m.runnerDialog(c)
	m.runRulesDialog(c)
	m.exportDialog(c)
	m.mockRuleDialog(c)
	m.settingsDialog(c)
	m.mcpDialog(c)
	m.mcpConsentDialog(c)
	m.envDialog(c)
	m.newEnvDialog(c)
	m.openCollectionDialogs(c)
}

// newRequestDialog 是「新建请求」：请求名、方法、地址，创建后写进集合并被选中。
func (m *Model) newRequestDialog(c ui.C) {
	ui.Modal(c, &m.ui.newReqOpen, "新建请求", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "请求会写进集合目录，文件名取请求名；重名自动加 -2。").FontSize(fs(11)).Muted()
			// 文件名取请求名：限长 60 并显示字数；校验交给 v0.6.9 的 FormField + validate，
			// 错误画在字段下方，而不是攒到弹窗底部一行通用提示。
			ui.FormField(c, &m.ui.newNameField, "请求名").FillWidth().Children(func() {
				name := ui.TextField(c, &m.ui.newName, "", "例如：获取用户列表",
					ui.MaxLength(60), ui.ShowCount()).FillWidth()
				if name.Changed() {
					validate.Apply(&m.ui.newNameField, m.ui.newName, m.ui.newNameRules)
					m.ui.newErr = ""
				}
			})
			ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "方法").FontSize(fs(11)).Muted()
				ui.Select(c, &m.ui.newMethod, httpMethods, nil).Width(px(120))
			})
			newURL := ui.TextField(c, &m.ui.newURL, "地址", "{{host}}/api/users").FillWidth()
			m.varTipField(newURL, m.ui.newURL)
			if m.ui.newErr != "" {
				ui.Text(c, m.ui.newErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.newReqOpen = false
			}
			if ui.Button(c, "创建").Clicked() {
				// 提交时再校验一次：输入框没碰过（Changed 没触发）也要拦住空名。
				if !validate.Apply(&m.ui.newNameField, m.ui.newName, m.ui.newNameRules) {
					m.invalidator.Invalidate()
					return
				}
				c.After(m.createRequest)
			}
		})
	})
}

// newFolderDialog 是「新建分组」：只填名字，落在右键的那个分组里（或集合根）。
func (m *Model) newFolderDialog(c ui.C) {
	ui.Modal(c, &m.ui.folderOpen, "新建分组", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "分组会建在当前层级下；重名自动加 -2。").FontSize(fs(11)).Muted()
			ui.TextField(c, &m.ui.folderName, "分组名", "例如：用户").FillWidth()
			if m.ui.folderErr != "" {
				ui.Text(c, m.ui.folderErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.codeOpen, "代码生成", ui.ModalSpec{Width: px(760), Height: px(520)}, func() {
		ui.Column(c).FillWidth().Height(px(380)).Gap(px(10)).Children(func() {
			ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "语言").FontSize(fs(11)).Muted()
				choice := ui.Choice(c, &m.codeLang, codeLangs, func(i int) string { return codeLangNames[i] })
				if choice.Changed() {
					m.refreshCode()
				}
				ui.Spacer(c)
				ui.Text(c, "按当前草稿生成，未保存的修改也会带上（变量保持 {{name}} 原样）。").
					FontSize(11).Muted()
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Paragraph(c, m.codeText, 0).Mono().FontSize(fs(11))
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
//
// 曾试过换成 v0.6.9 的 `ui.AlertDialog`（焦点范围、Escape 取消都是现成的），但它由
// components 直接画在 widget 层：headless 的 Snapshot 里既没有文字也没有命中区，
// 「每个交互都要有无头用例」这条约定就守不住了。自建 Modal 的按钮是普通 ui.Button，
// 标签可点、可断言，所以保留。
func (m *Model) deleteFolderDialog(c ui.C) {
	ui.Modal(c, &m.ui.folderDeleteOpen, "删除分组", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "「"+m.ui.folderDeleteName+"」及里面的请求 / 文档会一起移进回收站。").FontSize(fs(12))
			ui.Text(c, "需要时可从「回收站」恢复（回到集合根）。").FontSize(fs(11)).Muted()
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.renameOpen, "重命名", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "只改显示名，文件名保持不变。").FontSize(fs(11)).Muted()
			ui.TextField(c, &m.ui.renameName, "名称", "新的显示名").FillWidth()
			if m.ui.renameErr != "" {
				ui.Text(c, m.ui.renameErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.settingsOpen, "设置", ui.ModalSpec{Width: px(780), Height: px(520)}, func() {
		ui.Row(c).FillWidth().Height(px(420)).Gap(px(16)).Children(func() {
			ui.Column(c).Width(px(160)).Gap(px(4)).Children(func() {
				for index, name := range settingsSections {
					section := index
					row := ui.Row(c).FillWidth().PadX(px(10)).PadY(px(5)).Gap(px(6)).Items(ui.Center).
						Radius(c.Theme().Metrics.RadiusControl).
						Hover(ui.Look{}.Background(c.Theme().Hover)).
						Hit(ui.ID(c), "设置分区 "+name)
					if m.ui.settingsSection == section {
						row.Background(m.pal.active)
					}
					row.Children(func() {
						ui.Text(c, name).FontSize(fs(12))
					})
					if row.Clicked() {
						m.ui.settingsSection = section
					}
				}
			})
			ui.Divider(c)
			ui.Column(c).Grow(1).FillWidth().Items(ui.Start).Gap(px(10)).Children(func() {
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
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
var formLabelWidth float32 = 90

// scrollbarGutter 是带框架滚动条的滚动区给内容留的右侧空隙。
//
// viva 的 Scrollbars() 是覆盖式的（base.ScrollAreaBarSize = 10）：条子画在滚动区
// 右边缘之上，内容不留白就会被压住右端——文字被截、右对齐的按钮被盖一半。
// 留白是固定值，所以滚动条显示或隐藏时布局都不跳。
const scrollbarGutter float32 = 14

// mcpWorkspaceNameWidth 是 MCP 工作区列表里「集合名」列的固定宽度：
// 勾选框的名字宽度随内容变，不钉住的话后面的路径起点会漂。
const mcpWorkspaceNameWidth float32 = 170

// formRowWidth 是设置表单行的宽度：每行同宽，标签列才会在同一条竖线上。
var formRowWidth float32 = 360

// settingsBodyWidth 是设置右侧内容区的宽度：对话框 780 − 左右内边距 − 左侧导航 160 − 间距。
// 换行文本必须拿到一个确定宽度才会真的换行（Percent 在这里按对话框算，不按内容区）。
var settingsBodyWidth float32 = 556

// formLabel 是设置表单左侧的标签：固定 90px 的列，文字靠右，右缘贴着控件。
// 用一个定宽的 Row 兜住（而不是给 Text 设 Width + TextEnd：那样 TextEnd 会把
// 文本矩形整体挪位，不同长度的标签反而对不齐）。
func formLabel(c ui.C, text string) {
	ui.Row(c).Width(formLabelWidth).Justify(ui.End).Children(func() {
		ui.Text(c, text).FontSize(fs(11)).Muted()
	})
}

// hintTip 在设置行尾放一个感叹号：这一行的说明收进悬停气泡。
//
// 表单内容区只有五百来像素宽，原来的整段说明是一行不换行的 Text，长句直接被
// 右边缘切掉（「集合进 git 也只留密文」之后全看不见）。气泡由 viva 画在最上层，
// 按 MaxWidth 自动换行，放在控件右侧，既不挡输入也不再撑破表单。
func hintTip(c ui.C, text string) {
	id := ui.ID(c)
	ui.Icon(c, IconInfo).Size(px(14), px(14)).Shrink(0).
		Color(c.Theme().MutedForeground).
		Hit(id, text).
		Tooltip(text).
		TooltipOn(ui.TooltipRight)
}

// settingsAppearance 是外观分区：主题立即生效并落盘。
func (m *Model) settingsAppearance(c ui.C) {
	ui.Text(c, "外观").FontSize(fs(12)).Bold().Muted()
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "主题")
		ui.Choice(c, &m.ui.themePick, themeOptions, func(i int) string { return themeNames[i] })
		ui.Spacer(c)
		hintTip(c, "切换后立即生效，并写入本地偏好；Markdown 预览的配色一起切换。")
	})
	// 五档选项比一行表单宽：单独加宽，否则标签列会被挤没。
	ui.Row(c).Width(formRowWidth + px(80)).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "界面缩放")
		ui.Choice(c, &m.ui.scalePick, scaleOptions, func(pct int) string { return scaleLabel(pct) })
		ui.Spacer(c)
		hintTip(c, "按比例放大或缩小整个界面：文字、控件、行高、侧栏与弹窗一起变，立即生效并写入偏好。")
	})
	// Choice 在声明时就写回了 scalePick（百分比）：这里跟随切换，立即按新比例重排。
	if pct := m.ui.scalePick; pct != m.settings.UIScalePercent() {
		m.setUIScale(pct)
	}
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "请求 / 响应分栏")
		ui.Choice(c, &m.ui.splitSide, splitOptions, func(i int) string { return splitNames[i] })
		ui.Spacer(c)
		hintTip(c, "「左右」适合宽屏对照；拖动中间的分隔条可调整大小，切换后立即生效。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "文档打开")
		ui.Choice(c, &m.ui.docSplitPick, docOptions, func(i int) string { return docNames[i] })
		ui.Spacer(c)
		hintTip(c, "默认只显示预览（全宽）；选「编辑」就是左边写 Markdown、右边看效果。文档页右上角的按钮可以随时临时切换。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "自动保存")
		ui.Switch(c, &m.ui.autoSave, "自动保存").Changed()
		ui.Text(c, "停顿").FontSize(fs(11)).Muted()
		ui.TextField(c, &m.ui.autoSaveSec, "间隔", "2").Width(px(56))
		ui.Text(c, "秒").FontSize(fs(11)).Muted()
		ui.Spacer(c)
		hintTip(c, "开着：最后一次编辑停这么久就写回集合（与 Ctrl+S 等价）。"+
			"关掉：改动只存成草稿，重开还能捡回来，但集合文件不动。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "启动窗口")
		ui.Switch(c, &m.ui.startMaximized, "启动时最大化")
		ui.Spacer(c)
		hintTip(c, "开着：下次启动直接铺满整个工作区（最大化，标题栏与窗口按钮都还在，不是无边框全屏）；"+
			"关掉：按上次的浮动尺寸打开。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "关闭窗口")
		if ui.Switch(c, &m.ui.closeToTray, "关闭窗口时收到托盘").Changed() {
			// 开关就是图标的开关：关掉时把托盘图标摘掉，不留一个点不开的图标。
			if m.ui.closeToTray {
				m.installTray(m.app)
			} else {
				m.removeTray()
			}
		}
		ui.Spacer(c)
		hintTip(c, "开着时点关闭只把窗口收到托盘，进程继续跑；托盘不可用的桌面会自动退回「关窗即退出」。")
	})
	// 托盘状态是「结果」不是「说明」：它得一直看得见，所以留在表单里，但要换行。
	ui.Wrap(c, m.trayHint()).Width(settingsBodyWidth).FontSize(fs(11)).Muted()
}

// settingsNetwork 是网络与安全分区：默认超时与重定向策略。
func (m *Model) settingsNetwork(c ui.C) {
	ui.Text(c, "网络与安全").FontSize(fs(12)).Bold().Muted()
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "默认超时（秒）")
		ui.TextField(c, &m.ui.timeoutText, "默认超时", "20").Width(px(90)).Changed()
		ui.Spacer(c)
		hintTip(c, "请求文件里的 settings.timeout 显式设置时优先于这里。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "跟随重定向")
		ui.Switch(c, &m.ui.follow, "跟随重定向").Changed()
		ui.Spacer(c)
		hintTip(c, "请求文件里的 settings.followRedirects 显式设置时优先于这里。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "忽略证书校验")
		ui.Switch(c, &m.ui.insecure, "忽略证书校验").Changed()
		ui.Spacer(c)
		hintTip(c, "只用于自签 / 内网证书调试；请求文件里的 settings.insecureSsl 显式设置时优先于这里。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "加密敏感值")
		ui.Switch(c, &m.ui.sealSecrets, "加密保存敏感值").Changed()
		ui.Spacer(c)
		hintTip(c, "敏感值（环境里的 secret 变量）落盘时用 AES-256-GCM 加密，密钥在配置目录（0600），"+
			"与集合分开：集合进 git 也只留密文。关掉后新保存的值变回明文，已有的密文仍可读。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "保存 Cookie")
		ui.Switch(c, &m.ui.persistCookies, "保存 Cookie").Changed()
		ui.Spacer(c)
		hintTip(c, "开着时响应的 Cookie 会存本机，下次发同一域名的请求自动带上；不写进集合。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "代理地址")
		ui.TextField(c, &m.ui.proxyText, "代理地址", "http://127.0.0.1:7890").Grow(1).Changed()
		hintTip(c, "留空即直连；支持 http / https / socks5。")
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "客户端证书")
		ui.TextField(c, &m.ui.clientCertText, "客户端证书", "client.crt（PEM）").Grow(1).Changed()
	})
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "客户端私钥")
		ui.TextField(c, &m.ui.clientKeyText, "客户端私钥", "client.key（PEM）").Grow(1).Changed()
		hintTip(c, "证书与私钥都填才启用双向 TLS。")
	})
}

// settingsLocal 是本地数据分区：集合路径、配置文件与当前环境。
func (m *Model) settingsLocal(c ui.C) {
	ui.Text(c, "本地数据").FontSize(fs(12)).Bold().Muted()
	// 三条只读信息用 ui/descriptions（v0.6.9）：标签列对齐、带边框，比四行 Wrap 好读。
	// 切片复用 model 里的底层数组，绘制期不分配。
	// 值用 Content + Wrap：路径可能比值列宽，description 的纯文本 Value 会被裁掉；
	// 标签已经写明是什么，值里的「集合：」「环境：」前缀去掉，别重复。
	items := m.ui.localItems[:0]
	items = append(items, descriptions.Item{Label: "集合目录", Content: func(c ui.C) {
		ui.Wrap(c, strings.TrimPrefix(m.ui.dirLine, "集合：")).FontSize(fs(11)).Muted().Mono().FillWidth()
	}})
	if dir, err := settings.Dir(); err == nil {
		items = append(items, descriptions.Item{Label: "偏好文件", Content: func(c ui.C) {
			ui.Wrap(c, dir).FontSize(fs(11)).Muted().Mono().FillWidth()
		}})
	}
	items = append(items, descriptions.Item{Label: "当前环境", Content: func(c ui.C) {
		ui.Wrap(c, strings.TrimPrefix(m.ui.envLine, "环境：")).FontSize(fs(11)).Muted().FillWidth()
	}})
	m.ui.localItems = items
	descriptions.View(c, "", descriptions.Spec{
		Items: items, Columns: 1, Bordered: true, LabelWidth: px(76), Size: descriptions.Small,
	}).FillWidth()
	ui.Wrap(c, "集合是纯文本真相源，本机偏好（主题 / 超时 / 上次环境）不写进集合。").
		Width(settingsBodyWidth).FontSize(fs(11)).Muted()
	if ui.Button(c, "打开回收站").Icon(IconTrash).Secondary().Clicked() {
		c.After(m.openTrash)
	}
	ui.Row(c).Width(formRowWidth).Gap(px(8)).Items(ui.Center).Children(func() {
		formLabel(c, "发布仓库")
		ui.TextField(c, &m.ui.updateRepo, "发布仓库", "owner/repo（留空用内置默认）").Grow(1).Changed()
	})
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		label := "检查更新"
		if m.ui.updateBusy {
			label = "检查中…"
		}
		if ui.Button(c, label).Icon(IconImport).Outline().Disabled(m.ui.updateBusy).Clicked() {
			c.After(m.checkUpdate)
		}
		if m.ui.updateURL != "" {
			if ui.Button(c, "打开发布页").Icon(IconGlobe).Outline().Clicked() {
				c.After(m.openUpdatePage)
			}
		}
		ui.Text(c, "当前版本 v"+updater.Version).FontSize(fs(11)).Muted()
	})
	if m.ui.updateNote != "" {
		ui.Text(c, m.ui.updateNote).FontSize(fs(11)).Muted()
	}
}

// cookieDialog 是 Cookie Jar：发过的请求种下的 cookie 都在这儿，可查看与清空。
// 它与「请求历史」分开：历史是发过什么，这里是这台机器现在持有什么会话。
func (m *Model) cookieDialog(c ui.C) {
	ui.Modal(c, &m.ui.cookieOpen, "Cookie Jar", ui.ModalSpec{Width: px(700), Height: px(460)}, func() {
		ui.Column(c).FillWidth().Height(px(360)).Gap(px(6)).Children(func() {
			// 「清空」放在行首而不是 footer 右端：popconfirm 的气泡有 280px 宽，
			// 贴着弹窗右边界会越出去、点到弹窗的 scrim 上（实测会把弹窗一起关掉）。
			ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
				clearCookies := ui.Button(c, "清空").Icon(IconTrash).Outline()
				if clearCookies.Clicked() {
					m.cookieConfirm.Open()
				}
				m.cookieConfirm.View(c, clearCookies, popconfirm.Spec{
					Title:       "清空 Cookie Jar？",
					Description: "发过的请求种下的会话会一起清掉，之后的请求不再自动带 cookie。",
					OkLabel:     "确认清空",
					CancelLabel: "取消",
					Danger:      true,
				})
				if m.cookieConfirm.Confirmed() {
					c.After(m.clearCookies)
				}
				ui.Text(c, "发送请求时自动带上；集合运行器也用同一份 jar，所以「先登录再跑」是成立的。").
					FontSize(11).Muted().Grow(1)
			})
			if len(m.ui.cookieRows) == 0 {
				ui.Text(c, "还没有 cookie：发一次会种 cookie 的请求就会出现在这里。").FontSize(fs(11)).Muted()
				return
			}
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "域名").FontSize(fs(11)).Muted().Width(px(200))
				ui.Text(c, "名称").FontSize(fs(11)).Muted().Width(px(140))
				ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(1)
			})
			for _, item := range m.ui.cookieRows {
				row := item
				ui.Row(c).FillWidth().Gap(px(8)).PadY(px(3)).Items(ui.Center).Children(func() {
					ui.Text(c, row.Domain).FontSize(fs(11)).Mono().Width(px(200)).Ellipsis()
					ui.Text(c, row.Name).FontSize(fs(11)).Mono().Width(px(140)).Ellipsis()
					ui.Text(c, row.Value).FontSize(fs(11)).Mono().Muted().Grow(1).Ellipsis()
					if ui.Button(c, "删除 "+row.Name).Icon(IconTrash).Ghost().IconOnly().
						Tooltip("只删这一条（" + row.Name + "）").Clicked() {
						c.After(func() { m.deleteCookie(row) })
					}
				})
			}
			if m.ui.cookieNote != "" {
				ui.Text(c, m.ui.cookieNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "刷新").Outline().Clicked() {
				c.After(func() {
					m.reloadCookies()
					m.ui.cookieNote = ""
					m.invalidator.Invalidate()
				})
			}
			if ui.Button(c, "关闭").Clicked() {
				m.ui.cookieOpen = false
			}
		})
	})
}

// historyDialog 是「请求历史」：每次真实发送都记一条，点一行回到那条请求。
// 记录只在本机（history.jsonl），不写进集合。
func (m *Model) historyDialog(c ui.C) {
	ui.Modal(c, &m.ui.historyOpen, "请求历史", ui.ModalSpec{Width: px(760), Height: px(500)}, func() {
		ui.Column(c).FillWidth().Height(px(400)).Gap(px(6)).Children(func() {
			// 与 Cookie 弹窗同一个理由：「清空历史」放行首，popconfirm 的气泡才不越界。
			ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
				clearHistory := ui.Button(c, "清空历史").Icon(IconTrash).Outline()
				if clearHistory.Clicked() {
					m.historyConfirm.Open()
				}
				m.historyConfirm.View(c, clearHistory, popconfirm.Spec{
					Title:       "清空请求历史？",
					Description: "只清本机的 history.jsonl（集合文件不动），清完不可恢复。",
					OkLabel:     "确认清空",
					CancelLabel: "取消",
					Danger:      true,
				})
				if m.historyConfirm.Confirmed() {
					c.After(m.clearHistory)
				}
				ui.Text(c, "记录只在本机（history.jsonl），不写进集合。").FontSize(11).Muted().Grow(1)
			})
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
				row := ui.Row(c).FillWidth().PadX(px(8)).PadY(px(4)).Gap(px(8)).Items(ui.Center).
					Radius(c.Theme().Metrics.RadiusControl).
					Hover(ui.Look{}.Background(c.Theme().Hover)).
					Hit(ui.ID(c), "历史 "+item.URL).
					Tooltip("点这一行回到那条请求（不在当前集合时复制地址）")
				row.Children(func() {
					ui.Text(c, item.Time.Format("01-02 15:04")).FontSize(fs(10)).Mono().Muted().Width(px(76))
					ui.Text(c, item.Method).FontSize(fs(10)).Mono().Bold().Width(px(48)).
						Color(methodTint(item.Method, m.isLight()))
					tint := c.Theme().Success
					if item.Err != "" || item.Status >= 400 {
						tint = c.Theme().Danger
					}
					ui.Text(c, status).FontSize(fs(10)).Mono().Color(tint).Width(px(34))
					ui.Text(c, item.URL).FontSize(fs(11)).Grow(1).Ellipsis()
					ui.Text(c, meta).FontSize(fs(10)).Mono().Muted().Width(px(100))
					if ui.Button(c, "详情").Ghost().XSmall().
						Tooltip("看当时的请求头 / 参数 / 正文").Clicked() {
						c.After(func() {
							if m.ui.historyDetail == item.Time {
								m.ui.historyDetail = time.Time{}
							} else {
								m.ui.historyDetail = item.Time
							}
							m.invalidator.Invalidate()
						})
					}
				})
				if row.Clicked() {
					c.After(func() { m.reopenHistory(item) })
				}
				if m.ui.historyDetail.Equal(item.Time) {
					m.historyDetailCard(c, item)
				}
			}
			if shown == 0 {
				ui.Text(c, "还没有记录：发送一次请求就会记在这里。").FontSize(fs(11)).Muted()
			}
			if m.ui.historyNote != "" {
				ui.Text(c, m.ui.historyNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.historyOpen = false
			}
		})
	})
}

// historyDetailCard 是历史里展开的详情：当时真正发出去的请求头、参数与正文。
func (m *Model) historyDetailCard(c ui.C, item history.Entry) {
	ui.Column(c).FillWidth().Gap(px(4)).Pad(px(8)).Radius(c.Theme().Metrics.RadiusControl).
		Background(m.pal.panel).Border(1, m.pal.border).Children(func() {
		ui.Text(c, "请求头（敏感值已打码）").FontSize(fs(10)).Muted()
		if len(item.Headers) == 0 {
			ui.Text(c, "没有请求头").FontSize(fs(11)).Muted()
		}
		for _, header := range item.Headers {
			ui.Text(c, header.Name+": "+header.Value).FontSize(fs(10)).Mono().Ellipsis()
		}
		if len(item.Params) > 0 {
			ui.Text(c, "查询参数").FontSize(fs(10)).Muted()
			for _, param := range item.Params {
				ui.Text(c, param.Name+" = "+param.Value).FontSize(fs(10)).Mono().Ellipsis()
			}
		}
		if item.Body != "" {
			label := "请求体"
			if item.BodyTruncated {
				label += "（只存了前 8 KB）"
			}
			ui.Text(c, label).FontSize(fs(10)).Muted()
			ui.Text(c, item.Body).FontSize(fs(10)).Mono().MaxLines(6)
		}
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
	ui.Modal(c, &m.ui.mcpOpen, "MCP 服务", ui.ModalSpec{Width: px(700), Height: px(690)}, func() {
		// 整个内容放进定高滚动区：表单 + 工作区 + JSON 加起来超过弹窗高度，
		// 让它内部滚动，而不是撑破外框跑到窗口上。
		ui.Scroll(c).Grow(1).FillWidth().Scrollbars().Children(func() {
			// 右侧留出滚动条的位置，否则它压在「复制配置」「可写」这些右端控件上。
			ui.Column(c).FillWidth().PadRight(px(scrollbarGutter)).Gap(px(10)).Children(func() {
				// 一、服务配置
				m.mcpSectionTitle(c, "服务配置")
				// 启用服务与监听地址同一行：两者是同一个「服务」概念的两半。
				// 标签一律左对齐定宽，后面的输入框吃满剩余宽度（见 mcpRowLabel）。
				ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
					mcpRowLabel(c, "启用服务")
					ui.Switch(c, &m.ui.mcpEnabled, "启用 MCP 服务").Changed()
					ui.Slot(c).Width(px(24))
					mcpRowLabel(c, "监听地址")
					ui.TextField(c, &m.ui.mcpAddr, "监听地址", settings.DefaultMCPAddr).
						FillWidth().Changed()
					hintTip(c, "地址留空或只写端口时按回环 127.0.0.1 监听。")
				})
				// 浏览器来源紧跟在「启用服务 / 监听地址」这一栏下面。
				ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
					mcpRowLabel(c, "浏览器来源")
					ui.TextField(c, &m.ui.mcpOrigins, "浏览器来源", "留空 = 拒绝浏览器").
						FillWidth().Changed()
					hintTip(c, "浏览器里的 MCP 客户端发请求会带 Origin 头：这里列出允许的来源（逗号分隔），"+
						"留空则一律拒绝。命令行客户端不带 Origin，不受影响。")
				})
				// 两个权限开关同行（需求 3）：说明收进 tips，删掉常驻文字与安全提示条。
				ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
					mcpRowLabel(c, "权限选项")
					ui.Switch(c, &m.ui.mcpReadOnly, "只读模式").Changed()
					hintTip(c, "只读：不暴露请求与写磁盘的工具")
					// 两组开关之间留一点距离，否则四个控件连成一句。
					ui.Slot(c).Width(px(18))
					ui.Switch(c, &m.ui.mcpConsentOff, "写操作确认").Changed()
					hintTip(c, "关掉后写工具直接执行，不再询问")
					ui.Spacer(c)
				})
				// 只有真的起不来才占一行；正常运行时不再有状态条（需求 1）。
				if m.mcp.err != "" {
					ui.Text(c, "启动失败："+m.mcp.err).FontSize(fs(11)).Color(c.Theme().Danger)
				}

				// 二、工具权限
				m.mcpSectionTitle(c, "工具权限")
				ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
					mcpRowLabel(c, "暴露工具")
					for index, name := range appmcp.Groups {
						slot := index
						ui.Checkbox(c, &m.ui.mcpGroupOn[slot], mcpGroupLabels[name]).Changed()
					}
					ui.Spacer(c)
				})
				// 可用工具上移到这里（需求 6），底部不再重复。
				m.mcpNote(c, "可用工具：list_workspaces / list_requests / get_request / get_doc / send_request。")

				// 三、集合列表：单独一栏、左对齐，「可写」的说明收进标题旁的 tips。
				m.mcpSectionTitleTip(c, "可访问的集合",
					"「可写」留给将来的写类工具；当前工具集是只读 + 发送请求。")
				m.mcpAllowList(c)

				// 四、客户端接入：令牌 + 配置 JSON（都指向同一个客户端）。
				m.mcpSectionTitle(c, "客户端接入")
				// 这一栏从内容区最左排（与上面的集合列表、下面的客户端配置一致），
				// 不再缩进到 90px 标签列后面。
				ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
					mcpRowLabel(c, "访问令牌")
					ui.TextField(c, &m.ui.mcpToken, "访问令牌", "首次启用时自动生成").
						Mono().FillWidth().Changed()
					if ui.Button(c, "生成").Outline().Small().
						Tooltip("生成新令牌：客户端要用它才能连上").Clicked() {
						c.After(func() {
							m.ui.mcpToken = newMCPToken()
							m.invalidator.Invalidate()
						})
					}
					if ui.Button(c, "复制").Outline().Small().
						Tooltip("复制输入框当前令牌").Clicked() {
						m.copyValue(m.ui.mcpToken)
					}
				})
				ui.Row(c).FillWidth().Gap(px(6)).Items(ui.Center).Children(func() {
					ui.Text(c, "客户端配置（复制内容）").FontSize(fs(11)).Muted()
					ui.Spacer(c)
					if ui.Button(c, "复制配置").Icon(IconCopy).Outline().Small().
						Tooltip("复制 MCP 客户端配置（含监听地址与令牌）").Clicked() {
						m.copyValue(m.mcpConfigJSON())
					}
				})
				// 底色用 Box 的 Background 而不是 codeCard 的 Draw：Draw 直接往画布上画，
				// 不会被滚动区的裁剪 rect 收住，卡片就会溢出到弹窗外面。
				ui.Column(c).FillWidth().Pad(px(8)).Radius(px(6)).Background(m.pal.hover).Children(func() {
					ui.Paragraph(c, m.mcpConfigJSON(), 0).Mono().FontSize(fs(10))
				})

				// 五、最近调用：标题与操作按钮同一行（省一块高度，也和参考设计一致）。
				ui.Column(c).FillWidth().Gap(px(4)).Children(func() {
					ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
						ui.Text(c, "最近调用").FontSize(fs(12)).Bold()
						ui.Spacer(c)
						clearAudit := ui.Button(c, "清空").Ghost().XSmall()
						if clearAudit.Clicked() {
							m.auditConfirm.Open()
						}
						m.auditConfirm.View(c, clearAudit, popconfirm.Spec{
							Title:       "清空调用审计？",
							Description: "审计日志会整个删掉，导出过的文件不受影响。",
							OkLabel:     "确认清空",
							CancelLabel: "取消",
							Danger:      true,
						})
						if m.auditConfirm.Confirmed() {
							c.After(m.clearMCPAudit)
						}
						if ui.Button(c, "刷新").Ghost().XSmall().Clicked() {
							m.ui.mcpAuditRows = m.loadMCPAudit()
							m.invalidator.Invalidate()
						}
						if ui.Button(c, "导出").Ghost().XSmall().
							Tooltip("导出到偏好目录下的 mcp-audit-export.jsonl").Clicked() {
							m.exportMCPAudit()
						}
					})
					ui.Divider(c)
				})
				if len(m.ui.mcpAuditRows) == 0 {
					ui.Text(c, "还没有调用记录。").FontSize(fs(11)).Muted()
				}
				for _, row := range m.ui.mcpAuditRows {
					one := row
					ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
						ui.Text(c, one.Time.Format("15:04:05")).FontSize(fs(10)).Mono().Muted()
						ui.Text(c, one.Tool).FontSize(fs(11)).Mono().Ellipsis()
						if one.Write {
							ui.Text(c, "写").FontSize(fs(10)).Color(c.Theme().Danger)
						}
						if !one.OK {
							ui.Text(c, "失败").FontSize(fs(10)).Color(c.Theme().Danger)
						}
					})
				}
				if m.copyNote != "" {
					ui.Text(c, m.copyNote).FontSize(fs(11)).Muted()
				}
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
			ui.Text(c, "更改即时生效").FontSize(fs(11)).Muted()
			ui.Spacer(c)
			if ui.Button(c, "完成").Clicked() {
				m.ui.mcpOpen = false
			}
		})
	})
	if !m.ui.mcpOpen {
		return
	}
	// 弹窗开着时把改动落到偏好（开关 / 地址 / 令牌变化即重启服务）。
	m.applyMCPEdits()
}

// mcpSectionTitle 是弹窗里的分节标题：小字加粗 + 一条细分隔线，
// 与参考设计一致（分组之间靠线断句，不靠留白）。
func (m *Model) mcpSectionTitle(c ui.C, title string) {
	ui.Column(c).FillWidth().Gap(px(4)).Children(func() {
		ui.Text(c, title).FontSize(fs(12)).Bold()
		ui.Divider(c)
	})
}

// mcpRowLabelWidth 是 MCP 面板里标签列的宽度：比设置面板的 90 窄，
// 因为这些标签左对齐（不是右对齐到列尾），短一些也不会显得空。
const mcpRowLabelWidth float32 = 64

// mcpRowLabel 是左对齐的定宽标签：控件从同一条竖线起排，输入框再用 FillWidth 吃满。
func mcpRowLabel(c ui.C, text string) {
	ui.Row(c).Width(px(mcpRowLabelWidth)).Children(func() {
		ui.Text(c, text).FontSize(fs(11)).Muted()
	})
}

// mcpSectionTitleTip 与 mcpSectionTitle 一样，但标题右边跟一个 ⓘ：
// 整节的说明（比如「可写」是什么意思）收进气泡，不再占一行常驻文字。
func (m *Model) mcpSectionTitleTip(c ui.C, title, tip string) {
	ui.Column(c).FillWidth().Gap(px(4)).Children(func() {
		ui.Row(c).FillWidth().Gap(px(6)).Items(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(fs(12)).Bold()
			hintTip(c, tip)
		})
		ui.Divider(c)
	})
}

// mcpNote 是标签列之后的辅助说明：缩进到控件列，11px 弱化色。
func (m *Model) mcpNote(c ui.C, text string) {
	ui.Row(c).FillWidth().Children(func() {
		ui.Slot(c).Width(formLabelWidth)
		ui.Wrap(c, text).FillWidth().FontSize(fs(11)).Muted()
	})
}

// mcpAllowList 是 MCP 弹窗「安全与访问」里的工作区列表：勾选 = 授权 AI 读这个集合，
// 右侧「可写」决定写类工具能否落盘（当前工具集还没有写工具，先随配置存下来）。
func (m *Model) mcpAllowList(c ui.C) {
	if len(m.ui.mcpAllow) == 0 {
		ui.Text(c, "（还没有打开的集合：先在侧栏打开一个目录）").FontSize(fs(11)).Muted()
		return
	}
	for index := range m.ui.mcpAllow {
		slot := index
		row := &m.ui.mcpAllow[slot]
		// 这一栏从内容区最左开始排（不再留 90px 标签列），集合多时一眼能扫完。
		ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
			// 名称列定宽：勾选框自带名字，宽度随名字长短变，路径起点会跟着漂。
			// 用固定宽度的容器兜住它（不要给 Checkbox 本身 Width+Ellipsis，那会把控件压没）。
			ui.Row(c).Width(px(mcpWorkspaceNameWidth)).Items(ui.Center).Children(func() {
				ui.Checkbox(c, &row.Allowed, row.Name).Changed()
			})
			ui.Text(c, row.Dir).FontSize(fs(10)).Mono().Muted().Grow(1).Ellipsis()
			writable := ui.Checkbox(c, &row.Writable, "可写")
			if !row.Allowed {
				writable.Disabled(true)
			}
			writable.Changed()
		})
	}
}

// settingsAbout 是「关于」：版本与输入法现状。
func (m *Model) settingsAbout(c ui.C) {
	ui.Text(c, "关于").FontSize(fs(12)).Bold().Muted()
	ui.Text(c, "api-doc-go "+appVersion).FontSize(fs(13))
	ui.Divider(c)
	ui.Text(c, "输入法").FontSize(fs(12)).Bold().Muted()
	ui.Wrap(c, m.ui.imeDetail).Width(settingsBodyWidth).FontSize(fs(11)).Muted()
	ui.Wrap(c, "Viva 的 Linux 后端只支持 IBus；复制粘贴中文可用（Ctrl+C / Ctrl+V）。").
		Width(settingsBodyWidth).FontSize(fs(11)).Muted()
}

// mockRuleDialog 编辑一条请求的桩规则：状态码、延迟与整段响应体覆盖。
func (m *Model) mockRuleDialog(c ui.C) {
	ui.Modal(c, &m.ui.mockRuleOpen, "桩规则 · "+m.ui.mockRuleName, ui.ModalSpec{Width: px(720), Height: px(520)}, func() {
		ui.Column(c).FillWidth().Height(px(420)).Gap(px(8)).Children(func() {
			ui.Text(c, "只影响本地桩服务；留空表示用默认值（200 / 不延迟 / 由字段表生成）。").
				FontSize(11).Muted()
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "状态码")
				ui.TextField(c, &m.ui.mockRuleStatus, "状态码", "201").Width(px(120))
				formLabel(c, "延迟")
				ui.TextField(c, &m.ui.mockRuleDelay, "延迟（毫秒）", "120").Width(px(120))
				ui.Text(c, "毫秒").FontSize(fs(11)).Muted()
				ui.Spacer(c)
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "响应体")
				ui.Text(c, "留空则由请求体字段表生成示例 JSON").FontSize(fs(11)).Muted()
			})
			ui.TextArea(c, &m.ui.mockRuleBody, "覆盖响应体", `{"ok": true}`).FillWidth().Height(px(220))
			if m.ui.mockRuleNote != "" {
				ui.Text(c, m.ui.mockRuleNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.mockRuleOpen = false
			}
			if ui.Button(c, "保存").Clicked() {
				c.After(m.saveMockRule)
			}
		})
	})
}

// exportDialog 是导出范围：勾哪些分组 / 条目，按当前格式生成一份文件。
func (m *Model) exportDialog(c ui.C) {
	title := "导出 Markdown"
	if m.ui.exportFormat == "html" {
		title = "导出 HTML"
	}
	ui.Modal(c, &m.ui.exportOpen, title, ui.ModalSpec{Width: px(760), Height: px(560)}, func() {
		ui.Column(c).FillWidth().Height(px(460)).Gap(px(6)).Children(func() {
			requests, files := m.exportCounts()
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "勾选要导出的分组与请求；勾分组会带上它下面的条目。").FontSize(fs(11)).Muted()
				ui.Spacer(c)
				ui.Text(c, "已选 "+strconv.Itoa(requests)+" 条请求 · "+strconv.Itoa(files)+" 篇文档").
					FontSize(11).Muted().Mono()
				if ui.Button(c, "全选").Ghost().Small().Clicked() {
					c.After(func() { m.selectAllExports(true) })
				}
				if ui.Button(c, "全不选").Ghost().Small().Clicked() {
					c.After(func() { m.selectAllExports(false) })
				}
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(px(2)).Children(func() {
					if len(m.ui.exportRows) == 0 {
						ui.Text(c, "这个集合里没有可导出的条目。").FontSize(fs(11)).Muted()
					}
					for index := range m.ui.exportRows {
						row := &m.ui.exportRows[index]
						ui.Row(c).FillWidth().Gap(px(8)).PadY(px(2)).Items(ui.Center).Children(func() {
							ui.Slot(c).Width(float32(row.Depth) * 14)
							label := row.Title
							if ui.Checkbox(c, &row.Selected, label).Changed() {
								// 勾选状态变了才联动子项
								on := row.Selected
								c.After(func() { m.toggleExportRow(index, on) })
							}
							ui.Spacer(c)
							if row.Group {
								ui.Text(c, "分组").FontSize(fs(10)).Muted()
							}
						})
					}
				})
			})
			if m.ui.exportNote != "" {
				ui.Text(c, m.ui.exportNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.exportOpen = false
			}
			if ui.Button(c, "导出…").Clicked() {
				c.After(m.saveExport)
			}
		})
	})
}

// runRulesDialog 编辑一条请求的运行规则：提取（写进变量袋）与断言（判定成败）。
// 规则存进请求文件的顶层 run: 键，随集合走，不塞进本机偏好。
func (m *Model) runRulesDialog(c ui.C) {
	ui.Modal(c, &m.ui.rulesOpen, "运行规则 · "+m.ui.rulesName, ui.ModalSpec{Width: px(820), Height: px(580)}, func() {
		ui.Column(c).FillWidth().Height(px(480)).Gap(px(6)).Children(func() {
			ui.Text(c, "规则写在请求文件的 run: 键里；提取到的变量供本次运行后面的请求用 {{名字}} 引用。").
				FontSize(11).Muted()

			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "提取").FontSize(fs(11)).Bold().Muted()
				ui.Spacer(c)
				if ui.Button(c, "添加提取").Icon(IconPlus).Ghost().Small().Clicked() {
					m.ui.ruleExtracts = append(m.ui.ruleExtracts, runner.Extract{From: "json", Path: "$.data.token"})
				}
			})
			for index := range m.ui.ruleExtracts {
				row := &m.ui.ruleExtracts[index]
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					ui.TextField(c, &row.Name, "变量名", "token").Width(px(140))
					ui.Select(c, &row.From, runExtractSources, nil).Width(px(110))
					ui.TextField(c, &row.Path, "路径", "$.data.token（header 时填头名）").Grow(1)
					if ui.Button(c, "删除提取").Icon(IconTrash).Outline().IconOnly().Clicked() {
						m.ui.ruleExtracts = append(m.ui.ruleExtracts[:index], m.ui.ruleExtracts[index+1:]...)
					}
				})
			}
			if len(m.ui.ruleExtracts) == 0 {
				ui.Text(c, "还没有提取规则。").FontSize(fs(11)).Muted()
			}

			ui.Divider(c)
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "断言").FontSize(fs(11)).Bold().Muted()
				ui.Spacer(c)
				if ui.Button(c, "添加断言").Icon(IconPlus).Ghost().Small().Clicked() {
					m.ui.ruleAsserts = append(m.ui.ruleAsserts, runner.Assert{Kind: "status", Op: "equals", Want: "200"})
				}
			})
			for index := range m.ui.ruleAsserts {
				row := &m.ui.ruleAsserts[index]
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					ui.Select(c, &row.Kind, runAssertKinds, nil).Width(px(110))
					ui.TextField(c, &row.Path, "路径", "$.code（status / time 留空）").Width(px(200))
					ui.Select(c, &row.Op, runAssertOps, nil).Width(px(110))
					ui.TextField(c, &row.Want, "期望值", "200").Grow(1)
					if ui.Button(c, "删除断言").Icon(IconTrash).Outline().IconOnly().Clicked() {
						m.ui.ruleAsserts = append(m.ui.ruleAsserts[:index], m.ui.ruleAsserts[index+1:]...)
					}
				})
			}
			if len(m.ui.ruleAsserts) == 0 {
				ui.Text(c, "还没有断言规则。").FontSize(fs(11)).Muted()
			}
			if m.ui.rulesNote != "" {
				ui.Text(c, m.ui.rulesNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.runOpen, "运行集合", ui.ModalSpec{Width: px(820), Height: px(620)}, func() {
		ui.Column(c).FillWidth().Height(px(520)).Gap(px(8)).Children(func() {
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "顺序执行勾选的请求；状态码 ≥ 400 或连不上算失败。").FontSize(fs(11)).Muted()
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
				ui.Column(c).FillWidth().Gap(px(2)).Children(func() {
					if len(m.run.Rows) == 0 {
						ui.Text(c, "当前集合里没有 http 请求。").FontSize(fs(11)).Muted()
					}
					for index := range m.run.Rows {
						item := &m.run.Rows[index]
						ui.Row(c).FillWidth().Gap(px(8)).PadY(px(2)).Items(ui.Center).Children(func() {
							ui.Checkbox(c, &item.Selected, "")
							ui.Text(c, runMethodText(item.Method)).FontSize(fs(10)).Mono().Bold().Width(px(52)).
								Color(methodTint(item.Method, m.isLight()))
							ui.Text(c, item.Name).FontSize(fs(11)).Grow(1).Ellipsis()
							ui.Text(c, item.URL).FontSize(fs(10)).Mono().Muted().Grow(1).Ellipsis()
							if ui.Button(c, "规则 "+item.Name).Ghost().XSmall().Icon(IconSliders).Clicked() {
								file := item.File
								c.After(func() { m.openRunRules(file) })
							}
						})
					}
				})
			})
			ui.Divider(c)
			ui.Text(c, "结果").FontSize(fs(11)).Muted()
			if len(m.run.Results) == 0 {
				ui.Text(c, "还没运行。").FontSize(fs(11)).Muted()
			}
			for _, result := range m.run.Results {
				entry := result
				ui.Row(c).FillWidth().Gap(px(8)).PadY(px(2)).Items(ui.Center).Children(func() {
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
					ui.Text(c, status).FontSize(fs(10)).Mono().Color(tint).Width(px(44))
					ui.Text(c, entry.Name).FontSize(fs(11)).Width(px(180)).Ellipsis()
					ui.Text(c, formatDuration(entry.Duration.Milliseconds())).FontSize(fs(10)).Mono().Muted().Width(px(70))
					detail := entry.URL
					if entry.Err != "" {
						detail = entry.Err
					}
					ui.Text(c, detail).FontSize(fs(10)).Mono().Muted().Grow(1).Ellipsis()
				})
				for _, failure := range entry.Failures {
					ui.Text(c, "　✗ "+failure).FontSize(fs(10)).Color(c.Theme().Danger)
				}
				if len(entry.Extracted) > 0 {
					ui.Text(c, "　提取 "+strings.Join(runner.SortedVars(entry.Extracted), "；")).
						FontSize(10).Mono().Muted()
				}
			}
			if m.run.Note != "" {
				ui.Text(c, m.run.Note).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.runOpen = false
			}
			if m.run.Running {
				if ui.Button(c, "停止").Icon(IconX).Outline().Clicked() {
					c.After(m.stopRun)
				}
				return
			}
			if len(m.run.FailedFiles) > 0 {
				if ui.Button(c, "重跑失败项（"+strconv.Itoa(len(m.run.FailedFiles))+"）").
					Icon(IconRetry).Outline().
					Tooltip("只把失败的请求重新勾选并再跑一遍；顺序、extract 传递与「遇到失败即停」照旧").
					Clicked() {
					c.After(m.retryFailed)
				}
			}
			if len(m.run.Results) > 0 {
				if ui.Button(c, "导出 JUnit").Icon(IconImport).Outline().
					Tooltip("把这次结果写成 JUnit XML（CI 直接能读）").Clicked() {
					c.After(m.exportRunReport)
				}
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
	ui.Modal(c, &m.ui.mockOpen, "本地 Mock 服务", ui.ModalSpec{Width: px(760), Height: px(560)}, func() {
		ui.Column(c).FillWidth().Height(px(460)).Gap(px(8)).Children(func() {
			ui.Text(c, "按当前打开集合里的请求假回复：方法与路径取自请求文件，"+
				"响应体按请求体字段表生成示例值（没字段表就回 {\"mock\":true}）。").FontSize(fs(11)).Muted()
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "监听地址")
				ui.TextField(c, &m.ui.mockAddrText, "监听地址", settings.DefaultMockAddr).Width(px(220))
				ui.Spacer(c)
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "启用服务")
				ui.Switch(c, &m.ui.mockOn, "启用本地桩服务")
				ui.Spacer(c)
			})
			if m.mock.err != "" {
				ui.Text(c, "启动失败："+m.mock.err).FontSize(fs(11)).Color(c.Theme().Danger)
			} else if m.mock.running {
				ui.Text(c, "正在监听 http://"+m.mock.addr+"　路由 "+strconv.Itoa(len(m.mock.routes))+" 条").
					FontSize(11).Muted().Mono()
			}
			ui.Divider(c)
			ui.Text(c, "路由").FontSize(fs(11)).Muted()
			if len(m.mock.routes) == 0 {
				ui.Text(c, "没有路由：先打开集合（或在上面启用服务）。").FontSize(fs(11)).Muted()
			}
			for _, route := range m.mock.routes {
				item := route
				ui.Row(c).FillWidth().Gap(px(10)).PadY(px(3)).Items(ui.Center).Children(func() {
					ui.Text(c, item.Method).FontSize(fs(10)).Mono().Bold().Width(px(52)).
						Color(methodTint(item.Method, m.isLight()))
					ui.Text(c, item.Path).FontSize(fs(11)).Mono().Grow(1).Ellipsis()
					rule := ""
					if item.Status > 0 {
						rule = strconv.Itoa(item.Status)
					}
					if item.DelayMS > 0 {
						rule = strings.TrimSpace(rule + " · " + strconv.Itoa(item.DelayMS) + "ms")
					}
					if rule != "" {
						ui.Text(c, rule).FontSize(fs(10)).Mono().Muted().Width(px(96))
					}
					ui.Text(c, item.Name).FontSize(fs(10)).Muted().Width(px(150)).Ellipsis()
					if ui.Button(c, "规则 "+item.Name).Ghost().XSmall().Icon(IconSliders).Clicked() {
						file := item.File
						c.After(func() { m.openMockRule(file) })
					}
				})
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.mockOpen = false
			}
			if ui.Button(c, "应用").Clicked() {
				c.After(m.applyMockEdits)
			}
		})
	})
}

// importDialog 是「导入集合」：Postman（v1 / v2）、Insomnia、OpenAPI（JSON / YAML）、
// 集合包、Bruno 与另一个 OpenCollection 目录都从这一个弹窗进来。
// 各来源落到同一个落点分组，导完就重扫，同名一律加 -2。
func (m *Model) importDialog(c ui.C) {
	ui.Modal(c, &m.ui.importOpen, "导入集合", ui.ModalSpec{Width: px(720), Height: px(520)}, func() {
		ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
			ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
				ui.Text(c, "来源").FontSize(fs(11)).Muted().Width(px(48))
				// 六种来源横向排不下（Choice 会平分宽度把「Insomnia 集合」截断），
				// 用下拉：与 Wails 版同一种交互，也不受弹窗宽度影响。
				ui.Select(c, &m.ui.importMode, importModes, func(mode int) string {
					return importModeNames[mode]
				}).Width(px(230))
				ui.Spacer(c)
				ui.Text(c, "导入到").FontSize(fs(11)).Muted().Width(px(48))
				ui.Select(c, &m.ui.importFolder, m.ui.importFolders, nil).Width(px(200))
			})
			if m.ui.importMode == 3 {
				ui.Text(c, "api-doc-go 集合包：把另一个集合的请求、文档与清单原样搬进来，同名文件自动加 -2。").
					FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					if ui.Button(c, "选择集合包…").Icon(IconFilePlus).Outline().Clicked() {
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
				ui.TextArea(c, &m.ui.importText, "集合包 JSON", `{"format":"api-doc-go/collection","files":[...]}`).
					FillWidth().Height(px(220))
			} else if m.ui.importMode == 5 {
				ui.Text(c, "Insomnia 导出的 insomnia.json（Export Data）：分组照层级搬过来，请求换新 uid；"+
					"查询参数 / 请求头 / body / bearer·basic·apikey 认证会转过来，脚本与断言忽略；"+
					"{{ _.变量 }} 会转成 {{变量}}。").
					FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					if ui.Button(c, "选择文件…").Icon(IconFilePlus).Outline().Clicked() {
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
				ui.TextArea(c, &m.ui.importText, "Insomnia 导出 JSON", `{"_type":"export","resources":[...]}`).
					FillWidth().Height(px(220))
			} else if m.ui.importMode == 2 {
				ui.Text(c, "OpenAPI 3 文档（JSON 或 YAML）：一个「路径 + 方法」导入成一条请求，"+
					"查询参数与请求头进表，描述进文档；$ref 不展开，YAML 会先转成 JSON。").
					FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					ui.Text(c, "地址前缀").FontSize(fs(11)).Muted().Width(px(64))
					base := ui.TextField(c, &m.ui.importBase, "地址前缀", "{{host}}").Width(px(260))
					m.varTipField(base, m.ui.importBase)
					ui.Text(c, "留空则只写路径").FontSize(fs(11)).Muted()
					ui.Spacer(c)
					if ui.Button(c, "选择文件…").Icon(IconFilePlus).Outline().Clicked() {
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
				})
				ui.TextArea(c, &m.ui.importText, "OpenAPI 文档", `{"openapi":"3.0.0","paths":{...}}`).
					FillWidth().Height(px(200))
			} else if m.ui.importMode == 4 {
				ui.Text(c, "原生 Bruno 集合（含 .bru 文件）：方法 / 地址 / 查询参数 / 请求头 / "+
					"body:json·text·form·multipart / bearer·basic·apikey 会转过来；脚本与断言忽略。").
					FontSize(11).Muted()
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					ui.TextField(c, &m.ui.importDir, "Bruno 目录", "/path/to/bruno/collection").Grow(1)
					if ui.Button(c, "选择目录…").Icon(IconFolderOpen).Outline().Clicked() {
						c.After(m.pickImportDir)
					}
				})
			} else if m.ui.importMode == 1 {
				ui.Text(c, "另一个 OpenCollection 集合：分组照层级复制，请求换成新 uid，文档进 docs/。").FontSize(fs(11)).Muted().FillWidth().MaxLines(2)
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					ui.TextField(c, &m.ui.importDir, "集合目录", "/path/to/collection").Grow(1)
					if ui.Button(c, "选择目录…").Icon(IconFolderOpen).Outline().Clicked() {
						c.After(m.pickImportDir)
					}
				})
			} else {
				ui.Text(c, "Postman v2 集合：方法 / 地址 / 查询参数 / 请求头 / raw·urlencoded·formdata body / "+
					"bearer·basic·apikey 认证会转过来；脚本与断言忽略。").FontSize(fs(11)).Muted().FillWidth().MaxLines(2)
				ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
					if ui.Button(c, "选择文件…").Icon(IconFilePlus).Outline().Clicked() {
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
					FillWidth().Height(px(220))
			}
			if m.ui.importErr != "" {
				ui.Text(c, m.ui.importErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
			if m.ui.importNote != "" {
				ui.Text(c, m.ui.importNote).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	importModes     = []int{0, 1, 2, 3, 4, 5}
	importModeNames = []string{"Postman 集合（v1 / v2）", "OpenCollection 集合", "OpenAPI 3（JSON / YAML）",
		"集合包（.apidocgo.json）", "Bruno 集合", "Insomnia 集合"}
)

// curlImportDialog 是「导入 cURL」：粘贴命令，解析成一条新请求（参考 19）。
func (m *Model) curlImportDialog(c ui.C) {
	ui.Modal(c, &m.ui.curlOpen, "导入 cURL", ui.ModalSpec{Width: px(720), Height: px(460)}, func() {
		ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
			ui.Text(c, "支持 Chrome / Firefox / Postman 的 Copy as cURL；-d/-F/-H/-u/-b/-A/-k/-m 会映射到请求。").
				FontSize(11).Muted()
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "导入到").FontSize(fs(11)).Muted().Width(px(48))
				ui.Select(c, &m.ui.curlFolder, m.ui.curlFolders, nil).Width(px(200))
				ui.Text(c, "请求名").FontSize(fs(11)).Muted().Width(px(48))
				ui.TextField(c, &m.ui.curlName, "请求名", "留空则用 URL 末段").Grow(1)
			})
			ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
				if ui.Button(c, "从剪贴板粘贴").Icon(IconCopy).Ghost().Clicked() {
					text, err := app.ClipboardText()
					if err != nil {
						m.ui.curlErr = err.Error()
					} else {
						m.ui.curlText = text
						m.ui.curlErr = ""
					}
				}
				ui.Text(c, "命令行把整段 curl 复制过来即可。").FontSize(fs(11)).Muted()
			})
			ui.TextArea(c, &m.ui.curlText, "curl 命令", "curl 'https://api.example.com/users'").
				FillWidth().Height(px(210))
			if m.ui.curlErr != "" {
				ui.Text(c, m.ui.curlErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.trashOpen, "回收站", ui.ModalSpec{Width: px(720), Height: px(460)}, func() {
		ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
			ui.Text(c, "恢复的文件会回到集合根；同名时自动补「-恢复2」后缀，不覆盖。").
				FontSize(11).Muted()
			if len(m.ui.trashRows) == 0 {
				ui.Text(c, "回收站是空的").Muted()
				return
			}
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "勾选后可在底部批量恢复或彻底删除").FontSize(fs(11)).Muted()
				ui.Spacer(c)
				if ui.Button(c, "全选").Ghost().Small().Clicked() {
					c.After(func() { m.toggleAllTrash(true) })
				}
				if ui.Button(c, "全不选").Ghost().Small().Clicked() {
					c.After(func() { m.toggleAllTrash(false) })
				}
			})
			for index := range m.ui.trashRows {
				item := &m.ui.trashRows[index]
				selected := m.ui.trashSel[item.File]
				ui.Row(c).FillWidth().PadY(px(4)).Gap(px(8)).Items(ui.Center).Children(func() {
					if ui.Checkbox(c, &selected, item.Name).Changed() {
						// 勾选状态存进 map（行是每次重扫重建的，别挂在行结构上）。
						file, on := item.File, selected
						c.After(func() { m.ui.trashSel[file] = on })
					}
					ui.Text(c, item.File).FontSize(fs(10)).Mono().Muted().Ellipsis()
					ui.Spacer(c)
					trashFile := item.File
					if ui.Button(c, "恢复").Small().Clicked() {
						c.After(func() { m.restoreTrash(trashFile) })
					}
				})
			}
			if m.ui.trashMsg != "" {
				ui.Text(c, m.ui.trashMsg).FontSize(fs(11)).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			purgeLabel := "彻底删除"
			if m.ui.trashArmed {
				purgeLabel = "确认彻底删除"
			}
			if ui.Button(c, purgeLabel).Icon(IconTrash).Destructive().Clicked() {
				c.After(m.purgeSelectedTrash)
			}
			if ui.Button(c, "恢复选中").Outline().Clicked() {
				c.After(m.restoreSelectedTrash)
			}
			if ui.Button(c, "关闭").Clicked() {
				m.ui.trashOpen = false
			}
		})
	})
}

// envDialog 是「环境变量」：左侧环境列表，右侧变量表；敏感值单独写 *.secrets.yml。
func (m *Model) envDialog(c ui.C) {
	theme := c.Theme()
	ui.Modal(c, &m.ui.envOpen, "环境变量", ui.ModalSpec{Width: px(880), Height: px(520)}, func() {
		ui.Row(c).FillWidth().Gap(px(16)).Children(func() {
			ui.Column(c).Width(px(180)).Gap(px(6)).Children(func() {
				for _, name := range m.ui.envs {
					current := name
					active := current == m.ui.envSel
					row := ui.Row(c).FillWidth().PadX(px(10)).PadY(px(5)).Gap(px(6)).Items(ui.Center).
						Radius(theme.Metrics.RadiusControl).
						Hover(ui.Look{}.Background(theme.Hover)).
						Hit(ui.ID(c), "环境 "+current)
					if active {
						row.Background(m.pal.active)
					}
					row.Children(func() {
						text := ui.Text(c, current).FontSize(fs(12))
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
			ui.Column(c).Grow(1).Gap(px(10)).Children(func() {
				ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
					ui.Text(c, "环境名").FontSize(fs(11)).Muted().Width(px(52))
					ui.TextField(c, &m.ui.envNameEdit, "环境名", "dev").Width(px(220))
				})
				hint := "敏感值单独存 environments/<环境>.secrets.yml，落盘时加密（密钥在本机配置目录）；" +
					"没有密钥的机器上只能看到密文，集合进 git 也只留密文。"
				if !secrets.Enabled() {
					hint = "注意：设置里关掉了「加密敏感值」，新保存的敏感值会以明文写进 " +
						"environments/<环境>.secrets.yml（记得别提交）。"
				}
				ui.Text(c, hint).FontSize(fs(11)).Muted()
				ui.Column(c).FillWidth().Pad(px(8)).Gap(px(6)).Radius(theme.Metrics.RadiusControl).
					Border(1, m.pal.border).Children(func() {
					ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
						ui.Text(c, "启用").FontSize(fs(11)).Muted().Width(px(36))
						ui.Text(c, "变量名").FontSize(fs(11)).Muted().Width(px(150))
						ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(1)
						ui.Text(c, "敏感值").FontSize(fs(11)).Muted().Width(px(56))
						ui.Slot(c).Width(px(28))
					})
					// 变量表用 ui.List：按住行拖动即可重排（ListState.Reorder）。
					ui.List(c, &m.ui.envList, len(m.ui.envRows), func(r ui.ListRow) {
						row := m.ui.envRows[r.Index]
						slot := r.Index
						r.Element.Gap(px(8)).Items(ui.Center).Children(func() {
							ui.Checkbox(c, &row.enabled, "")
							ui.TextField(c, &row.name, "变量名", "host").Width(px(150))
							ui.TextField(c, &row.value, "值", "http://127.0.0.1:8080").Grow(1)
							ui.Checkbox(c, &row.secret, "")
							// 行尾也有上移 / 下移：拖拽之外给一个稳妥的键盘/点击路径。
							m.rowMover(c, slot, len(m.ui.envRows), func(from, to int) {
								if moveItem(m.ui.envRows, from, to) {
									m.invalidator.Invalidate()
								}
							})
							if ui.Button(c, "删除变量").Icon(IconX).IconOnly().Ghost().Tooltip("删除变量").Clicked() {
								c.After(func() {
									m.ui.envRows = append(m.ui.envRows[:slot], m.ui.envRows[slot+1:]...)
								})
							}
						})
					}).FillWidth().Height(px(240))
				})
				ui.Row(c).FillWidth().Children(func() {
					if ui.Button(c, "添加变量").Icon(IconPlus).Ghost().Clicked() {
						m.ui.envRows = append(m.ui.envRows, &kvRow{enabled: true})
					}
				})
				if m.ui.envErr != "" {
					ui.Text(c, m.ui.envErr).FontSize(fs(11)).Color(theme.Danger)
				}
				ui.Spacer(c)
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.newEnvOpen, "新建环境", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "环境是 environments/<名称>.yml，与客户端同一个位置。").FontSize(fs(11)).Muted()
			ui.TextField(c, &m.ui.newEnvName, "环境名", "例如：staging").FillWidth()
			if m.ui.newEnvErr != "" {
				ui.Text(c, m.ui.newEnvErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
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
	ui.Modal(c, &m.ui.pathOpen, "打开集合", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "输入集合目录的绝对路径（目录里应有 opencollection.yml）。").FontSize(fs(11)).Muted()
			ui.TextField(c, &m.ui.pathText, "集合目录", "/home/you/collections/demo").FillWidth()
			if m.ui.openErr != "" {
				ui.Text(c, m.ui.openErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.pathOpen = false
			}
			if ui.Button(c, "打开").Clicked() {
				c.After(m.openPathInput)
			}
		})
	})

	ui.Modal(c, &m.ui.initOpen, "初始化集合", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
			ui.Text(c, m.ui.pendingDir).FontSize(fs(11)).Muted().Mono()
			ui.Text(c, "这个目录里没有 opencollection.yml。初始化会在目录里写一份清单，").FontSize(fs(12))
			ui.Text(c, "目录里已有的 .yml 会被识别为本项目的接口。取消则不载入、不写盘。").FontSize(fs(12))
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.initOpen = false
			}
			if ui.Button(c, "初始化并打开").Clicked() {
				c.After(m.confirmInit)
			}
		})
	})

	ui.Modal(c, &m.ui.envRenameOpen, "重命名环境", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "会同时重命名 <名称>.yml 与 <名称>.secrets.yml。").FontSize(fs(11)).Muted()
			ui.TextField(c, &m.ui.envRenameName, "新环境名", "staging").FillWidth()
			if m.ui.envErr != "" {
				ui.Text(c, m.ui.envErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envRenameOpen = false
			}
			if ui.Button(c, "重命名环境").Clicked() {
				c.After(m.renameEnv)
			}
		})
	})

	ui.Modal(c, &m.ui.envDeleteOpen, "删除环境", ui.ModalSpec{}, func() {
		ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
			ui.Text(c, "环境："+m.ui.envPick).FontSize(fs(12)).Bold()
			ui.Text(c, "主文件与密钥文件都会移进集合的 .trash/，不会直接删除。").FontSize(fs(11)).Muted()
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				m.ui.envDeleteOpen = false
			}
			if ui.Button(c, "删除环境").Destructive().Clicked() {
				c.After(m.deleteEnv)
			}
		})
	})
}
