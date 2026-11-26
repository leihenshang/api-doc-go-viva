package view

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// statusBarHeight 是底栏高度（与 Wails 的 --app-statusbar-h 同量级）。
const statusBarHeight = 28

// statusBar 是底部状态栏，排版对齐 Wails 客户端：
// 左侧只读信息（离线优先 · 纯文本存储），中间弹性留白，
// 右侧是全局入口（回收站 / MCP / 设置）与集合规模计数。
func (m *Model) statusBar(c ui.C) {
	ui.Divider(c)
	ui.Row(c).FillWidth().Height(statusBarHeight).PadX(px(12)).Gap(px(10)).Items(ui.Center).
		Background(m.pal.bg).Children(func() {
		// 只读视图的行号：正文不挪原点（动了选区的坐标换算就全偏），
		// 光标所在行列与总行数放在底栏，滚到哪儿都能看见自己在第几行。
		if info := m.lineStatus(); info != "" {
			ui.Text(c, info).FontSize(fs(11)).Muted().Mono().Shrink(0)
		}
		ui.Spacer(c)
		if m.ui.exportNote != "" {
			ui.Text(c, m.ui.exportNote).FontSize(fs(11)).Muted()
		}
		ui.Spacer(c)
		m.statusActions(c)
		ui.Text(c, m.statusCounts()).FontSize(fs(11)).Muted().Mono()
	})
}

// statusActions 是底栏右侧的入口组：回收站、MCP（带运行状态点）、设置。
func (m *Model) statusActions(c ui.C) {
	hasCollection := m.hasCollection()
	trash := ui.Button(c, "回收站").Icon(IconTrash).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip("查看回收站")
	if !hasCollection {
		trash.Disabled(true)
	}
	if trash.Clicked() {
		m.openTrash()
	}

	mockLabel, mockHint := m.mockLabel()
	mockButton := ui.Button(c, "Mock").Icon(IconServer).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip(mockHint)
	if !hasCollection {
		mockButton.Disabled(true)
	}
	if mockButton.Clicked() {
		m.openMock()
	}
	mockDot := m.pal.border
	if m.mock.running {
		mockDot = c.Theme().Success
	} else if m.mock.err != "" {
		mockDot = c.Theme().Danger
	}
	ui.Slot(c).Width(px(6)).Height(px(6)).Radius(px(3)).Background(mockDot).Hit(ui.ID(c), mockLabel)

	if ui.Button(c, "历史").Icon(IconClock).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip("查看请求历史").Clicked() {
		m.openHistory()
	}
	// Cookie 只跟「发过请求」有关，不依赖集合是否打开。
	cookieHint := "查看 Cookie Jar（发送请求时自动带上）"
	if n := len(send.Cookies()); n > 0 {
		cookieHint = fmt.Sprintf("Cookie Jar 里有 %d 条", n)
	}
	if ui.Button(c, "Cookie").Icon(IconCookie).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip(cookieHint).Clicked() {
		m.openCookies()
	}

	label, hint := m.mcpLabel()
	mcp := ui.Button(c, "MCP").Icon(IconCode).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip(hint)
	if !hasCollection {
		mcp.Disabled(true)
	}
	if mcp.Clicked() {
		m.openMCPSettings()
	}
	dot := m.pal.border
	if m.mcp.running {
		dot = c.Theme().Success
	} else if m.mcp.err != "" {
		dot = c.Theme().Danger
	}
	ui.Slot(c).Width(px(6)).Height(px(6)).Radius(px(3)).Background(dot).Hit(ui.ID(c), label)

	if ui.Button(c, "设置").Icon(IconGear).Ghost().XSmall().Weight(gfx.WeightRegular).Tooltip("打开设置").Clicked() {
		m.openSettings()
	}
}

// mcpLabel 是 MCP 入口的状态文案与提示（带监听地址或错误原因）。
func (m *Model) mcpLabel() (label, hint string) {
	switch {
	case m.mcp.running:
		return "MCP 运行中", "MCP 服务运行中：" + m.mcp.addr
	case m.mcp.err != "":
		return "MCP 出错", "MCP 服务启动失败：" + m.mcp.err
	default:
		return "MCP 未启用", "内嵌 MCP 服务未启用（在设置里打开）"
	}
}

// statusCounts 是集合规模文案：请求 / 文档 / 环境三项，与集合树一致。
func (m *Model) statusCounts() string {
	if !m.hasCollection() {
		return "未打开集合"
	}
	requests, docs := 0, 0
	for _, entry := range m.col.Entries {
		if entry.IsDoc() {
			docs++
			continue
		}
		requests++
	}
	text := fmt.Sprintf("%d 个请求 · %d 个环境", requests, len(m.ui.envs))
	if docs > 0 {
		text = fmt.Sprintf("%d 个请求 · %d 篇文档 · %d 个环境", requests, docs, len(m.ui.envs))
	}
	return text
}

// lineStatus 是底栏左侧那句「行 12 列 5 · 共 340 行」。
//
// 口径（与 N22 的约定一致）：只标逻辑行，折行不单独编号——
// 原始报文里被切开的续行记回上一行的号；JSON 视图用原始 JSON 的行号，
// 折叠隐藏的行不参与计数，但总数仍是整篇的行数。
func (m *Model) lineStatus() string {
	entry := m.activeEntry()
	if entry == nil {
		return ""
	}
	switch entry.Kind {
	case explorer.KindDoc:
		if m.tab != tabPreview {
			return ""
		}
		return "文档 " + strconv.Itoa(lineCount(m.editor.text)) + " 行"
	case explorer.KindHTTP:
		if m.tab == tabDocs && !m.docsEditing {
			return "文档 " + strconv.Itoa(lineCount(m.req.docs)) + " 行"
		}
		row, total, ok := m.responseLine()
		if !ok {
			return ""
		}
		info := "行 " + strconv.Itoa(row) + " 列 " + strconv.Itoa(m.jsonSel.head.col+1) +
			" · 共 " + strconv.Itoa(total) + " 行"
		if sel := m.jsonSel; sel.has {
			span := sel.head.line - sel.anchor.line
			if span < 0 {
				span = -span
			}
			info += " · 选中 " + strconv.Itoa(span+1) + " 行"
		}
		return info
	default:
		return ""
	}
}

// lineCount 是一段文本的逻辑行数（空文本算 1 行，与编辑器一致）。
func lineCount(text string) int {
	return strings.Count(text, "\n") + 1
}
