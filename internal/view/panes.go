package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// 表头与列宽是包级常量：每帧声明 Table 时复用，不再逐帧拼切片。
var (
	kvHeaders    = []string{"名称", "值", "说明"}
	kvColumns    = []components.TableColumn{{Width: 200}, {}, {Width: 200}}
	fieldHeaders = []string{"字段", "类型", "必填", "说明"}
	fieldColumns = []components.TableColumn{{Width: 180}, {Width: 90}, {Width: 60}, {}}
)

// docDetail 是一篇 Markdown 的编辑 / 预览。
func (m *Model) docDetail(c ui.C, entry *explorer.Entry) {
	if ui.Shortcut(c, ui.Primary, "s") {
		c.After(m.saveDoc)
	}
	dirty := m.docDirty()
	ui.Column(c).Fill().Pad(20).Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
			methodBadge(c, entry)
			ui.Text(c, entry.Name).FontSize(18).Bold()
			ui.Spacer(c)
			switch {
			case dirty:
				ui.Text(c, "未保存").FontSize(11).Color(c.Theme().Danger)
			case m.editor.status != "":
				ui.Text(c, m.editor.status).FontSize(11).Muted()
			}
			save := ui.Button(c, "保存")
			if !dirty {
				save.Disabled(true)
			}
			if save.Clicked() {
				c.After(m.saveDoc)
			}
		})
		ui.Text(c, m.editor.metaLine).FontSize(11).Muted().Mono()
		ui.Tabs(c, &m.tab, docTabs, tabLabel)
		if m.tab == tabEdit {
			ui.TextArea(c, &m.editor.text, "文档内容", "在此编写 Markdown…").FillWidth().Grow(1)
			return
		}
		if strings.TrimSpace(m.editor.text) == "" {
			emptyBox(c, "空文档", "在「编辑」页签里写内容，这里会实时预览")
			return
		}
		ui.Scroll(c).Grow(1).Children(func() {
			m.previewBlock(c, m.editor.text, m.editor.previewKey)
		})
	})
}

// requestTab 展示一条 http 请求：地址、启用的参数 / 头、认证、请求体字段与正文。
func (m *Model) requestTab(c ui.C, entry *explorer.Entry) {
	if entry.Kind != explorer.KindHTTP {
		m.grpcTab(c, entry)
		return
	}
	block := entry.Payload.HTTP
	m.urlCard(c, block.URL)
	m.tableSection(c, 0, "查询参数", &m.tables.params, kvHeaders, kvColumns)
	m.tableSection(c, 1, "请求头", &m.tables.headers, kvHeaders, kvColumns)
	if m.tables.authLine != "" {
		ui.Text(c, m.tables.authLine).FontSize(11).Muted()
	}
	m.tableSection(c, 2, "请求体字段", &m.tables.fields, fieldHeaders, fieldColumns)
	if m.tables.bodyText != "" {
		m.codeCard(c, m.tables.bodyText)
	}
}

// tableSection 有行才画：空表头比不画更吵。
func (m *Model) tableSection(c ui.C, slot int, title string, rows *ui.TableRows, headers []string, columns []components.TableColumn) {
	if rows == nil || len(*rows) == 0 {
		return
	}
	ui.Text(c, title).FontSize(12).Bold().Muted()
	table(c, &m.tableStates[slot], rows, headers, columns)
}

// grpcTab 只读展示 gRPC 元数据：调用不在这次框架验证的范围。
func (m *Model) grpcTab(c ui.C, entry *explorer.Entry) {
	if entry.Payload.GRPC == nil {
		ui.Text(c, "缺少 grpc 段").Muted()
		return
	}
	ui.Column(c).FillWidth().Pad(14).Gap(6).Draw(cardDraw).Children(func() {
		ui.Text(c, m.tables.grpcEndpoint).Mono().FontSize(12)
		if m.tables.grpcMeta != "" {
			ui.Text(c, m.tables.grpcMeta).FontSize(11).Muted()
		}
		if m.tables.grpcMessage != "" {
			ui.Text(c, "请求消息").FontSize(11).Muted()
			ui.Paragraph(c, m.tables.grpcMessage, 0).Mono().FontSize(11)
		}
	})
	ui.Text(c, "验证版只读展示 gRPC 元数据；发送能力未接。").FontSize(11).Muted()
}

// responseTab 是响应区：gRPC 只读 / 发送中 / 尚未发送 / 失败 / 结果（三张数字卡 + 响应体卡片）。
func (m *Model) responseTab(c ui.C, entry *explorer.Entry) {
	switch {
	case entry.Kind != explorer.KindHTTP:
		emptyBox(c, "gRPC 没有响应", "验证版只读展示 gRPC 元数据，不发起调用")
	case m.sending:
		ui.Row(c).Gap(8).Items(ui.Center).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "请求中…").Muted()
		})
	case !m.hasResp:
		emptyBox(c, "尚未发送", "点右上角「发送」运行这条请求")
	case m.failText != "":
		ui.Column(c).FillWidth().Pad(14).Gap(6).Draw(cardDraw).Children(func() {
			ui.Text(c, "请求失败").FontSize(13).Bold().Color(c.Theme().Danger)
			ui.Text(c, m.failText).Mono().FontSize(11).Muted()
		})
	default:
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			metric(c, "状态", m.status, m.reason, m.statusOK)
			metric(c, "耗时", m.elapsed, "单次请求", true)
			metric(c, "体积", m.size, "响应体", true)
		})
		if m.missing != "" {
			ui.Text(c, m.missing).FontSize(11).Color(c.Theme().Danger)
		}
		if m.bodyText == "" {
			ui.Text(c, "响应体为空").Muted()
			return
		}
		m.codeCard(c, m.bodyText)
	}
}

// docsTab 展示请求的概述与 Docs；Docs 交给 markdown + MarkdownView 排版。
func (m *Model) docsTab(c ui.C, entry *explorer.Entry) {
	summary := entry.Summary
	if summary == "" {
		summary = "（没有概述）"
	}
	ui.Column(c).FillWidth().Pad(14).Gap(4).Draw(cardDraw).Children(func() {
		ui.Text(c, "概述").FontSize(11).Muted()
		ui.Paragraph(c, summary, 0).FontSize(13)
	})
	if strings.TrimSpace(entry.Payload.Docs) == "" {
		ui.Text(c, "（没有详细说明）").Muted()
		return
	}
	m.previewBlock(c, entry.Payload.Docs, m.tables.docsKey)
	ui.Text(c, "Markdown 已按文档排版；链接点击与文档图片尚未接。").FontSize(11).Muted()
}

// previewBlock 把一段 Markdown 排好版画进当前布局（不加滚动容器，由调用方决定）。
func (m *Model) previewBlock(c ui.C, source, key string) {
	height := m.previewHeight(c, key, source, m.contentWidth(c))
	ui.Slot(c).FillWidth().Height(height).Draw(func(d *ui.DrawContext) {
		m.docs.view.Paint(d.Canvas, components.MarkdownPaint{
			Look: m.docs.look,
			X:    d.Rect.X,
			Y:    d.Rect.Y,
		})
	})
}

// urlCard 是地址卡片：组件目录的卡片底 + 等宽 URL。
func (m *Model) urlCard(c ui.C, url string) {
	ui.Column(c).FillWidth().Pad(12).Gap(4).Draw(cardDraw).Children(func() {
		ui.Text(c, "地址").FontSize(11).Muted()
		ui.Text(c, url).Mono().FontSize(12)
	})
}

// codeCard 是等宽长文卡片：请求体原文与响应体共用。
func (m *Model) codeCard(c ui.C, text string) {
	ui.Column(c).FillWidth().Pad(12).Draw(cardDraw).Children(func() {
		ui.Paragraph(c, text, 0).Mono().FontSize(11)
	})
}
