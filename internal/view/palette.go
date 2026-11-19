package view

import (
	"strings"

	"github.com/vivaui/viva/ui"
)

// paletteCommand 是命令面板的一行：名字、右侧提示与动作。
type paletteCommand struct {
	name string
	hint string
	run  func(m *Model)
}

// paletteCommands 是命令面板的静态清单（避免每帧构造）。
var paletteCommands = []paletteCommand{
	{name: "新建请求", hint: "", run: func(m *Model) { m.openNewRequest() }},
	{name: "新建文档", hint: "", run: func(m *Model) { m.createDoc() }},
	{name: "新建临时请求", hint: "", run: func(m *Model) { m.openScratch() }},
	{name: "打开集合", hint: "", run: func(m *Model) { m.chooseCollection() }},
	{name: "环境设置", hint: "", run: func(m *Model) { m.openEnvEditorFor(m.envName) }},
	{name: "切换主题", hint: "", run: func(m *Model) { m.setTheme(!m.isLight()) }},
	{name: "设置", hint: "", run: func(m *Model) { m.openSettings() }},
	{name: "关闭当前标签", hint: "", run: func(m *Model) { m.closeTab(m.curTab) }},
}

// openPalette 打开命令面板（已打开时切回输入框）。
func (m *Model) openPalette() {
	m.ui.paletteOpen = true
	m.ui.paletteQuery = ""
	m.invalidator.Invalidate()
}

// commandPalette 是居中的命令面板：搜索框 + 过滤后的命令行（参考 14）。
func (m *Model) commandPalette(c ui.C) {
	ui.ModalWith(c, &m.ui.paletteOpen, "命令面板", ui.ModalSpec{Width: 560, Height: 420}, func() {
		ui.Column(c).FillWidth().Height(340).Gap(6).Children(func() {
			ui.TextField(c, &m.ui.paletteQuery, "搜索命令", "输入命令名…").FillWidth()
			needle := strings.ToLower(strings.TrimSpace(m.ui.paletteQuery))
			shown := 0
			for _, command := range paletteCommands {
				if needle != "" && !strings.Contains(strings.ToLower(command.name), needle) {
					continue
				}
				shown++
				cmd := command
				row := ui.Row(c).FillWidth().PadX(10).PadY(6).Gap(8).Items(ui.Center).
					Radius(c.Theme().Metrics.RadiusControl).
					Hover(ui.Look{}.Background(c.Theme().Hover)).
					Hit(ui.ID(c), "命令 "+cmd.name)
				row.Children(func() {
					ui.Text(c, cmd.name).FontSize(13)
					ui.Spacer(c)
					if cmd.hint != "" {
						ui.Text(c, cmd.hint).FontSize(10).Mono().Muted()
					}
				})
				if row.Clicked() {
					c.After(func() {
						m.ui.paletteOpen = false
						cmd.run(m)
						m.invalidator.Invalidate()
					})
				}
			}
			if shown == 0 {
				ui.Text(c, "没有匹配的命令").FontSize(11).Muted()
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if ui.Button(c, "关闭").Clicked() {
				m.ui.paletteOpen = false
			}
		})
	})
}
