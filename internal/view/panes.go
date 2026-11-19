package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// requestTitle 是请求标题行：方法角标、名称、概述与脏状态。
func (m *Model) requestTitle(c ui.C, entry *explorer.Entry) {
	ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
		methodBadge(c, entry)
		ui.Text(c, entry.Name).FontSize(17).Bold()
		if entry.Summary != "" {
			ui.Text(c, entry.Summary).FontSize(12).Muted().Ellipsis()
		}
		ui.Spacer(c)
		if m.req.note != "" {
			ui.Text(c, m.req.note).FontSize(11).Muted()
		}
		if ui.Button(c, "复制 cURL").Icon(IconCopy).Ghost().Small().Clicked() {
			c.After(func() { m.copyCurl(entry) })
		}
		if m.req.dirty {
			ui.Text(c, "未保存").FontSize(11).Color(c.Theme().Danger)
		}
	})
	ui.Text(c, m.tables.metaLine).FontSize(11).Muted().Mono()
}

// requestBar 是地址栏：方法 + 地址，紧跟「保存请求」「发送」两个动作，
// 与 Wails 的 RequestBar 同一排、同一顺序（URL 后面才是动作，不放到顶部工具条）。
func (m *Model) requestBar(c ui.C, entry *explorer.Entry) {
	if ui.Shortcut(c, ui.Primary, "s") {
		c.After(m.saveRequest)
	}
	if ui.Shortcut(c, ui.Primary, "enter") && !m.sending {
		m.start(entry)
	}
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		if ui.Select(c, &m.req.method, httpMethods, nil).Width(120).Changed() {
			m.req.dirty = true
		}
		if ui.TextField(c, &m.req.url, "地址", "{{host}}/api/users").Grow(1).Changed() {
			m.req.dirty = true
		}
		saveLabel := "保存请求"
		if m.scratch {
			saveLabel = "另存为"
		}
		save := ui.Button(c, saveLabel).Secondary().Tooltip("保存请求（Ctrl/Cmd+S）")
		if !m.req.dirty && !m.scratch {
			save.Disabled(true)
		}
		if save.Clicked() {
			c.After(m.saveRequest)
		}
		if m.sending {
			ui.Button(c, "发送中…").Disabled(true)
		} else if ui.Button(c, "发送").Icon(IconSend).Tooltip("发送（Ctrl/Cmd+Enter）").Clicked() {
			m.start(entry)
		}
	})
}

// kvEditor 是表格式键值编辑：表头 + 每行（启用 / 名称 / 值 / 说明 / 删除）+ 底部添加。
func (m *Model) kvEditor(c ui.C, rows *[]*kvRow, addLabel string) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Slot(c).Width(24)
		ui.Text(c, "名称").FontSize(11).Muted().Width(150)
		ui.Text(c, "值").FontSize(11).Muted().Grow(1)
		ui.Text(c, "说明").FontSize(11).Muted().Width(160)
		ui.Slot(c).Width(32)
	})
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
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, addLabel).Icon(IconPlus).Ghost().Clicked() {
			*rows = append(*rows, &kvRow{enabled: true})
			m.req.dirty = true
		}
	})
}

// respTabs / respTabLabel 是响应区的两个分段：正文与响应头。
var (
	respTabs  = []int{0, 1}
	formTypes = []string{"text", "file"}
)

func respTabLabel(index int) string {
	if index == 1 {
		return "响应头"
	}
	return "正文"
}

// bodyTab 编辑请求体：类型 + 原文（json/text）或表单行，并只读展示 body.fields。
func (m *Model) bodyTab(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "类型").FontSize(11).Muted()
		if ui.Select(c, &m.req.bodyType, bodyTypes, nil).Width(140).Changed() {
			m.req.dirty = true
		}
		ui.Spacer(c)
	})
	switch m.req.bodyType {
	case "", "none":
		ui.Text(c, "无请求体（空字符串与 `none` 同义）。").FontSize(11).Muted()
	case "form", "multipart":
		m.formEditor(c, m.req.bodyType == "multipart")
	default:
		if ui.TextArea(c, &m.req.bodyRaw, "请求体", `{"name": "value"}`).FillWidth().Height(240).Changed() {
			m.req.dirty = true
		}
	}
	m.fieldEditor(c)
}

// fieldEditor 编辑请求体字段表（body.fields）：字段名 / 类型 / 必填 / 示例 / 说明。
func (m *Model) fieldEditor(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "字段名").FontSize(11).Muted().Width(150)
		ui.Text(c, "类型").FontSize(11).Muted().Width(100)
		ui.Text(c, "必填").FontSize(11).Muted().Width(40)
		ui.Text(c, "示例").FontSize(11).Muted().Width(160)
		ui.Text(c, "说明").FontSize(11).Muted().Grow(1)
		ui.Slot(c).Width(32)
	})
	for index, row := range m.req.fields {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.TextField(c, &row.name, "字段名", "id").Width(150).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.typ, "类型", "string").Width(100).Changed() {
				m.req.dirty = true
			}
			if ui.Checkbox(c, &row.required, "").Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.example, "示例", "abc").Width(160).Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.desc, "说明", "").Grow(1).Changed() {
				m.req.dirty = true
			}
			if ui.Button(c, "删除字段").Icon(IconTrash).IconOnly().Clicked() {
				c.After(func() {
					m.req.fields = append(m.req.fields[:slot], m.req.fields[slot+1:]...)
					m.req.dirty = true
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加字段").Icon(IconPlus).Ghost().Clicked() {
			m.req.fields = append(m.req.fields, &fieldRow{typ: "string"})
			m.req.dirty = true
		}
	})
}

// formEditor 编辑 form / multipart 的逐行字段；multipart 多一列「类型」（text 或 file）。
func (m *Model) formEditor(c ui.C, multipart bool) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Slot(c).Width(24)
		ui.Text(c, "名称").FontSize(11).Muted().Width(150)
		ui.Text(c, "值").FontSize(11).Muted().Grow(1)
		if multipart {
			ui.Text(c, "类型").FontSize(11).Muted().Width(96)
		}
		ui.Text(c, "说明").FontSize(11).Muted().Width(140)
		ui.Slot(c).Width(32)
	})
	for index, row := range m.req.form {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.req.dirty = true
			}
			if ui.TextField(c, &row.name, "名称", "field").Width(150).Changed() {
				m.req.dirty = true
			}
			placeholder := "value"
			if multipart && row.typ == "file" {
				placeholder = "相对集合根的文件路径"
			}
			if ui.TextField(c, &row.value, "值", placeholder).Grow(1).Changed() {
				m.req.dirty = true
			}
			if multipart {
				if ui.Select(c, &row.typ, formTypes, nil).Width(96).Changed() {
					m.req.dirty = true
				}
			}
			if ui.TextField(c, &row.desc, "说明", "").Width(140).Changed() {
				m.req.dirty = true
			}
			if ui.Button(c, "删除").Icon(IconTrash).IconOnly().Clicked() {
				c.After(func() {
					m.req.form = append(m.req.form[:slot], m.req.form[slot+1:]...)
					m.req.dirty = true
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加表单字段").Icon(IconPlus).Ghost().Clicked() {
			m.req.form = append(m.req.form, &kvRow{enabled: true, typ: "text"})
			m.req.dirty = true
		}
	})
}

// authTab 编辑认证；none 不写 auth 段，inherit 表示继承集合默认。
func (m *Model) authTab(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "类型").FontSize(11).Muted()
		if ui.Select(c, &m.req.authType, authTypes, nil).Width(140).Changed() {
			m.req.dirty = true
		}
		ui.Spacer(c)
	})
	switch m.req.authType {
	case "", "none":
		ui.Text(c, "不发送认证信息。").FontSize(11).Muted()
	case "inherit":
		ui.Text(c, "沿用集合清单 defaults.auth（值支持 {{变量}}）。").FontSize(11).Muted()
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

// docsTab 是接口文档：左边写概述与 Markdown 原文，右边实时预览（参考 10）。
func (m *Model) docsTab(c ui.C, entry *explorer.Entry) {
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c).Grow(1).Gap(8).Children(func() {
			ui.Text(c, "概述").FontSize(11).Muted()
			if ui.TextField(c, &m.req.summary, "概述", "一句话说明这个接口").FillWidth().Changed() {
				m.req.dirty = true
			}
			ui.Text(c, "详细说明（Markdown）").FontSize(11).Muted()
			if ui.TextArea(c, &m.req.docs, "详细说明", "支持标题、列表、表格与代码块").
				FillWidth().Height(240).Changed() {
				m.req.dirty = true
			}
		})
		ui.Divider(c)
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, "预览").FontSize(11).Muted()
			if strings.TrimSpace(m.req.docs) == "" {
				ui.Text(c, "（没有详细说明）").Muted()
				return
			}
			m.previewBlock(c, m.req.docs, m.tables.docsKey, m.contentWidth(c)/2-20)
		})
	})
}

// responsePanel 是请求页下方的响应区：状态行固定，正文滚动。
func (m *Model) responsePanel(c ui.C, entry *explorer.Entry) {
	theme := c.Theme()
	ui.Column(c).Fill().Pad(12).Gap(8).Radius(theme.Metrics.RadiusSurface).
		Background(theme.Card).Border(1, m.pal.border).Children(func() {
		ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
			ui.Text(c, "响应").FontSize(12).Bold()
			if entry.Kind == explorer.KindHTTP {
				ui.Tabs(c, &m.respTab, respTabs, respTabLabel).Line()
			}
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "gRPC 只读").FontSize(11).Muted()
			case m.sending:
				ui.Spinner(c)
			case m.hasResp && m.failText == "":
				statusTint := theme.Success
				if !m.statusOK {
					statusTint = theme.Danger
				}
				ui.Text(c, m.status).FontSize(12).Bold().Color(statusTint)
				ui.Text(c, m.reason).FontSize(11).Muted()
				ui.Spacer(c)
				if m.copyNote != "" {
					ui.Text(c, m.copyNote).FontSize(11).Muted()
				}
				if ui.Button(c, "复制正文").Icon(IconCopy).Ghost().Tooltip("复制响应正文").Clicked() {
					c.After(m.copyResponse)
				}
				ui.Text(c, m.elapsed+" · "+m.size).FontSize(11).Muted().Mono()
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "验证版只读展示 gRPC 元数据，不发起调用。").FontSize(11).Muted()
			case m.sending:
				ui.Text(c, "请求中…").Muted()
			case !m.hasResp:
				ui.Text(c, "尚未发送：点地址栏右侧「发送」运行这条请求。").FontSize(12).Muted()
			case m.failText != "":
				ui.Text(c, "请求失败："+m.failText).FontSize(11).Color(theme.Danger)
			case m.respTab == 1:
				m.responseHeaders(c)
			default:
				if m.missing != "" {
					ui.Text(c, m.missing).FontSize(11).Color(theme.Danger)
				}
				if m.bodyText == "" {
					ui.Text(c, "响应体为空").Muted()
					return
				}
				if len(m.tables.json) > 0 {
					m.jsonBody(c)
					return
				}
				ui.Paragraph(c, m.bodyText, 0).Mono().FontSize(11)
			}
		})
	})
}

// toggleJSONFold 折叠 / 展开响应 JSON 的某一行。
func (m *Model) toggleJSONFold(line int) {
	if m.tables.jsonFolded == nil {
		m.tables.jsonFolded = make(map[int]bool)
	}
	m.tables.jsonFolded[line] = !m.tables.jsonFolded[line]
	m.storeTab()
	m.invalidator.Invalidate()
}

// copyResponse 把响应正文写进系统剪贴板；失败时留一句提示。
func (m *Model) copyResponse() {
	if err := app.SetClipboardText(m.resp.Body); err != nil {
		m.copyNote = "复制失败：" + err.Error()
		return
	}
	m.copyNote = "已复制"
	m.storeTab()
	m.invalidator.Invalidate()
}

// jsonBody 画响应 JSON：可折叠行带箭头，折叠后直接跳到收尾行。
func (m *Model) jsonBody(c ui.C) {
	hiddenUntil := -1
	for index, line := range m.tables.json {
		if index < hiddenUntil {
			continue
		}
		end, foldable := m.tables.jsonFolds[index]
		folded := foldable && m.tables.jsonFolded[index]
		lineIndex := index
		ui.Row(c).Gap(2).Items(ui.Center).Children(func() {
			if foldable {
				arrow := "▾"
				if folded {
					arrow = "▸"
				}
				if ui.Button(c, arrow).Ghost().XSmall().Clicked() {
					c.After(func() { m.toggleJSONFold(lineIndex) })
				}
			} else {
				ui.Slot(c).Width(18)
			}
			for _, span := range line {
				ui.Text(c, span.text).Mono().FontSize(11).Color(m.jsonTint(span.kind))
			}
			if folded {
				ui.Text(c, "…").Mono().FontSize(11).Muted()
			}
		})
		if folded {
			hiddenUntil = end + 1
		}
	}
}

// responseHeaders 是响应头的两列表格：名称定宽、值占满剩余宽度。
func (m *Model) responseHeaders(c ui.C) {
	if len(m.tables.respHeaders) == 0 {
		ui.Text(c, "没有响应头").Muted()
		return
	}
	ui.Row(c).FillWidth().Gap(12).PadY(4).Children(func() {
		ui.Text(c, "名称").FontSize(11).Muted().Width(220)
		ui.Text(c, "值").FontSize(11).Muted().Grow(1)
	})
	for _, header := range m.tables.respHeaders {
		ui.Row(c).FillWidth().Gap(12).PadY(2).Children(func() {
			ui.Text(c, header.Name).Mono().FontSize(11).Width(220)
			ui.Text(c, header.Value).Mono().FontSize(11).Grow(1).Ellipsis()
		})
	}
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
		ui.Row(c).FillWidth().Grow(1).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "源码").FontSize(11).Muted()
				ui.TextArea(c, &m.editor.text, "文档内容", "在此编写 Markdown…").FillWidth().Grow(1)
			})
			ui.Divider(c)
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "预览").FontSize(11).Muted()
				if strings.TrimSpace(m.editor.text) == "" {
					ui.Text(c, "（空文档）").Muted()
					return
				}
				ui.Scroll(c).Grow(1).Children(func() {
					m.previewBlock(c, m.editor.text, m.editor.previewKey, m.contentWidth(c)/2-20)
				})
			})
		})
	})
}

// previewBlock 把一段 Markdown 排好版画进当前布局（不加滚动容器，由调用方决定）。
func (m *Model) previewBlock(c ui.C, source, key string, width float32) {
	height := m.previewHeight(c, key, source, width)
	ui.Slot(c).FillWidth().Height(height).Draw(func(d *ui.DrawContext) {
		m.docs.view.Paint(d.Canvas, components.MarkdownPaint{
			Look: m.docs.look,
			X:    d.Rect.X,
			Y:    d.Rect.Y,
		})
	})
}

// codeCard 是等宽长文卡片（请求体原文与响应体共用）。
func (m *Model) codeCard(c ui.C, text string) {
	ui.Column(c).FillWidth().Pad(12).Draw(cardDraw).Children(func() {
		ui.Paragraph(c, text, 0).Mono().FontSize(11)
	})
}
