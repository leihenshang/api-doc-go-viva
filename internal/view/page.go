package view

import (
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
		ui.Column(c).Width(m.split.Pos).FillHeight().Background(c.Theme().Card).Children(func() {
			m.sidebar(c)
		})
		ui.Splitter(c, &m.split).Mins(240, 420)
		ui.Column(c).Grow(1).FillHeight().Children(func() {
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
		ui.TextField(c, &m.filter, "搜索条目", "按名称 / URL / 分组过滤").Width(280)
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

// sidebar 是左侧集合树：新建入口 + 条目列表；空集合与无匹配都走组件目录的空状态。
func (m *Model) sidebar(c ui.C) {
	ui.Column(c).Fill().Pad(10).Gap(8).Children(func() {
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
			emptyBox(c, "集合里没有条目", "点「新建文档」或在桌面客户端里新建请求")
		case len(m.shown) == 0:
			emptyBox(c, "没有匹配的条目", "换个关键字，或清空搜索框")
		default:
			ui.List(c, &m.list, len(m.shown), m.row).Label("条目列表").Grow(1)
		}
	})
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

// detail 是右侧：文档走编辑 / 预览，请求走请求 / 响应 / 文档。
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
	ui.Column(c).Fill().Pad(20).Gap(12).Children(func() {
		m.detailHeader(c, entry)
		ui.Tabs(c, &m.tab, httpTabs, tabLabel)
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				switch m.tab {
				case tabRequest:
					m.requestTab(c, entry)
				case tabResponse:
					m.responseTab(c, entry)
				default:
					m.docsTab(c, entry)
				}
			})
		})
	})
}

// detailHeader 是请求的标题行：方法徽标、名称、路径与发送按钮。
func (m *Model) detailHeader(c ui.C, entry *explorer.Entry) {
	ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
		methodBadge(c, entry)
		ui.Text(c, entry.Name).FontSize(18).Bold()
		ui.Spacer(c)
		if entry.Kind == explorer.KindHTTP {
			if m.req.dirty {
				ui.Text(c, "未保存").FontSize(11).Color(c.Theme().Danger)
				if ui.Button(c, "保存请求").Icon(IconFilePlus).Clicked() {
					c.After(m.saveRequest)
				}
			}
			m.sendButton(c, entry)
		} else {
			ui.Text(c, "gRPC 只读").FontSize(11).Muted()
		}
	})
	if entry.Summary != "" {
		ui.Text(c, entry.Summary).FontSize(13).Muted()
	}
	ui.Text(c, m.tables.metaLine).FontSize(11).Muted().Mono()
}

// sendButton 是唯一的写操作入口；发送中禁用，避免并发覆盖响应。
func (m *Model) sendButton(c ui.C, entry *explorer.Entry) {
	if m.sending {
		ui.Button(c, "发送中…").Disabled(true)
		return
	}
	if ui.Button(c, "发送").Icon(IconSend).Clicked() {
		m.start(entry)
	}
}
