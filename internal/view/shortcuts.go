package view

import (
	"github.com/vivaui/viva/ui"
)

// shortcutRow 是一行快捷键说明。primary 为真时前缀补上主修饰键（⌘ / Ctrl）。
type shortcutRow struct {
	keys    string
	primary bool
	what    string
}

// shortcutGroup 是一组快捷键。
type shortcutGroup struct {
	name string
	rows []shortcutRow
}

// shortcutGroups 是快捷键卡的内容。
//
// 只写**真实注册过**的和弦（`ui.Shortcut` 那些），没有的不要写进来——
// 一张骗人的快捷键表比没有更糟。
var shortcutGroups = []shortcutGroup{
	{"全局", []shortcutRow{
		{keys: "K", primary: true, what: "命令面板（也能搜请求与文档）"},
		{keys: "N", primary: true, what: "新建请求"},
		{keys: "W", primary: true, what: "关闭当前标签"},
		{keys: ",", primary: true, what: "设置"},
		{keys: "/", primary: true, what: "这张快捷键卡"},
	}},
	{"编辑与发送", []shortcutRow{
		{keys: "S", primary: true, what: "保存（自动保存关掉时才必须按）"},
		{keys: "Enter", primary: true, what: "发送当前请求"},
		{keys: "Z", primary: true, what: "撤销"},
		{keys: "Shift+Z / Y", primary: true, what: "重做"},
	}},
	{"查找与复制", []shortcutRow{
		{keys: "F", primary: true, what: "在响应正文里查找"},
		{keys: "Esc", what: "关掉查找条"},
		{keys: "C", primary: true, what: "复制正文里选中的文字"},
	}},
	{"鼠标", []shortcutRow{
		{keys: "双击分组", what: "展开 / 折叠"},
		{keys: "拖动标签", what: "换位置（左移 / 右移在右键菜单里）"},
		{keys: "Shift+滚轮", what: "横向滚动页签条"},
		{keys: "拖动分隔条", what: "调分栏大小"},
	}},
}

// openShortcuts 打开快捷键卡。
func (m *Model) openShortcuts() {
	m.ui.shortcutsOpen = true
	m.invalidator.Invalidate()
}

// shortcutsDialog 是 Ctrl+/ 打开的快捷键卡。
func (m *Model) shortcutsDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.shortcutsOpen, "快捷键", ui.ModalSpec{Width: 560, Height: 520}, func() {
		primary := primaryName()
		ui.Column(c).FillWidth().Height(430).Gap(2).Children(func() {
			for _, group := range shortcutGroups {
				ui.Text(c, group.name).FontSize(10).Muted().PadY(5)
				for _, row := range group.rows {
					keys := row.keys
					if row.primary {
						keys = primary + "+" + keys
					}
					keysCopy, what := keys, row.what
					ui.Row(c).FillWidth().PadY(3).Gap(10).Items(ui.Center).Children(func() {
						ui.Text(c, keysCopy).FontSize(11).Mono().Width(130).Muted()
						ui.Text(c, what).FontSize(12).Ellipsis()
					})
				}
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.shortcutsOpen = false
			}
		})
	})
}
