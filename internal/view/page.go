package view

import (
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// Page 声明整窗：工具条 + （集合树 | 分隔条 | 详情）。
// 搜索框在 sidebar 里先声明、当帧写入过滤词，随后重建树，避免一帧延迟。
func (m *Model) Page(c ui.C) {
	if ui.Shortcut(c, ui.Primary, "k") {
		c.After(func() {
			if m.ui.paletteOpen {
				m.ui.paletteOpen = false
				return
			}
			m.openPalette()
		})
	}
	m.toolbar(c)
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(m.split.Pos).FillHeight().Background(m.pal.sidebar).Children(func() {
			m.sidebar(c)
		})
		ui.Splitter(c, &m.split).Mins(240, 420)
		ui.Column(c).Grow(1).FillHeight().Background(m.pal.panel).Children(func() {
			m.tabBar(c)
			if m.overview {
				m.overviewPage(c)
			} else {
				m.detail(c, m.activeEntry())
			}
		})
	})
	m.dialogs(c)
}

// appVersion 是顶栏展示的版本号（与窗口标题一起构成产品标识）。
const appVersion = "v0.1.0"

// toolbar 是顶部一条：左列产品名 + 版本，右列从请求页左边缘开始放环境与动作。
func (m *Model) toolbar(c ui.C) {
	ui.Row(c).FillWidth().Height(44).Items(ui.Center).Children(func() {
		ui.Row(c).Width(m.split.Pos).Height(44).PadX(12).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, "api-doc-go").FontSize(15).Bold()
			ui.Text(c, appVersion).FontSize(11).Muted().Mono()
		})
		ui.Row(c).Grow(1).Height(44).PadLeft(24).PadRight(12).Gap(8).Items(ui.Center).Children(func() {
			m.envPicker(c)
			ui.Spacer(c)
			themeLabel, themeIcon := "日间", IconSun
			if m.isLight() {
				themeLabel, themeIcon = "夜间", IconMoon
			}
			if ui.Button(c, themeLabel).Icon(themeIcon).Clicked() {
				m.setTheme(!m.isLight())
			}
			if ui.Button(c, "设置").Icon(IconSliders).Clicked() {
				m.openSettings()
			}
		})
	})
	ui.Divider(c)
}

// envPicker 是环境下拉：只有环境列表与「环境设置…」。
// 放在标签栏右侧，与请求页同在内容区，不再是标题栏的一部分。
func (m *Model) envPicker(c ui.C) {
	if len(m.ui.envs) == 0 {
		return
	}
	label := "无环境"
	if m.envName != "" {
		label = m.envName
	}
	envButton := ui.Button(c, "环境 "+label).Icon(IconGlobe).Secondary().Tooltip("切换环境")
	if envButton.Clicked() {
		c.After(func() { m.envMenuOpen = !m.envMenuOpen })
	}
	if m.envMenuOpen {
		ui.Menu(c, envButton, &m.envMenuOpen, func(menu *ui.MenuItems) {
			for _, name := range m.ui.envs {
				envName := name
				item := menu.Item(envName)
				if envName == m.envName {
					item.Checked(true)
				}
				if item.Clicked() {
					c.After(func() { m.applyEnv(envName) })
				}
			}
			menu.Separator()
			if menu.Item("环境设置…").Icon(IconSliders).Clicked() {
				c.After(func() { m.openEnvEditorFor(m.envName) })
			}
		})
	}
}

// sidebar 是左侧集合树：顶部动作 + 搜索 + 树（集合根 / 分组 / 请求 / 文档）。
func (m *Model) sidebar(c ui.C) {
	m.sideC = c
	ui.Column(c).Fill().Pad(6).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(4).Items(ui.Center).Children(func() {
			if ui.Button(c, "打开集合").Icon(IconFolderOpen).IconOnly().Tooltip("打开集合").Clicked() {
				c.After(m.chooseCollection)
			}
			ui.Spacer(c)
			if ui.Button(c, "新建请求").Icon(IconPlus).IconOnly().Tooltip("新建请求").Clicked() {
				m.openNewRequest()
			}
			if ui.Button(c, "新建文档").Icon(IconFilePlus).IconOnly().Tooltip("新建文档").Clicked() {
				c.After(m.createDoc)
			}
			if ui.Button(c, "导入 cURL").Icon(IconImport).IconOnly().Tooltip("导入 cURL").Clicked() {
				m.openCurlImport()
			}
		})
		if ui.TextField(c, &m.filter, "搜索请求", "搜索请求…").Icon(IconSearch).FillWidth().Changed() {
			m.shownOnce = false
		}
		m.refreshShown()
		m.syncTree()
		m.applySelect()
		m.applyPendingSelect()
		m.flushHoverExpand()
		if m.createErr != "" {
			ui.Text(c, m.createErr).FontSize(11).Color(c.Theme().Danger)
		}
		if len(m.rows) == 0 {
			emptyBox(c, "没有匹配的请求", "换个关键字，或清空搜索框")
			return
		}
		ui.Tree(c, &m.tree, len(m.rows), m.treeLabel, m.treeContent).Grow(1)
		if index, ok := m.tree.Toggled(); ok {
			m.toggleRow(index)
		} else if index, ok := m.tree.Chosen(); ok {
			m.chooseRow(index)
		} else if move, ok := m.tree.Dropped(); ok {
			m.dropRow(move)
		}
	})
}

// tabWidth 是标签的固定宽度：标签多了才能算出一屏放几个并滚动。
const tabWidth float32 = 132

// tabBar 是详情区顶部的标签条：固定的「集合」页 + 可横向滚动的条目标签。
// 标签超出可用宽度时出现「‹ ›」按钮，切标签会自动把当前标签滚进可视区。
func (m *Model) tabBar(c ui.C) {
	ui.Column(c).FillWidth().Background(m.pal.sidebar).Children(func() {
		ui.Row(c).FillWidth().Height(37).PadX(6).Gap(2).Items(ui.Center).Children(func() {
			m.pinnedTab(c)
			capacity := m.tabCapacity(c)
			m.clampTabStart(capacity)
			if m.tabStart > 0 {
				if ui.Button(c, "左移标签").Icon(IconChevronLeft).IconOnly().Ghost().Tooltip("向左滚动标签").Clicked() {
					c.After(func() { m.tabStart = max(0, m.tabStart-1) })
				}
			}
			end := min(len(m.tabs), m.tabStart+capacity)
			for index := m.tabStart; index < end; index++ {
				m.tabItem(c, m.tabs[index], index, !m.overview && index == m.curTab)
			}
			if end < len(m.tabs) {
				if ui.Button(c, "右移标签").Icon(IconChevronRight).IconOnly().Ghost().Tooltip("向右滚动标签").Clicked() {
					c.After(func() {
						m.tabStart = min(m.tabStart+1, max(0, len(m.tabs)-capacity))
					})
				}
			}
			ui.Spacer(c)
			if ui.Button(c, "新建临时请求").Icon(IconPlus).IconOnly().Ghost().Tooltip("新建临时请求").Clicked() {
				c.After(m.openScratch)
			}
		})
		ui.Divider(c)
	})
}

// tabCapacity 算一屏能放几个标签：预留固定的「集合」页、＋ 与滚动箭头。
func (m *Model) tabCapacity(c ui.C) int {
	strip := c.Bounds().Width - m.split.Pos - 8 - 12
	if strip < 180 {
		strip = 180
	}
	reserved := float32(64 + 40)
	capacity := int((strip - reserved) / (tabWidth + 2))
	if capacity < 1 {
		capacity = 1
	}
	if len(m.tabs) > capacity {
		capacity = int((strip - reserved - 68) / (tabWidth + 2))
		if capacity < 1 {
			capacity = 1
		}
	}
	return capacity
}

// clampTabStart 把滚动起点收进合法范围，并保证当前标签可见。
func (m *Model) clampTabStart(capacity int) {
	if m.tabStart > len(m.tabs)-capacity {
		m.tabStart = max(0, len(m.tabs)-capacity)
	}
	if m.curTab != m.tabActive {
		m.tabActive = m.curTab
		if m.curTab >= 0 && !m.overview {
			if m.curTab < m.tabStart {
				m.tabStart = m.curTab
			}
			if m.curTab >= m.tabStart+capacity {
				m.tabStart = m.curTab - capacity + 1
			}
			if m.tabStart < 0 {
				m.tabStart = 0
			}
		}
	}
}

// pinnedTab 是固定在最左的「集合」页：点它回到概览。
func (m *Model) pinnedTab(c ui.C) {
	theme := c.Theme()
	ui.Column(c).Width(68).Gap(0).Children(func() {
		row := ui.Row(c).Height(35).PadX(10).Gap(6).Items(ui.Center).
			Hover(ui.Look{}.Background(theme.Hover)).
			Hit(ui.ID(c), "标签 集合")
		if m.overview {
			row.Background(theme.Card)
		}
		row.Children(func() {
			ui.Text(c, "集合").FontSize(12.5)
		})
		if row.Clicked() {
			c.After(func() { m.overview = true })
		}
		line := ui.RGBA(0, 0, 0, 0)
		if m.overview {
			line = theme.Primary
		}
		ui.Slot(c).FillWidth().Height(2).Background(line)
	})
}

// tabItem 是一个条目标签：方法 + 名称 + 关闭按钮，选中时白底 + 绿色下划线。
func (m *Model) tabItem(c ui.C, tab *tabState, slot int, active bool) {
	theme := c.Theme()
	title := m.tabTitle(tab)
	ui.Column(c).Gap(0).Children(func() {
		row := ui.Row(c).Height(35).PadX(8).Gap(6).Items(ui.Center).
			Hover(ui.Look{}.Background(theme.Hover)).
			Hit(ui.ID(c), "标签 "+title)
		if active {
			row.Background(theme.Card)
		}
		row.Children(func() {
			if tab.entry != nil && tab.entry.Kind == explorer.KindHTTP {
				ui.Text(c, tab.entry.Method).FontSize(9).Mono().Muted()
			} else if tab.scratch {
				ui.Text(c, tab.req.method).FontSize(9).Mono().Muted()
			}
			ui.Text(c, title).FontSize(12.5).Ellipsis()
			if ui.Button(c, "关闭 "+title).Icon(IconX).IconOnly().Ghost().Tooltip("关闭标签").Clicked() {
				c.After(func() { m.closeTab(slot) })
			}
		})
		ui.ContextMenu(c, row, func(menu *ui.MenuItems) {
			if menu.Item("关闭").Clicked() {
				c.After(func() { m.closeTab(slot) })
			}
			if menu.Item("关闭其他").Clicked() {
				c.After(func() { m.closeOthers(slot) })
			}
			if menu.Item("关闭右侧").Clicked() {
				c.After(func() { m.closeRight(slot) })
			}
			if !tab.scratch && tab.file != "" {
				if menu.Item("复制请求路径").Clicked() {
					file := tab.file
					c.After(func() { m.copyText(file) })
				}
			}
		})
		if row.Clicked() {
			c.After(func() { m.activateTab(slot) })
		}
		line := ui.RGBA(0, 0, 0, 0)
		if active {
			line = theme.Primary
		}
		ui.Slot(c).FillWidth().Height(2).Background(line)
	})
}

// detail 是右侧：请求按 Wails 的排版——标题、地址栏、分段页签、响应区（可拖拽分隔）。
func (m *Model) detail(c ui.C, entry *explorer.Entry) {
	if entry == nil {
		ui.Column(c).Fill().Pad(24).Children(func() {
			emptyBox(c, "选择左侧的一条内容", "请求会显示地址与响应，文档会打开编辑器")
		})
		return
	}
	m.ensureEntry(entry)
	if entry.IsDoc() {
		m.docDetail(c, entry)
		return
	}
	if entry.Kind != explorer.KindHTTP {
		ui.Column(c).Fill().Pad(20).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
				methodBadge(c, entry)
				ui.Text(c, entry.Name).FontSize(17).Bold()
				ui.Spacer(c)
				ui.Text(c, "gRPC 只读").FontSize(11).Muted()
			})
			ui.Text(c, m.tables.metaLine).FontSize(11).Muted().Mono()
			ui.Scroll(c).Grow(1).Children(func() {
				m.grpcTab(c, entry)
			})
		})
		return
	}
	ui.Column(c).Fill().Pad(16).Gap(10).Children(func() {
		m.requestTitle(c, entry)
		m.requestBar(c, entry)
		ui.Tabs(c, &m.tab, httpSegments, tabLabel).Line()
		ui.Column(c).FillWidth().Height(m.rsplit.Pos).Children(func() {
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					switch m.tab {
					case tabBody:
						m.bodyTab(c)
					case tabHeaders:
						m.kvEditor(c, &m.req.headers, "添加请求头")
					case tabAuth:
						m.authTab(c)
					case tabDocs:
						m.docsTab(c, entry)
					default:
						m.kvEditor(c, &m.req.params, "添加参数")
					}
					if m.req.err != "" {
						ui.Text(c, m.req.err).FontSize(11).Color(c.Theme().Danger)
					}
				})
			})
		})
		ui.Splitter(c, &m.rsplit).Mins(200, 120)
		ui.Column(c).Grow(1).FillWidth().Children(func() {
			m.responsePanel(c, entry)
		})
	})
}
