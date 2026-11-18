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

// requestTab 是请求编辑器：方法/地址、参数、请求头、请求体、认证都可改，保存写回文件。
func (m *Model) requestTab(c ui.C, entry *explorer.Entry) {
	if entry.Kind != explorer.KindHTTP {
		m.grpcTab(c, entry)
		return
	}
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		if ui.Select(c, &m.req.method, httpMethods, nil).Width(120).Changed() {
			m.req.dirty = true
		}
		if ui.TextField(c, &m.req.url, "地址", "{{host}}/api/users").Grow(1).Changed() {
			m.req.dirty = true
		}
	})
	m.kvEditor(c, "查询参数", &m.req.params, "添加参数")
	m.kvEditor(c, "请求头", &m.req.headers, "添加请求头")
	m.bodyEditor(c)
	m.authEditor(c)
	if m.req.err != "" {
		ui.Text(c, m.req.err).FontSize(11).Color(c.Theme().Danger)
	}
	// 请求体字段表格（body.fields）目前只读：它是文档生成的真源，编辑入口留到下一轮。
	m.tableSection(c, 2, "请求体字段（只读）", &m.tables.fields, fieldHeaders, fieldColumns)
}

// kvEditor 是可编辑的键值区：每行一个启用勾选 + 名称/值/说明 + 删除。
func (m *Model) kvEditor(c ui.C, title string, rows *[]*kvRow, addLabel string) {
	ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(12).Bold().Muted()
		ui.Spacer(c)
		if ui.Button(c, addLabel).Icon(IconPlus).Clicked() {
			*rows = append(*rows, &kvRow{enabled: true})
			m.req.dirty = true
		}
	})
	if len(*rows) == 0 {
		ui.Text(c, "（没有）").FontSize(11).Muted()
		return
	}
	for index, row := range *rows {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.name, "名称", "name").Width(150).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.value, "值", "value").Grow(1).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.desc, "说明", "").Width(160).Changed() {
				m.req.dirty = true
			}
			if ui.Button(c, "删除").Icon(IconTrash).IconOnly().Clicked() {
				c.After(func() {
					*rows = append((*rows)[:slot], (*rows)[slot+1:]...)
					m.req.dirty = true
				})
			}
		})
	}
}

// bodyEditor 编辑请求体类型与原文；表单/多文件逐行编辑暂未支持，但原数据会保留。
func (m *Model) bodyEditor(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "请求体").FontSize(12).Bold().Muted()
		ui.Spacer(c)
		if ui.Select(c, &m.req.bodyType, bodyTypes, nil).Width(130).Changed() {
			m.req.dirty = true
		}
	})
	switch m.req.bodyType {
	case "", "none":
		ui.Text(c, "无请求体（`''` 与 `none` 同义）").FontSize(11).Muted()
	case "form", "multipart":
		ui.Text(c, "表单 / 多文件的逐行编辑暂未支持；保存时保留文件里原有的 form 数据。").FontSize(11).Muted()
	default:
		if ui.TextArea(c, &m.req.bodyRaw, "请求体", `{"name": "value"}`).FillWidth().Height(170).Changed() {
			m.req.dirty = true
		}
	}
}

// authEditor 编辑认证；none 表示文件里不写 auth 段，inherit 表示继承集合默认。
func (m *Model) authEditor(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "认证").FontSize(12).Bold().Muted()
		ui.Spacer(c)
		if ui.Select(c, &m.req.authType, authTypes, nil).Width(130).Changed() {
			m.req.dirty = true
		}
	})
	switch m.req.authType {
	case "", "none":
		ui.Text(c, "不发送认证信息").FontSize(11).Muted()
	case "inherit":
		ui.Text(c, "沿用集合清单 defaults.auth（值支持 {{变量}}）").FontSize(11).Muted()
	case "basic":
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if ui.TextField(c, &m.req.authUser, "用户名", "{{username}}").Grow(1).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &m.req.authPass, "口令", "{{password}}").Grow(1).Secure().Changed() {
				m.req.dirty = true
			}
		})
	case "bearer":
		if ui.TextField(c, &m.req.authToken, "Token", "{{token}}").Grow(1).Changed() {
			m.req.dirty = true
		}
	case "apikey":
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.TextField(c, &m.req.authKey, "Key", "X-API-Key").Grow(1).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &m.req.authValue, "Value", "{{apiKey}}").Grow(1).Changed() {
				m.req.dirty = true
			}
			if ui.Select(c, &m.req.authIn, authPlaces, nil).Width(110).Changed() {
				m.req.dirty = true
			}
		})
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

// docsTab 编辑概述与详细说明，下面实时预览 Markdown 排版结果。
func (m *Model) docsTab(c ui.C, entry *explorer.Entry) {
	ui.Text(c, "概述").FontSize(12).Bold().Muted()
	if ui.TextField(c, &m.req.summary, "概述", "一句话说明这个接口").FillWidth().Changed() {
		m.req.dirty = true
	}
	ui.Text(c, "详细说明（Markdown）").FontSize(12).Bold().Muted()
	if ui.TextArea(c, &m.req.docs, "详细说明", "支持标题、列表、表格与代码块").FillWidth().Height(200).Changed() {
		m.req.dirty = true
	}
	ui.Text(c, "预览").FontSize(12).Bold().Muted()
	if strings.TrimSpace(m.req.docs) == "" {
		ui.Text(c, "（没有详细说明）").Muted()
		return
	}
	m.previewBlock(c, m.req.docs, m.tables.docsKey)
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
