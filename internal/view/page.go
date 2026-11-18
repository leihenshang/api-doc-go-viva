package view

import (
	"strconv"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// Page 声明整窗：工具条 + （集合树 | 分隔条 | 详情）。
// 每帧都从这里重跑：搜索框在本帧写入 m.filter，随后立刻重建列表，避免一帧延迟。
func (m *Model) Page(c ui.C) {
	m.toolbar(c)
	m.refreshShown()
	m.applyPendingSelect()
	current := m.current()
	ui.Row(c).Fill().Children(func() {
		ui.Column(c).Width(m.split.Pos).FillHeight().Background(m.pal.sidebar).Children(func() {
			m.sidebar(c)
		})
		ui.Splitter(c, &m.split).Mins(240, 420)
		ui.Column(c).Grow(1).FillHeight().Background(m.pal.panel).Children(func() {
			m.detail(c, current)
		})
	})
	m.dialogs(c)
}

// toolbar 是顶部一条：集合名、条数·环境、搜索框。
func (m *Model) toolbar(c ui.C) {
	ui.Row(c).FillWidth().Pad(16).Gap(16).Items(ui.Center).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, m.col.Name).FontSize(16).Bold()
			ui.Text(c, m.subtitle).FontSize(11).Muted()
		})
		ui.Spacer(c)
		if ui.Button(c, "打开集合").Clicked() {
			c.After(m.chooseCollection)
		}
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
	ui.Divider(c)
}

// sidebar 是左侧：已打开的集合根、当前集合的搜索、以及条目列表。
func (m *Model) sidebar(c ui.C) {
	ui.Column(c).Fill().Pad(8).Gap(8).Children(func() {
		ui.Text(c, "集合").FontSize(11).Bold().Muted().PadX(6)
		m.rootsPane(c)
		ui.TextField(c, &m.filter, "搜索条目", "搜索当前集合").FillWidth()
		ui.Row(c).FillWidth().PadX(6).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, "内容").FontSize(11).Bold().Muted()
			ui.Spacer(c)
			if ui.Button(c, "新建请求").Icon(IconPlus).IconOnly().Tooltip("新建请求").Clicked() {
				m.openNewRequest(-1)
			}
			if ui.Button(c, "新建文档").Icon(IconFilePlus).IconOnly().Tooltip("新建文档").Clicked() {
				c.After(m.createDoc)
			}
		})
		if m.createErr != "" {
			ui.Text(c, m.createErr).FontSize(11).Color(c.Theme().Danger)
		}
		switch {
		case len(m.col.Entries) == 0:
			emptyBox(c, "集合里没有条目", "点「新建请求 / 新建文档」或在桌面客户端里新建")
		case len(m.shown) == 0:
			emptyBox(c, "没有匹配的条目", "换个关键字，或清空搜索框")
		default:
			ui.List(c, &m.list, len(m.shown), m.row).Label("条目列表").Grow(1)
		}
	})
}

// rootsPane 列出已打开的集合根：点一行切过去，右键可以关掉（至少留一个）。
func (m *Model) rootsPane(c ui.C) {
	theme := c.Theme()
	for index, root := range m.roots {
		slot := index
		row := ui.Row(c).FillWidth().PadX(8).PadY(5).Gap(6).Items(ui.Center).
			Radius(theme.Metrics.RadiusControl).
			Hover(ui.Look{}.Background(theme.Hover)).
			Hit(ui.ID(c), "集合 "+root.col.Name)
		if index == m.active {
			row.Background(theme.Accent)
		}
		row.Children(func() {
			ui.Text(c, root.col.Name).FontSize(12).Ellipsis()
			ui.Spacer(c)
			ui.Text(c, strconv.Itoa(len(root.col.Entries))).FontSize(10).Muted().Mono()
		})
		ui.ContextMenu(c, row, func(menu *ui.MenuItems) {
			item := menu.Item("关闭集合")
			if len(m.roots) <= 1 {
				item.Disabled(true)
			}
			if item.Clicked() {
				c.After(func() { m.closeRoot(slot) })
			}
		})
		if row.Clicked() {
			c.After(func() { m.activateRoot(slot) })
		}
	}
}

// row 是一行：类型徽标（组件目录 Badge）+ 名称 + 分组。
func (m *Model) row(r ui.ListRow) {
	theme := r.C.Theme()
	entry := m.shown[r.Index]
	r.Element.PadY(6).PadX(8).Gap(8).Items(ui.Center).
		Radius(theme.Metrics.RadiusControl).
		Hover(ui.Look{}.Background(theme.Hover))
	if r.Selected() {
		r.Element.Background(theme.Accent)
	}
	methodBadge(r.C, entry)
	ui.Column(r.C).Grow(1).Gap(2).Children(func() {
		ui.Text(r.C, entry.Name).FontSize(13).Ellipsis()
		if entry.Group != "" {
			ui.Text(r.C, entry.Group).FontSize(10).Muted().Ellipsis()
		}
	})
	// 右键菜单：集合树的常用操作（与客户端同语义，重命名不改文件名）。
	ui.ContextMenu(r.C, r.Element, func(menu *ui.MenuItems) {
		if menu.Item("新建请求").Icon(IconPlus).Clicked() {
			m.openNewRequest(r.Index)
		}
		if menu.Item("新建文档").Icon(IconFilePlus).Clicked() {
			r.C.After(m.createDoc)
		}
		if menu.Item("重命名").Clicked() {
			m.openRename(r.Index)
		}
		if menu.Item("删除到回收站").Icon(IconTrash).Destructive().Clicked() {
			file := m.shown[r.Index].File
			r.C.After(func() { m.trashEntry(file) })
		}
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
