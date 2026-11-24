package view

import (
	"path/filepath"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// Page 声明整窗：工具条 + （集合树 | 分隔条 | 详情）。
// 搜索框在 sidebar 里先声明、当帧写入过滤词，随后重建树，避免一帧延迟。
func (m *Model) Page(c ui.C) {
	m.drainMCP()
	m.drainRun()
	m.drainGRPC()
	// 上一帧的编辑在这里落成一步撤销（按 400ms 合并连续输入）。
	m.captureUndo()
	// 自动保存：定时器只叫醒窗口，真正落盘在帧首做（与绘制串行）。
	m.pumpAutoSave()
	if ui.Shortcut(c, ui.Primary, "k") {
		c.After(func() {
			if m.ui.paletteOpen {
				m.ui.paletteOpen = false
				return
			}
			m.openPalette()
		})
	}
	// 命令面板里写了提示的快捷键必须真的能用（paletteGroups 的 hint）。
	if ui.Shortcut(c, ui.Primary, "n") {
		c.After(m.openNewRequest)
	}
	if ui.Shortcut(c, ui.Primary, "w") {
		c.After(func() { m.closeTab(m.curTab) })
	}
	if ui.Shortcut(c, ui.Primary, "/") {
		c.After(m.openShortcuts)
	}
	if ui.Shortcut(c, ui.Primary, ",") {
		c.After(m.openSettings)
	}
	// 撤销只对「正在编辑的请求」有意义：概览页没有草稿。
	if !m.overview && (m.scratch || m.req.file != "") {
		m.undoShortcuts(c)
	}
	m.toolbar(c)
	if !m.hasCollection() {
		m.welcomePage(c)
		m.statusBar(c)
		m.dialogs(c)
		return
	}
	ui.Row(c).Grow(1).FillWidth().Children(func() {
		// Shrink(0)：侧栏宽度只由分隔条决定。详情页的内容（请求列 + 响应列）一宽，
		// 可收缩的侧栏就会被分走几十像素——用户看到的就是「切到请求标签，集合栏变窄」。
		ui.Column(c).Width(m.split.Pos).Shrink(0).FillHeight().Background(m.pal.sidebar).Children(func() {
			m.sidebar(c)
		})
		ui.Splitter(c, &m.split).Mins(px(240), px(420))
		// MinWidth(0)：详情页自己会声明最小宽度（请求列 + 响应列），这里再要求一份就会
		// 反过来挤压侧栏——切到请求标签时「集合栏」忽然变窄，就是这个链路。
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(m.pal.panel).Children(func() {
			m.tabBar(c)
			if m.overview {
				m.overviewPage(c)
			} else {
				m.detail(c, m.activeEntry())
			}
		})
	})
	m.statusBar(c)
	m.dialogs(c)
}

// hasCollection 报告当前有没有可用的集合根（双击启动时可能还没有）。
func (m *Model) hasCollection() bool {
	return m.col != nil && m.col.Dir != ""
}

// welcomePage 是还没打开集合时的首屏：一个说明 + 打开集合 / 打开上次集合。
func (m *Model) welcomePage(c ui.C) {
	ui.Column(c).Fill().Items(ui.Center).Justify(ui.Center).Gap(px(12)).Children(func() {
		ui.Text(c, "尚未打开集合").FontSize(fs(20)).Bold()
		ui.Text(c, "选择一个含 opencollection.yml 的目录，或在命令行用 -collection 指定。").FontSize(fs(12)).Muted()
		ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.Button(c, "打开集合").Icon(IconFolderOpen).Clicked() {
				c.After(m.chooseCollection)
			}
			if m.settings.LastCollection != "" {
				if ui.Button(c, "打开上次的集合").Secondary().Clicked() {
					c.After(func() { m.openCollection(m.settings.LastCollection) })
				}
			}
		})
	})
}

// appVersion 是顶栏展示的版本号（与窗口标题一起构成产品标识）。
const appVersion = "v0.1.0"

// titlebarHeight 是标题栏高度，与 Wails 版的 --app-titlebar-h 一致。
var titlebarHeight float32 = 44

// 标题栏图标按钮的无障碍名称：按钮上不再写字，读屏软件靠这个说清点下去会怎样。
const (
	splitSideLabel  = "左右分栏"
	splitStackLabel = "上下分栏"
	themeDarkLabel  = "切换到夜间"
	themeLightLabel = "切换到日间"
)

// toolbar 是顶部一条：左列产品名 + 版本，右列从请求页左边缘开始放环境与动作。
// 窗口是无边框的（见 config 的 WindowCustomTitlebar），这一条**就是标题栏**：
// 整行是拖动区（MovesWindow），空白处按下拖走窗口，控件自己吃点击。
// 右端 3×46 留给 viva 自绘的最小化 / 最大化 / 关闭，所以右侧要多留 TitlebarInsets 的尾宽。
func (m *Model) toolbar(c ui.C) {
	_, trailing := app.TitlebarInsets()
	ui.Row(c).FillWidth().Height(titlebarHeight).Items(ui.Center).MovesWindow().Children(func() {
		ui.Row(c).Width(m.split.Pos).Height(titlebarHeight).PadX(px(12)).Gap(px(8)).Items(ui.Center).Children(func() {
			ui.Text(c, "api-doc-go").FontSize(fs(15)).Bold()
			ui.Text(c, appVersion).FontSize(fs(11)).Muted().Mono()
		})
		// PadLeft(6)：集合 / 环境下拉的左缘与下方标签条、内容区左侧对齐（侧栏右框的 x）。
		ui.Row(c).Grow(1).Height(titlebarHeight).PadLeft(6).PadRight(trailing + 12).Gap(px(8)).Items(ui.Center).Children(func() {
			m.collectionPicker(c)
			m.envPicker(c)
			ui.Spacer(c)
			m.splitToggle(c)
			m.themeToggle(c)
		})
	})
	ui.Divider(c)
}

// splitToggle 是分栏方向的分段按钮：只有图标，外面一圈细描边，选中项浅绿底 + 主色图标。
// 两个方向同时摊开，比一个循环切换的按钮更直白——一眼看得出当前是左右还是上下。
func (m *Model) splitToggle(c ui.C) {
	side := m.settings.SplitSideBySide
	ui.Row(c).Border(1, m.pal.border).Radius(px(8)).Pad(px(2)).Gap(px(2)).Items(ui.Center).Children(func() {
		// 点当前已选中的那一段不算切换：setSplitLayout 会把分隔条位置重置，
		// 也会多写一次偏好。只有真的换方向才动。
		m.iconToggle(c, IconColumns, splitSideLabel, side, func() {
			if !m.settings.SplitSideBySide {
				m.setSplitLayout(true)
			}
		})
		m.iconToggle(c, IconRows, splitStackLabel, !side, func() {
			if m.settings.SplitSideBySide {
				m.setSplitLayout(false)
			}
		})
	})
}

// themeToggle 是主题切换：同样只有一个图标（与参考图的月亮一致），点一下换主题。
func (m *Model) themeToggle(c ui.C) {
	icon, label := IconSun, themeLightLabel
	if m.isLight() {
		icon, label = IconMoon, themeDarkLabel
	}
	m.iconToggle(c, icon, label, false, func() { m.setTheme(!m.isLight()) })
}

// iconToggle 是一个只有图标的小方块：选中时铺主色浅底、图标用主色；悬停给一层灰底。
// 按下后交给 c.After，和别的按钮一样不在一帧里改状态。
func (m *Model) iconToggle(c ui.C, icon gfx.Icon, label string, active bool, onClick func()) {
	box := ui.Row(c).Size(px(28), px(24)).Shrink(0).Center().Radius(px(6))
	if active {
		box = box.Background(m.pal.accentTint)
	}
	box = box.Transition().
		Hover(ui.Look{}.Background(m.pal.hover)).
		Focus(ui.Look{}.Ring(2, m.theme.Ring)).
		Hit(ui.ID(c), label)
	if box.Clicked() {
		c.After(onClick)
	}
	ink := m.theme.MutedForeground
	if active {
		ink = m.pal.accent
	}
	box.Children(func() {
		glyph := ui.Icon(c, icon).Size(px(15), px(15)).Color(ink)
		if !active {
			// 未选中：图标平时是次要色，指针移上来才提到正文色。
			glyph.StateFrom(box).Hover(ui.Look{}.Color(m.theme.Foreground))
		}
	})
}

// collectionPicker 是集合切换下拉：已打开的集合根、打开另一个集合、关闭当前集合。
// 重名集合带上目录名尾巴，避免下拉里出现两个一模一样的名字。
func (m *Model) collectionPicker(c ui.C) {
	label := "未打开集合"
	if m.col != nil && m.col.Name != "" {
		label = m.collectionLabel(m.active)
	}
	button := ui.Button(c, label).Icon(IconFolderOpen).Outline().Weight(gfx.WeightRegular).Tooltip("切换集合")
	if button.Clicked() {
		c.After(func() { m.collectionMenuOpen = !m.collectionMenuOpen })
	}
	if !m.collectionMenuOpen {
		return
	}
	ui.Menu(c, button, &m.collectionMenuOpen, func(menu *ui.MenuItems) {
		for index := range m.roots {
			if m.roots[index].col.Dir == "" {
				continue
			}
			slot := index
			item := menu.Item(m.collectionLabel(slot))
			if slot == m.active {
				item.Checked(true)
			}
			if item.Clicked() {
				c.After(func() { m.activateRoot(slot) })
			}
		}
		menu.Separator()
		if menu.Item("打开集合…").Icon(IconFolderOpen).Clicked() {
			c.After(m.chooseCollection)
		}
		if menu.Item("导入 Postman 集合…").Icon(IconImport).Clicked() {
			c.After(func() { m.openImport(0) })
		}
		if menu.Item("导入 OpenCollection 集合…").Icon(IconImport).Clicked() {
			c.After(func() { m.openImport(1) })
		}
		if menu.Item("导入集合包…").Icon(IconImport).Clicked() {
			c.After(func() { m.openImport(3) })
		}
		if menu.Item("导入 Bruno 集合…").Icon(IconImport).Clicked() {
			c.After(func() { m.openImport(4) })
		}
		if menu.Item("运行集合…").Icon(IconSend).Clicked() {
			c.After(m.openRunner)
		}
		menu.Separator()
		if menu.Item("导出 Markdown…").Icon(IconImport).Clicked() {
			c.After(func() { m.openExport("md") })
		}
		if menu.Item("导出 HTML…").Icon(IconImport).Clicked() {
			c.After(func() { m.openExport("html") })
		}
		if menu.Item("导出集合包…").Icon(IconImport).Clicked() {
			c.After(func() { m.exportCollection("bundle") })
		}
		if menu.Item("导出 OpenAPI…").Icon(IconImport).Clicked() {
			c.After(func() { m.exportCollection("openapi") })
		}
		closeItem := menu.Item("关闭当前集合")
		if len(m.roots) <= 1 {
			closeItem.Disabled(true)
		}
		if closeItem.Clicked() {
			c.After(func() { m.closeRoot(m.active) })
		}
	})
}

// collectionLabel 是下拉里的集合名：同名时补上目录名，便于区分。
func (m *Model) collectionLabel(index int) string {
	if index < 0 || index >= len(m.roots) {
		return "未打开集合"
	}
	root := m.roots[index]
	if root.col.Dir == "" {
		return "未打开集合"
	}
	name := root.col.Name
	if name == "" {
		name = filepath.Base(root.col.Dir)
	}
	for i, other := range m.roots {
		if i != index && other.col.Name == root.col.Name {
			return name + " · " + filepath.Base(root.col.Dir)
		}
	}
	return name
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
	envButton := ui.Button(c, "环境 "+label).Icon(IconGlobe).Outline().Weight(gfx.WeightRegular).Tooltip("切换环境")
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
	ui.Column(c).Fill().Pad(px(6)).Gap(px(6)).Children(func() {
		ui.Row(c).FillWidth().Gap(px(4)).Items(ui.Center).Children(func() {
			if ui.Button(c, "打开集合").Icon(IconFolderOpen).Outline().IconOnly().Tooltip("打开集合").Clicked() {
				c.After(m.chooseCollection)
			}
			ui.Spacer(c)
			if ui.Button(c, "新建请求").Icon(IconPlus).Outline().IconOnly().Tooltip("新建请求").Clicked() {
				m.openNewRequest()
			}
			if ui.Button(c, "新建文档").Icon(IconFilePlus).Outline().IconOnly().Tooltip("新建文档").Clicked() {
				c.After(m.createDoc)
			}
			if ui.Button(c, "导入 cURL").Icon(IconImport).Outline().IconOnly().Tooltip("导入 cURL").Clicked() {
				m.openCurlImport("")
			}
			sortBtn := ui.Button(c, "排序").Icon(IconSliders).Outline().IconOnly().
				Tooltip("排序：" + sortLabel(m.sortMode))
			if sortBtn.Clicked() {
				m.sortMenuOpen = !m.sortMenuOpen
			}
			if m.sortMenuOpen {
				ui.Menu(c, sortBtn, &m.sortMenuOpen, func(menu *ui.MenuItems) {
					menu.Label("排序")
					for _, item := range []struct{ mode, label string }{
						{"", "磁盘顺序"},
						{"name-asc", "名称 A→Z"},
						{"name-desc", "名称 Z→A"},
					} {
						mode := item.mode
						if menu.Item(item.label).Checked(m.sortMode == mode).Clicked() {
							c.After(func() { m.setSortMode(mode) })
						}
					}
				})
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
			ui.Text(c, m.createErr).FontSize(fs(11)).Color(c.Theme().Danger)
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

// tabWidth 是标签的固定宽度：宽度一致，横向滚动的偏移才好算。
var tabWidth float32 = 132

// tabPitch 是一个标签占的横向距离（标签 + 行间距）。
var tabPitch float32 = tabWidth + 2

// tabBar 是详情区顶部的标签条：固定的「集合」页 + 可横向滚动的条目标签。
//
// 超出宽度靠横向滚动（Shift + 滚轮、触控板横扫），不再做「一屏 N 个 + 左右箭头」的分页：
// 分页会让第 N+1 个标签凭空消失，而滚动是连续、可预期的。右侧「⋯」列出全部标签，
// 给不方便横扫的场景兜底。
func (m *Model) tabBar(c ui.C) {
	// 每帧重建「点击身份 → 槽位」的映射：拖拽回调只拿到身份。
	m.tabSlotIDs = m.tabSlotIDs[:0]
	m.tabSlots = make(map[gfx.ID]int, len(m.tabs)*2)
	ui.Column(c).FillWidth().Background(m.pal.sidebar).Children(func() {
		ui.Row(c).FillWidth().Height(px(37)).PadX(px(6)).Gap(px(2)).Items(ui.Center).Children(func() {
			m.pinnedTab(c)
			// 装不下时两端各一颗滚动按钮；左箭头在 strip 之前声明，
			// 所以用的是上一帧量出的 tabOverflows。
			if m.tabOverflows {
				m.tabArrow(c, -1)
			}
			strip := ui.Scroll(c).Grow(1).Height(px(37)).Sideways()
			strip.Children(func() {
				ui.Row(c).Gap(px(2)).Height(px(37)).Items(ui.Center).Children(func() {
					for index, tab := range m.tabs {
						m.tabItem(c, tab, index, !m.overview && index == m.curTab)
					}
				})
			})
			m.followTab(strip)
			if m.tabOverflows {
				m.tabArrow(c, 1)
			}
			m.tabOverflow(c)
			if ui.Button(c, "新建临时请求").Icon(IconPlus).IconOnly().Ghost().Tooltip("新建临时请求").Clicked() {
				c.After(m.openScratch)
			}
		})
		ui.Divider(c)
	})
}

// followTab 让页签条跟着当前标签走：换根后先恢复上次的位置，再保证当前标签可见。
func (m *Model) followTab(scroll ui.Box) {
	state := scroll.ScrollState()
	if state == nil {
		return
	}
	if m.tabScrollRestore {
		m.tabScrollRestore = false
		state.ScrollToX(m.tabScrollWant)
	}
	if m.tabFollowed != m.curTab {
		state.ScrollToX(tabScrollTarget(m.curTab, m.overview, state.OffsetX, state.ViewportWidth))
		m.tabFollowed = m.curTab
	}
	m.tabScrollX = state.OffsetX
	m.tabStrip = state
	overflow := state.ContentWidth > state.ViewportWidth+0.5
	if overflow != m.tabOverflows {
		// 左箭头在 strip 之前声明，这一帧才知道装不下：补一帧让它出现。
		m.tabOverflows = overflow
		m.invalidator.Invalidate()
	}
}

// tabArrow 是标签条两端的横向滚动按钮，只在装不下时出现：一次滚三格。
func (m *Model) tabArrow(c ui.C, dir int) {
	st := m.tabStrip
	if st == nil {
		return
	}
	icon, label := IconChevronLeft, "向左滚动标签"
	disabled := st.OffsetX <= 0.5
	if dir > 0 {
		icon, label = IconChevronRight, "向右滚动标签"
		disabled = st.OffsetX+st.ViewportWidth >= st.ContentWidth-0.5
	}
	btn := ui.Button(c, label).Icon(icon).Ghost().IconOnly().Disabled(disabled).Tooltip(label)
	if btn.Clicked() {
		target := st.OffsetX + float32(dir)*tabPitch*3
		if limit := st.ContentWidth - st.ViewportWidth; target > limit {
			target = limit
		}
		if target < 0 {
			target = 0
		}
		st.ScrollToX(target)
	}
}

// tabScrollTarget 算「让第 cur 个标签可见」需要的横向偏移。
//
// 已经可见就原样返回（不把标签硬生生居中）；只在被挡住时挪最小的一步。
// 独立成函数是为了能直接测这段算术，不必驱动窗口。
func tabScrollTarget(cur int, overview bool, offset, viewport float32) float32 {
	if overview || cur < 0 || viewport <= 0 {
		return offset
	}
	left := float32(cur) * tabPitch
	right := left + tabWidth
	switch {
	case left < offset:
		return left
	case right > offset+viewport:
		return right - viewport
	}
	return offset
}

// tabOverflow 是右侧「⋯」：列出全部标签（当前项打勾），再给两个批量关闭。
// 拖不动、又不想按 Shift 横扫时，从这里挑。
func (m *Model) tabOverflow(c ui.C) {
	if len(m.tabs) == 0 {
		return
	}
	button := ui.Button(c, "全部标签").Icon(IconEllipsis).IconOnly().Ghost()
	if button.Clicked() {
		c.After(func() {
			m.tabMenuOpen = !m.tabMenuOpen
		})
	}
	if !m.tabMenuOpen {
		return
	}
	ui.Menu(c, button, &m.tabMenuOpen, func(menu *ui.MenuItems) {
		for index := range m.tabs {
			slot := index
			name := m.tabTitleAt(index)
			if menu.Item(name).Checked(!m.overview && index == m.curTab).Clicked() {
				c.After(func() { m.activateTab(slot) })
			}
		}
		menu.Separator()
		if menu.Item("关闭其他").Disabled(len(m.tabs) <= 1).Clicked() {
			c.After(m.closeOtherTabs)
		}
		if menu.Item("关闭全部").Clicked() {
			c.After(m.closeAllTabs)
		}
	})
}

// pinnedTab 是固定在最左的「集合」页：点它回到概览。
func (m *Model) pinnedTab(c ui.C) {
	theme := c.Theme()
	ui.Column(c).Width(px(68)).Gap(px(0)).Children(func() {
		row := ui.Row(c).Height(px(35)).PadX(px(10)).Gap(px(6)).Items(ui.Center).
			Hover(ui.Look{}.Background(theme.Hover)).
			Hit(ui.ID(c), "标签 集合")
		if m.overview {
			row.Background(theme.Card)
		}
		row.Children(func() {
			ui.Text(c, "集合").FontSize(fs(12.5))
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
	title := m.tabTitleAt(slot)
	ui.Column(c).Gap(px(0)).Children(func() {
		id := ui.ID(c)
		m.setTabSlotID(slot, id)
		// 标签既是拖拽源也是目标：拖着换位置（排序是用户自己排的，与树的排序无关）。
		row := ui.Row(c).Height(px(35)).PadX(px(8)).Gap(px(6)).Items(ui.Center).
			Hover(ui.Look{}.Background(theme.Hover)).
			Hit(id, "标签 "+title).
			DragSource(&m.tabDrag).
			DropTarget(&m.tabDrag, nil, m.dropTab)
		dropping := m.tabDrag.Dragging() && m.tabDrag.Target() == id
		if active {
			row.Background(theme.Card)
		}
		if dropping {
			row.Background(theme.Hover)
		}
		row.Children(func() {
			if tab.entry != nil && tab.entry.Kind == explorer.KindHTTP {
				ui.Text(c, tab.entry.Method).FontSize(fs(9)).Mono().Muted()
			} else if tab.scratch {
				ui.Text(c, tab.req.method).FontSize(fs(9)).Mono().Muted()
			}
			// 备注：文字上按下不会启动拖拽（viva 把文字当无障碍标签，press 归它），
			// 所以拖拽要按在标签的空白处；菜单里另有「左移 / 右移」兜底。
			ui.Text(c, title).FontSize(fs(12.5)).Ellipsis()
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
			if menu.Item("关闭全部").Clicked() {
				c.After(m.closeAll)
			}
			menu.Separator()
			if menu.Item("左移一位").Disabled(slot == 0).Clicked() {
				c.After(func() { m.moveTab(slot, slot-1) })
			}
			if menu.Item("右移一位").Disabled(slot == len(m.tabs)-1).Clicked() {
				c.After(func() { m.moveTab(slot, slot+1) })
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
		if dropping {
			// 拖到哪个标签上，就把那个标签的底边点亮：松手后它会落在这一位。
			line = theme.Primary
		}
		ui.Slot(c).FillWidth().Height(2).Background(line)
	})
}

// detail 是右侧：请求按 Wails 的排版——标题、地址栏、分段页签、响应区（可拖拽分隔）。
func (m *Model) detail(c ui.C, entry *explorer.Entry) {
	if entry == nil {
		ui.Column(c).Fill().Pad(px(24)).Children(func() {
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
		ui.Column(c).Fill().Pad(px(20)).Gap(px(12)).Children(func() {
			ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
				methodBadge(c, entry)
				ui.Text(c, entry.Name).FontSize(fs(17)).Bold()
				ui.Spacer(c)
				if m.grpcDirty {
					ui.Text(c, "未保存").FontSize(fs(11)).Color(c.Theme().Danger)
				}
				save := ui.Button(c, "保存").Outline()
				save.Disabled(!m.grpcDirty)
				if save.Clicked() {
					c.After(m.saveGRPC)
				}
				label, tooltip := "调用", "发送这次 gRPC 调用（Ctrl/Cmd+Enter）"
				if m.grpcSending {
					label = "调用中…"
					tooltip = "正在等响应"
				}
				call := ui.Button(c, label).Tooltip(tooltip)
				if m.grpcSending {
					call.Disabled(true)
				}
				if call.Clicked() {
					c.After(func() { m.sendGRPC(entry) })
				}
			})
			ui.Text(c, m.tables.metaLine).FontSize(fs(11)).Muted().Mono()
			ui.Scroll(c).Grow(1).Children(func() {
				m.grpcTab(c, entry)
			})
		})
		return
	}
	ui.Column(c).Fill().Pad(px(16)).Gap(px(10)).Children(func() {
		m.requestTitle(c, entry)
		m.requestBar(c, entry)
		m.requestResponsePanes(c, entry)
	})
}

// requestResponsePanes 按设置把请求与响应排成上下或左右；两种方向各用一个分隔条状态，
// 这样切换方向后各自记住拖动位置。
func (m *Model) requestResponsePanes(c ui.C, entry *explorer.Entry) {
	if m.settings.SplitSideBySide {
		ui.Row(c).FillWidth().Grow(1).Children(func() {
			// MinWidth(0)：这一列不该锁死 hsplit.Pos。窗口放不下时（缩小窗口、界面放大），
			// 锁死的下限会顺着 Row 往上传递，最后从侧栏身上扣宽度；让分隔条按 span 自己
			// 钳制 Pos 更贴近用户预期。下限由 Splitter 的 Mins(320) 兜底。
			ui.Column(c).Width(m.hsplit.Pos).MinWidth(0).FillHeight().Children(func() {
				m.requestPane(c)
			})
			// 请求 / 响应之间不画竖线：透明手柄自己拖。
			m.splitHandle(c, &m.hsplit, px(320), px(260))
			// MinWidth(0)：响应内容（格式化 JSON 的行）不该反过来决定这一列的宽度，
			// 否则切「原始 / 格式化」时请求列会被挤窄或放宽。
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				m.responsePanel(c, entry)
			})
		})
		return
	}
	ui.Column(c).FillWidth().Height(m.rsplit.Pos).Children(func() {
		m.requestPane(c)
	})
	ui.Splitter(c, &m.rsplit).Mins(px(200), px(120))
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		m.responsePanel(c, entry)
	})
}

// requestPane 是请求编辑区（参数 / 请求体 / 请求头 / 认证 / 文档），两种分栏共用。
// 分段页签放在这一栏**内部**：左右分栏时它只占请求列（与 Wails 版一致），
// 不再横跨整幅、跑到分栏线之上；上下分栏时它本来就在请求区顶部，观感不变。
// 页签固定、内容单独滚动，滚到底部时页签仍可见。
// 整栏是一张和响应区同款的卡片（同内边距 / 圆角 / 描边）—— 两栏观感一致。
func (m *Model) requestPane(c ui.C) {
	theme := c.Theme()
	ui.Column(c).Fill().Pad(panePadding).Gap(px(8)).Radius(theme.Metrics.RadiusSurface).
		Background(theme.Card).Border(1, m.pal.border).Children(func() {
		ui.Tabs(c, &m.tab, httpSegments, tabLabel).Line()
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
				switch m.tab {
				case tabBody:
					m.bodyTab(c)
				case tabHeaders:
					m.kvEditor(c, &m.req.headers, "添加请求头")
				case tabAuth:
					m.authTab(c)
				case tabDocs:
					m.docsTab(c)
				default:
					m.kvEditor(c, &m.req.params, "添加参数")
				}
				if m.req.err != "" {
					ui.Text(c, m.req.err).FontSize(fs(11)).Color(c.Theme().Danger)
				}
				// 外部改动冲突：给两个明确的出路，别让用户猜「保存为什么失败」。
				if m.req.conflict {
					ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
						if ui.Button(c, "重新加载磁盘版本").Outline().Clicked() {
							c.After(m.reloadRequestFromDisk)
						}
						if ui.Button(c, "用我的覆盖").Destructive().Clicked() {
							c.After(m.forceSaveRequest)
						}
					})
				}
			})
		})
	})
}
