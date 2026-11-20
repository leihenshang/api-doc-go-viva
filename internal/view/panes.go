package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// requestTitle 是请求标题行：方法角标、名称、脏状态与「复制 cURL」；
// 接口概述排在标题**下方**（取草稿里的 summary，编辑后立刻跟着变）。
// 不再显示文件路径与分组：页签和集合树已经标明这是哪个条目。
func (m *Model) requestTitle(c ui.C, entry *explorer.Entry) {
	ui.Row(c).FillWidth().Gap(10).Items(ui.Center).Children(func() {
		methodBadge(c, entry)
		ui.Text(c, entry.Name).FontSize(17).Bold()
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
	if summary := strings.TrimSpace(m.req.summary); summary != "" {
		ui.Text(c, summary).FontSize(12).Muted().Ellipsis()
	}
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
			m.markEdited()
		}
		if ui.TextField(c, &m.req.url, "地址", "{{host}}/api/users").Grow(1).Changed() {
			m.markEdited()
		}
		saveLabel := "保存请求"
		if m.scratch {
			saveLabel = "另存为"
		}
		more := ui.Button(c, "更多操作").Icon(IconEllipsis).Ghost().Tooltip("更多操作")
		if more.Clicked() {
			c.After(func() { m.reqMenuOpen = !m.reqMenuOpen })
		}
		if m.reqMenuOpen {
			ui.Menu(c, more, &m.reqMenuOpen, func(menu *ui.MenuItems) {
				saveItem := menu.Item(saveLabel)
				if !m.req.dirty && !m.scratch {
					saveItem.Disabled(true)
				}
				if saveItem.Clicked() {
					c.After(m.saveRequest)
				}
				menu.Separator()
				if menu.Item("导入 CURL").Icon(IconImport).Clicked() {
					c.After(func() { m.openCurlImport(m.newRequestFolder()) })
				}
				if menu.Item("导出 CURL").Icon(IconCopy).Clicked() {
					c.After(m.exportCurl)
				}
				if menu.Item("代码生成").Icon(IconCode).Clicked() {
					c.After(m.openCodeDialog)
				}
				if menu.Item("格式化 URL").Icon(IconGlobe).Clicked() {
					c.After(m.formatURL)
				}
			})
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
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range *rows {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.name, "名称", "name").Width(150).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.value, "值", "value").Grow(1).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.desc, "说明", "").Width(160).Changed() {
				m.markEdited()
			}
			m.rowMover(c, index, len(*rows), func(from, to int) {
				if moveItem(*rows, from, to) {
					m.markEdited()
				}
			})
			if ui.Button(c, "删除").Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					*rows = append((*rows)[:slot], (*rows)[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, addLabel).Icon(IconPlus).Ghost().Clicked() {
			*rows = append(*rows, &kvRow{enabled: true})
			m.markEdited()
		}
	})
}

// rowActionWidth 是表格行尾「上移 / 下移 + 删除」三颗小按钮占的宽度（表头用它留位）。
const rowActionWidth = 92

// rowMover 是表格行尾的上移 / 下移：到顶的那一行上移置灰，到底的那一行下移置灰。
// 按下去只记一个「从哪一行挪到哪一行」，真正的交换交给 c.After，和别的按钮一致。
func (m *Model) rowMover(c ui.C, index, total int, onMove func(from, to int)) {
	up := ui.Button(c, "上移").Icon(IconChevronUp).Ghost().IconOnly().Tooltip("上移一行")
	if index == 0 {
		up.Disabled(true)
	}
	if up.Clicked() && index > 0 {
		c.After(func() { onMove(index, index-1) })
	}
	down := ui.Button(c, "下移").Icon(IconChevronDown).Ghost().IconOnly().Tooltip("下移一行")
	if index >= total-1 {
		down.Disabled(true)
	}
	if down.Clicked() && index < total-1 {
		c.After(func() { onMove(index, index+1) })
	}
}

// reorderItems 把 rows 这些行整体挪到 to 之前（List 的 Reorder 语义：
// rows 是拖动前的下标升序，to 也是拖动前的落点）。
func reorderItems[T any](list []T, rows []int, to int) bool {
	if len(list) == 0 || len(rows) == 0 || to < 0 || to > len(list) {
		return false
	}
	moved := make([]T, 0, len(rows))
	rest := make([]T, 0, len(list))
	picked := make(map[int]bool, len(rows))
	for _, index := range rows {
		if index >= 0 && index < len(list) {
			picked[index] = true
		}
	}
	for index, item := range list {
		if picked[index] {
			moved = append(moved, item)
			continue
		}
		rest = append(rest, item)
	}
	if len(moved) == 0 {
		return false
	}
	insert := to
	for _, index := range rows {
		if index < to {
			insert--
		}
	}
	if insert < 0 {
		insert = 0
	}
	if insert > len(rest) {
		insert = len(rest)
	}
	out := make([]T, 0, len(list))
	out = append(out, rest[:insert]...)
	out = append(out, moved...)
	out = append(out, rest[insert:]...)
	copy(list, out)
	return true
}

// moveItem 交换切片里相邻的两项；越界就原样不动（返回 false）。
func moveItem[T any](list []T, from, to int) bool {
	if from < 0 || to < 0 || from >= len(list) || to >= len(list) {
		return false
	}
	list[from], list[to] = list[to], list[from]
	return true
}

// respTabs / respTabLabel 是响应区的两个分段：正文与响应头。
var (
	respTabs      = []int{0, 1}
	respViewModes = []bool{false, true}
	respViewNames = []string{"格式化", "原始"}
	formTypes     = []string{"text", "file"}
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
			m.markEdited()
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
			m.markEdited()
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
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range m.req.fields {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.TextField(c, &row.name, "字段名", "id").Width(150).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.typ, "类型", "string").Width(100).Changed() {
				m.markEdited()
			}
			if ui.Checkbox(c, &row.required, "").Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.example, "示例", "abc").Width(160).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.desc, "说明", "").Grow(1).Changed() {
				m.markEdited()
			}
			m.rowMover(c, index, len(m.req.fields), func(from, to int) {
				if moveItem(m.req.fields, from, to) {
					m.markEdited()
				}
			})
			if ui.Button(c, "删除字段").Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					m.req.fields = append(m.req.fields[:slot], m.req.fields[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加字段").Icon(IconPlus).Ghost().Clicked() {
			m.req.fields = append(m.req.fields, &fieldRow{typ: "string"})
			m.markEdited()
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
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range m.req.form {
		slot := index
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.name, "名称", "field").Width(150).Changed() {
				m.markEdited()
			}
			placeholder := "value"
			if multipart && row.typ == "file" {
				placeholder = "相对集合根的文件路径"
			}
			if ui.TextField(c, &row.value, "值", placeholder).Grow(1).Changed() {
				m.markEdited()
			}
			if multipart {
				if ui.Select(c, &row.typ, formTypes, nil).Width(96).Changed() {
					m.markEdited()
				}
			}
			if ui.TextField(c, &row.desc, "说明", "").Width(140).Changed() {
				m.markEdited()
			}
			m.rowMover(c, index, len(m.req.form), func(from, to int) {
				if moveItem(m.req.form, from, to) {
					m.markEdited()
				}
			})
			if ui.Button(c, "删除").Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					m.req.form = append(m.req.form[:slot], m.req.form[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加表单字段").Icon(IconPlus).Ghost().Clicked() {
			m.req.form = append(m.req.form, &kvRow{enabled: true, typ: "text"})
			m.markEdited()
		}
	})
}

// authTab 编辑认证；none 不写 auth 段，inherit 表示继承集合默认。
func (m *Model) authTab(c ui.C) {
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "类型").FontSize(11).Muted()
		if ui.Select(c, &m.req.authType, authTypes, nil).Width(140).Changed() {
			m.markEdited()
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
				m.markEdited()
			}
			if ui.TextField(c, &m.req.authPass, "口令", "{{password}}").Grow(1).Secure().Changed() {
				m.markEdited()
			}
		})
	case "bearer":
		if ui.TextField(c, &m.req.authToken, "Token", "{{token}}").Grow(1).Changed() {
			m.markEdited()
		}
	case "apikey":
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if ui.TextField(c, &m.req.authKey, "Key", "X-API-Key").Grow(1).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &m.req.authValue, "Value", "{{apiKey}}").Grow(1).Changed() {
				m.markEdited()
			}
			if ui.Select(c, &m.req.authIn, authPlaces, nil).Width(110).Changed() {
				m.markEdited()
			}
		})
	}
}

// docsTab 是接口文档：默认只读预览，点预览区或「编辑」才进编辑态。
// 编辑态只放概述与 Markdown 原文 —— 左右分栏时左栏只有 400 多像素，
// 再并排一列实时预览会把两边都挤没；要看效果就切回预览。
func (m *Model) docsTab(c ui.C) {
	if !m.docsEditing {
		m.docsPreview(c)
		return
	}
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "概述").FontSize(11).Muted()
		if ui.TextField(c, &m.req.summary, "概述", "一句话说明这个接口").Grow(1).Changed() {
			m.markEdited()
		}
	})
	ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Text(c, "详细说明（Markdown）").FontSize(11).Muted()
		ui.Spacer(c)
		if ui.Button(c, "预览").Ghost().Small().Tooltip("完成编辑，回到预览").Clicked() {
			c.After(func() { m.docsEditing = false })
		}
	})
	if ui.TextArea(c, &m.req.docs, "详细说明", "支持标题、列表、表格与代码块").
		FillWidth().Height(320).Changed() {
		m.markEdited()
	}
}

// docsPreview 是文档分段的只读视图：整块可点进编辑态，右上角另有「编辑」按钮。
func (m *Model) docsPreview(c ui.C) {
	area := ui.Column(c).FillWidth().Gap(6).Pad(2).
		Hit(ui.ID(c), "文档预览").Tooltip("点击编辑这个接口的文档")
	area.Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
			ui.Text(c, "点击内容或「编辑」修改").FontSize(11).Muted()
			ui.Spacer(c)
			if ui.Button(c, "编辑").Ghost().Small().Tooltip("编辑概述与 Markdown 原文").Clicked() {
				c.After(func() { m.docsEditing = true })
			}
		})
		if strings.TrimSpace(m.req.docs) == "" {
			ui.Text(c, "（还没有文档，点击这里开始写）").FontSize(12).Muted()
			return
		}
		m.previewBlock(c, m.req.docs, m.tables.docsKey, m.paneWidth(c))
	})
	if area.Clicked() {
		c.After(func() { m.docsEditing = true })
	}
}

// responsePanel 是请求页下方的响应区：状态行固定，正文滚动。
func (m *Model) responsePanel(c ui.C, entry *explorer.Entry) {
	theme := c.Theme()
	if entry.Kind == explorer.KindHTTP {
		if ui.Shortcut(c, ui.Primary, "f") {
			c.After(m.openFind)
		}
		if m.find.open && ui.Shortcut(c, 0, "escape") {
			c.After(m.closeFind)
		}
	}
	// 窄栏（左右分栏把响应拖窄、或窗口本身就窄）只留最要紧的信息：
	// 状态码 + 下拉。reason / 耗时 / 体积 / 分段名会让这一行溢出，把下拉顶到窗外。
	narrow := m.paneWidth(c) < 420
	ui.Column(c).Fill().Pad(panePadding).Gap(8).Radius(theme.Metrics.RadiusSurface).
		Background(theme.Card).Border(1, m.pal.border).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			// 状态在最左、且不参与收缩：窄栏（左右分栏把响应拖窄）时宁可裁掉右边的分段控件，
			// 也不能让 200 被压成 0 宽——那是这一栏最要紧的信息。
			ui.Text(c, "响应").FontSize(12).Bold().Shrink(0)
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "gRPC 只读").FontSize(11).Muted().Shrink(0)
			case m.sending:
				ui.Spinner(c)
			case m.hasResp || m.failText != "":
				statusTint := theme.Success
				if !m.statusOK || m.failText != "" {
					statusTint = theme.Danger
				}
				if m.status != "" {
					ui.Text(c, m.status).FontSize(12).Bold().Color(statusTint).Shrink(0)
				}
				if m.reason != "" && !narrow {
					ui.Text(c, m.reason).FontSize(11).Muted().Shrink(0)
				}
				if m.elapsed != "" && !narrow {
					ui.Text(c, m.elapsed+" · "+m.size).FontSize(11).Muted().Mono().Shrink(0)
				}
				if m.copyNote != "" {
					// 可收缩 + 省略号：提示再长也不能把右边的两个下拉顶出去。
					ui.Text(c, m.copyNote).FontSize(11).Muted().Ellipsis()
				}
			}
			if m.exSel != "" && !narrow {
				m.exampleTag(c)
			}
			ui.Spacer(c)
			// 右上角两个下拉，与 Wails 版同位置：切「看哪一份」+ 存 / 删当前这份。
			if entry.Kind == explorer.KindHTTP {
				m.examplePicker(c)
			}
		})
		// 第二行：分段在左，格式化/复制在右。分开是因为挤在一行时窄栏会把状态码顶掉。
		if entry.Kind == explorer.KindHTTP {
			ui.Row(c).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Tabs(c, &m.respTab, respTabs, respTabLabel).Line().Grow(0)
				ui.Spacer(c)
				// 保存放在这一行：第一行被状态与下拉占满，1180 宽时按钮会被挤到窗外。
				m.responseSaveMenu(c, narrow)
				if m.respTab == 0 && len(m.tables.json) > 0 {
					m.responseViewSwitch(c)
				}
				if m.hasResp {
					if ui.Button(c, "复制正文").Icon(IconCopy).Ghost().IconOnly().
						Tooltip("复制响应正文").Clicked() {
						c.After(m.copyResponse)
					}
				}
			})
		}
		if m.find.open && entry.Kind == explorer.KindHTTP {
			m.findBar(c)
		}
		body := ui.Scroll(c).Grow(1).FillWidth()
		m.scrollToFind(body)
		body.Children(func() {
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "验证版只读展示 gRPC 元数据，不发起调用。").FontSize(11).Muted()
			case m.sending:
				ui.Text(c, "请求中…").Muted()
			case !m.hasResp:
				ui.Text(c, "尚未发送：点地址栏右侧「发送」运行这条请求；"+
					"发送后可在右上角「当前响应」下拉里把它存成示例（Mock 回放与写文档都用它）。").
					FontSize(12).Muted().MaxLines(2)
			case m.failText != "":
				ui.Text(c, "请求失败："+m.failText).FontSize(11).Color(theme.Danger)
			case m.respTab == 1:
				m.responseHeaders(c)
			default:
				if m.missing != "" {
					ui.Text(c, m.missing).FontSize(11).Color(theme.Danger)
				}
				if m.resp.Truncated {
					ui.Text(c, truncationNote(m.resp)).FontSize(11).Color(theme.Danger)
				}
				if len(m.resp.Body) > rawDisplayLimit {
					ui.Text(c, "正文较大，这里只渲染前 "+humanBytes(rawDisplayLimit)+
						"（「复制正文」仍是完整内容）").FontSize(11).Muted()
				}
				if m.bodyText == "" {
					ui.Text(c, "响应体为空").Muted()
					return
				}
				// 文件类响应（图片 / PDF / 二进制 / 下载）：不当文本渲染，给保存卡片。
				// 仍然保留「原始」这一路：想硬看字节的人可以切过去。
				if m.resp.IsFile() && !m.rawResp {
					m.fileResponse(c)
					return
				}
				if m.rawResp || len(m.tables.json) == 0 {
					// 原始报文同样可选：拖动选中 + Ctrl+C（见 jsonselect.go）。
					m.rawBody(c)
					return
				}
				m.jsonBody(c)
			}
		})
	})
}

// truncationNote 是「响应太大，只读回了一段」的提示。
// 有 Content-Length 就说清总共多大，没有就只说显示了多少。
func truncationNote(out send.Outcome) string {
	note := "响应超过上限，只显示前 " + humanBytes(out.Size)
	if out.TotalSize > int64(out.Size) {
		note += "（共 " + humanBytes(int(out.TotalSize)) + "）"
	}
	return note
}

// responseViewSwitchMin 是「格式化 / 原始」两段并排的最低栏宽；再窄就收成一个图标按钮。
const responseViewSwitchMin = 620

// responseViewSwitch 是「格式化 / 原始」的切换：栏够宽时两段并排，
// 左右分栏把响应拖窄时收成一个图标按钮，点一下切到另一种视图 ——
// 宁可少一个分段，也不能把状态码挤掉。按钮名仍是「原始 / 格式化」，读屏与用例照旧。
func (m *Model) responseViewSwitch(c ui.C) {
	if m.paneWidth(c) >= responseViewSwitchMin {
		ui.Choice(c, &m.rawResp, respViewModes, func(raw bool) string {
			if raw {
				return respViewNames[1]
			}
			return respViewNames[0]
		})
		return
	}
	target := respViewNames[1]
	if m.rawResp {
		target = respViewNames[0]
	}
	if ui.Button(c, target).Icon(IconCode).Ghost().IconOnly().
		Tooltip("切换到「" + target + "」视图").Clicked() {
		c.After(func() {
			m.rawResp = !m.rawResp
			m.storeTab()
			m.invalidator.Invalidate()
		})
	}
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
func (m *Model) copyResponse() { m.copyValue(m.resp.Body) }

// copyValue 把任意响应片段写进剪贴板：整段正文、某一行 JSON 或某条响应头。
func (m *Model) copyValue(value string) {
	if err := app.SetClipboardText(value); err != nil {
		m.copyNote = "复制失败：" + err.Error()
	} else {
		m.copyNote = "已复制"
	}
	m.storeTab()
	m.invalidator.Invalidate()
}

// responseHeaders 是响应头的两列表格：名称定宽、值占满剩余宽度（整行可点复制）。
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
		row := ui.Row(c).FillWidth().Gap(12).PadY(2).Items(ui.Center).
			Hover(ui.Look{}.Background(m.pal.hover)).
			Hit(ui.ID(c), "响应头 "+header.Name).
			Tooltip("点击复制这一条")
		row.Children(func() {
			ui.Text(c, header.Name).Mono().FontSize(11).Width(220)
			ui.Text(c, header.Value).Mono().FontSize(11).Grow(1).Ellipsis()
		})
		if row.Clicked() {
			m.copyValue(header.Name + ": " + header.Value)
		}
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
