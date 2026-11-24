package view

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// paletteCommand 是命令面板的一行：名字、右侧快捷键提示与动作。
type paletteCommand struct {
	name string
	hint string
	run  func(m *Model)
}

// paletteGroup 是命令面板里的一组：请求 / 环境 / 视图 / 集合。
type paletteGroup struct {
	name     string
	commands []paletteCommand
}

// primaryName 是主修饰键在界面上的写法：macOS 是 ⌘，其余平台是 Ctrl。
func primaryName() string {
	if runtime.GOOS == "darwin" {
		return "⌘"
	}
	return "Ctrl"
}

// paletteGroups 是命令面板的静态清单（避免每帧构造）。
// hint 只写**真实存在**的快捷键（Page 里注册的那些），没有就不写，别放空提示。
var paletteGroups = []paletteGroup{
	{"请求", []paletteCommand{
		{name: "新建请求", hint: primaryName() + " N", run: func(m *Model) { m.openNewRequest() }},
		{name: "新建文档", run: func(m *Model) { m.createDoc() }},
		{name: "新建临时请求", run: func(m *Model) { m.openScratch() }},
		{name: "关闭当前标签", hint: primaryName() + " W", run: func(m *Model) { m.closeTab(m.curTab) }},
	}},
	{"集合", []paletteCommand{
		{name: "打开集合", run: func(m *Model) { m.chooseCollection() }},
		{name: "导入 Postman 集合", run: func(m *Model) { m.openImport(0) }},
		{name: "导入 OpenCollection 集合", run: func(m *Model) { m.openImport(1) }},
	}},
	{"环境", []paletteCommand{
		{name: "环境设置", run: func(m *Model) { m.openEnvEditorFor(m.envName) }},
	}},
	{"视图", []paletteCommand{
		{name: "切换主题", run: func(m *Model) { m.setTheme(!m.isLight()) }},
		{name: "设置", hint: primaryName() + " ,", run: func(m *Model) { m.openSettings() }},
		{name: "快捷键", hint: primaryName() + " /", run: func(m *Model) { m.openShortcuts() }},
		{name: "请求历史", run: func(m *Model) { m.openHistory() }},
		{name: "Cookie Jar", run: func(m *Model) { m.openCookies() }},
		{name: "本地 Mock 服务", run: func(m *Model) { m.openMock() }},
		{name: "运行集合", run: func(m *Model) { m.openRunner() }},
		{name: "导出 Markdown", run: func(m *Model) { m.openExport("md") }},
		{name: "导出 HTML", run: func(m *Model) { m.openExport("html") }},
	}},
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
		ui.Column(c).FillWidth().Height(px(340)).Gap(px(6)).Children(func() {
			ui.TextField(c, &m.ui.paletteQuery, "搜索命令", "输入命令名…").FillWidth()
			needle := strings.ToLower(strings.TrimSpace(m.ui.paletteQuery))
			shown := 0
			for _, group := range paletteGroups {
				// 先看这一组还剩几条：整组都被过滤掉就不画组名。
				left := 0
				for _, command := range group.commands {
					if needle == "" || strings.Contains(strings.ToLower(command.name), needle) {
						left++
					}
				}
				if left == 0 {
					continue
				}
				ui.Text(c, group.name).FontSize(fs(10)).Muted().PadX(px(10)).PadY(px(2))
				for _, command := range group.commands {
					if needle != "" && !strings.Contains(strings.ToLower(command.name), needle) {
						continue
					}
					shown++
					cmd := command
					row := ui.Row(c).FillWidth().PadX(px(10)).PadY(px(6)).Gap(px(8)).Items(ui.Center).
						Radius(c.Theme().Metrics.RadiusControl).
						Hover(ui.Look{}.Background(c.Theme().Hover)).
						Hit(ui.ID(c), "命令 "+cmd.name)
					row.Children(func() {
						ui.Text(c, cmd.name).FontSize(fs(13))
						ui.Spacer(c)
						if cmd.hint != "" {
							// 右侧对齐：定宽 + 右对齐，几行提示不会参差。
							ui.Text(c, cmd.hint).FontSize(fs(10)).Mono().Muted().Width(px(56)).TextEnd()
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
			}
			// 输入了词才搜条目：空查询列整个集合会淹掉命令本身。
			if needle != "" && m.col != nil {
				hits := 0
				for _, entry := range m.col.Entries {
					name := strings.ToLower(entry.Name + " " + entry.File)
					if !strings.Contains(name, needle) {
						continue
					}
					if hits == 0 {
						ui.Text(c, "打开").FontSize(fs(10)).Muted().PadX(px(10)).PadY(px(2))
					}
					hits++
					if hits > 8 {
						continue
					}
					entryCopy := entry
					label := entry.Name
					if entry.Kind == explorer.KindHTTP {
						label = entry.Method + "  " + entry.Name
					}
					row := ui.Row(c).FillWidth().PadX(px(10)).PadY(px(6)).Gap(px(8)).Items(ui.Center).
						Radius(c.Theme().Metrics.RadiusControl).
						Hover(ui.Look{}.Background(c.Theme().Hover)).
						Hit(ui.ID(c), "打开 "+entry.Name)
					row.Children(func() {
						ui.Text(c, label).FontSize(fs(13)).Ellipsis()
						ui.Spacer(c)
						ui.Text(c, entryCopy.Group).FontSize(fs(10)).Mono().Muted()
					})
					if row.Clicked() {
						c.After(func() {
							m.ui.paletteOpen = false
							m.openEntry(entryCopy)
							m.invalidator.Invalidate()
						})
					}
				}
				if hits > 8 {
					ui.Text(c, fmt.Sprintf("……还有 %d 条，收窄关键词或去左侧树过滤", hits-8)).
						FontSize(10).Muted().PadX(px(10))
				}
			}
			if shown == 0 && needle != "" && m.col == nil {
				ui.Text(c, "没有匹配的命令").FontSize(fs(11)).Muted()
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
