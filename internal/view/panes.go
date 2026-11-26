package view

import (
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/card"
	"github.com/vivaui/viva/ui/descriptions"
	"github.com/vivaui/viva/ui/typography"
)

// requestTitle 是请求标题行：方法角标、名称、脏状态与「复制 cURL」；
// 接口概述排在标题**下方**（取草稿里的 summary，编辑后立刻跟着变）。
// 不再显示文件路径与分组：页签和集合树已经标明这是哪个条目。
func (m *Model) requestTitle(c ui.C, entry *explorer.Entry) {
	ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
		methodBadge(c, entry)
		ui.Text(c, entry.Name).FontSize(fs(17)).Bold()
		ui.Spacer(c)
		if m.req.note != "" {
			ui.Text(c, m.req.note).FontSize(fs(11)).Muted()
		}
		if ui.Button(c, "复制 cURL").Icon(IconCopy).Ghost().Small().Clicked() {
			c.After(func() { m.copyCurl(entry) })
		}
		if m.req.dirty {
			ui.Text(c, "未保存").FontSize(fs(11)).Color(c.Theme().Danger)
		}
	})
	if summary := strings.TrimSpace(m.req.summary); summary != "" {
		ui.Text(c, summary).FontSize(fs(12)).Muted().Ellipsis()
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
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		if ui.Select(c, &m.req.method, httpMethods, nil).Width(px(120)).Changed() {
			m.markEdited()
		}
		address := ui.TextField(c, &m.req.url, "地址", "{{host}}/api/users").Grow(1)
		if m.varTipField(address, m.req.url).Changed() {
			m.markEdited()
		}
		saveLabel := "保存请求"
		if m.scratch {
			saveLabel = "另存为"
		}
		// 只画三个点：地址栏右侧寸土寸金，动作名放进悬停提示与无障碍名就够了。
		more := ui.Button(c, "更多操作").Icon(IconEllipsis).Ghost().IconOnly().Tooltip("更多操作")
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
				menu.Separator()
				// 请求级设置：超时 / 重定向 / TLS / 编码。临时请求没有文件可写，置灰。
				reqSet := menu.Item("请求设置…").Icon(IconGear).Disabled(m.scratch)
				if reqSet.Clicked() {
					c.After(m.openReqSettings)
				}
			})
		}
		// 忙碌态用 viva 的 Loading：它期间不派发按下（第二次点不会重复发）、也不翻转绑定值，
		// 外观保持不变，只在图标位换成指示器——比换成「发送中…」的禁用按钮稳（宽度不跳）。
		send := ui.Button(c, "发送").Icon(IconSend).Tooltip("发送（Ctrl/Cmd+Enter）").Loading(m.sending)
		if send.Clicked() {
			m.start(entry)
		}
	})
}

// kvEditor 是表格式键值编辑：表头 + 每行（启用 / 名称 / 值 / 说明 / 删除）+ 底部添加。
func (m *Model) kvEditor(c ui.C, rows *[]*kvRow, addLabel string) {
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Slot(c).Width(px(24))
		// v0.6.8 的 Row 不再压缩子项：列宽用 Grow 比例分配，窄栏也能排下。
		ui.Text(c, "名称").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		ui.Text(c, "说明").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range *rows {
		slot := index
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.markEdited()
			}
			name := ui.TextField(c, &row.name, "名称", "name").Grow(2).MinWidth(0)
			if m.varTipField(name, row.name).Changed() {
				m.markEdited()
			}
			value := ui.TextField(c, &row.value, "值", "value").Grow(3).MinWidth(0)
			if m.varTipField(value, row.value).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.desc, "说明", "").Grow(2).MinWidth(0).Changed() {
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
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "类型").FontSize(fs(11)).Muted()
		if ui.Select(c, &m.req.bodyType, bodyTypes, nil).Width(px(140)).Changed() {
			m.markEdited()
		}
		ui.Spacer(c)
	})
	switch m.req.bodyType {
	case "", "none":
		ui.Text(c, "无请求体（空字符串与 `none` 同义）。").FontSize(fs(11)).Muted()
	case "form", "multipart":
		m.formEditor(c, m.req.bodyType == "multipart")
	default:
		body := ui.TextArea(c, &m.req.bodyRaw, "请求体", `{"name": "value"}`).FillWidth().Height(px(240))
		if m.varTipBox(body, m.req.bodyRaw).Changed() {
			m.markEdited()
		}
	}
	m.fieldEditor(c)
}

// fieldEditor 编辑请求体字段表（body.fields）：字段名 / 类型 / 必填 / 示例 / 说明。
func (m *Model) fieldEditor(c ui.C) {
	m.fieldTable(c, &m.req.fields, "字段")
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		if ui.Button(c, "添加字段").Icon(IconPlus).Ghost().Clicked() {
			m.req.fields = append(m.req.fields, &fieldRow{typ: "string"})
			m.markEdited()
		}
		// 字段表可以照着一次真实返回自动生成：省得手敲路径与类型。
		if ui.Button(c, "从当前响应生成").Icon(IconImport).Outline().
			Tooltip("按当前响应推字段名与类型；同名字段的说明、示例、必填保持不变").
			Disabled(!m.hasResp).Clicked() {
			c.After(m.generateFields)
		}
		if len(m.req.fields) > 0 {
			if ui.Button(c, "清空字段").Icon(IconTrash).Outline().Clicked() {
				c.After(m.clearFields)
			}
		}
	})
}

// fieldTable 是 5 列表格本体（请求体字段表与响应字段表共用）。
// 只画行与行尾操作；「添加 / 生成 / 清空」这排按钮由调用方按自己的语义摆。
func (m *Model) fieldTable(c ui.C, rows *[]*fieldRow, noun string) {
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "字段名").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		ui.Text(c, "类型").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "必填").FontSize(fs(11)).Muted().Width(px(40))
		ui.Text(c, "示例").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "说明").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range *rows {
		slot := index
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.TextField(c, &row.name, "字段名", "id").Grow(3).MinWidth(0).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.typ, "类型", "string").Grow(2).MinWidth(0).Changed() {
				m.markEdited()
			}
			if ui.Checkbox(c, &row.required, "").Changed() {
				m.markEdited()
			}
			example := ui.TextField(c, &row.example, "示例", "abc").Width(px(160))
			if m.varTipField(example, row.example).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.desc, "说明", "").Grow(1).Changed() {
				m.markEdited()
			}
			m.rowMover(c, index, len(*rows), func(from, to int) {
				if moveItem(*rows, from, to) {
					m.markEdited()
				}
			})
			if ui.Button(c, "删除"+noun).Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					*rows = append((*rows)[:slot], (*rows)[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
}

// formEditor 编辑 form / multipart 的逐行字段；multipart 多一列「类型」（text 或 file）。
func (m *Model) formEditor(c ui.C, multipart bool) {
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Slot(c).Width(px(24))
		ui.Text(c, "名称").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		if multipart {
			ui.Text(c, "类型").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		}
		ui.Text(c, "说明").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range m.req.form {
		slot := index
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.markEdited()
			}
			name := ui.TextField(c, &row.name, "名称", "field").Grow(2).MinWidth(0)
			if m.varTipField(name, row.name).Changed() {
				m.markEdited()
			}
			placeholder := "value"
			if multipart && row.typ == "file" {
				placeholder = "相对集合根的文件路径"
			}
			value := ui.TextField(c, &row.value, "值", placeholder).Grow(3).MinWidth(0)
			if m.varTipField(value, row.value).Changed() {
				m.markEdited()
			}
			if multipart {
				if ui.Select(c, &row.typ, formTypes, nil).Grow(2).MinWidth(0).Changed() {
					m.markEdited()
				}
			}
			if ui.TextField(c, &row.desc, "说明", "").Grow(2).MinWidth(0).Changed() {
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
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "类型").FontSize(fs(11)).Muted()
		if ui.Select(c, &m.req.authType, authTypes, nil).Width(px(140)).Changed() {
			m.markEdited()
		}
		ui.Spacer(c)
	})
	switch m.req.authType {
	case "", "none":
		ui.Text(c, "不发送认证信息。").FontSize(fs(11)).Muted()
	case "inherit":
		ui.Text(c, "沿用集合清单 defaults.auth（值支持 {{变量}}）。").FontSize(fs(11)).Muted()
	case "basic":
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			user := ui.TextField(c, &m.req.authUser, "用户名", "{{username}}").Grow(1)
			if m.varTipField(user, m.req.authUser).Changed() {
				m.markEdited()
			}
			// 口令是密文控件，悬停不显示取值，避免把密码摆到屏幕上。
			if ui.TextField(c, &m.req.authPass, "口令", "{{password}}").Grow(1).Secure().Changed() {
				m.markEdited()
			}
		})
	case "bearer":
		token := ui.TextField(c, &m.req.authToken, "Token", "{{token}}").Grow(1)
		if m.varTipField(token, m.req.authToken).Changed() {
			m.markEdited()
		}
	case "apikey":
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			key := ui.TextField(c, &m.req.authKey, "Key", "X-API-Key").Grow(1)
			if m.varTipField(key, m.req.authKey).Changed() {
				m.markEdited()
			}
			value := ui.TextField(c, &m.req.authValue, "Value", "{{apiKey}}").Grow(1)
			if m.varTipField(value, m.req.authValue).Changed() {
				m.markEdited()
			}
			if ui.Select(c, &m.req.authIn, authPlaces, nil).Width(px(110)).Changed() {
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
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "概述").FontSize(fs(11)).Muted()
		if ui.TextField(c, &m.req.summary, "概述", "一句话说明这个接口").Grow(1).Changed() {
			m.markEdited()
		}
	})
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "详细说明（Markdown）").FontSize(fs(11)).Muted()
		ui.Spacer(c)
		if ui.Button(c, "预览").Ghost().Small().Tooltip("完成编辑，回到预览").Clicked() {
			c.After(func() { m.docsEditing = false })
		}
	})
	if ui.TextArea(c, &m.req.docs, "详细说明", "支持标题、列表、表格与代码块").
		FillWidth().Height(px(320)).Changed() {
		m.markEdited()
	}
}

// docsPreview 是文档分段的只读视图：整块可点进编辑态，右上角另有「编辑」按钮。
func (m *Model) docsPreview(c ui.C) {
	area := ui.Column(c).FillWidth().Gap(px(6)).Pad(px(2)).
		Hit(ui.ID(c), "文档预览").Tooltip("点击编辑这个接口的文档")
	area.Children(func() {
		ui.Row(c).FillWidth().Items(ui.Center).Children(func() {
			ui.Text(c, "点击内容或「编辑」修改").FontSize(fs(11)).Muted()
			ui.Spacer(c)
			if ui.Button(c, "编辑").Ghost().Small().Tooltip("编辑概述与 Markdown 原文").Clicked() {
				c.After(func() { m.docsEditing = true })
			}
		})
		if strings.TrimSpace(m.req.docs) == "" {
			ui.Text(c, "（还没有文档，点击这里开始写）").FontSize(fs(12)).Muted()
			return
		}
		m.previewBlock(c, m.req.docs, m.tables.docsKey, docBase(m.req.file), m.paneWidth(c))
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
	ui.Column(c).Fill().Pad(panePadding).Gap(px(8)).Radius(theme.Metrics.RadiusSurface).
		Background(theme.Card).Border(1, m.pal.border).Children(func() {
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			// 状态在最左、且不参与收缩：窄栏（左右分栏把响应拖窄）时宁可裁掉右边的分段控件，
			// 也不能让 200 被压成 0 宽——那是这一栏最要紧的信息。
			ui.Text(c, "响应").FontSize(fs(12)).Bold().Shrink(0)
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "gRPC 只读").FontSize(fs(11)).Muted().Shrink(0)
			case m.sending:
				ui.Spinner(c)
			case m.hasResp || m.failText != "":
				statusTint := theme.Success
				if !m.statusOK || m.failText != "" {
					statusTint = theme.Danger
				}
				if m.status != "" {
					ui.Text(c, m.status).FontSize(fs(12)).Bold().Color(statusTint).Shrink(0)
				}
				if m.reason != "" && !narrow {
					ui.Text(c, m.reason).FontSize(fs(11)).Muted().Shrink(0)
				}
				if m.elapsed != "" && !narrow {
					ui.Text(c, m.elapsed+" · "+m.size).FontSize(fs(11)).Muted().Mono().Shrink(0)
				}
				if m.copyNote != "" {
					// 可收缩 + 省略号：提示再长也不能把右边的两个下拉顶出去。
					ui.Text(c, m.copyNote).FontSize(fs(11)).Muted().Ellipsis()
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
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
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
		// 连接信息单独一行：塞进上面那行会和「保存 / 原始 / 复制」抢宽度
		// （实测 434 宽时按钮的包围盒直接落到窗口外），而这一行只放它自己，
		// 窄栏用省略号截断即可。
		if entry.Kind == explorer.KindHTTP && m.hasResp {
			if info := m.connInfo(false); info != "" {
				ui.Text(c, info).FontSize(fs(10)).Mono().Muted().FillWidth().Ellipsis()
			}
		}
		if m.find.open && entry.Kind == explorer.KindHTTP {
			m.findBar(c)
		}
		// 断言与脚本结果紧跟状态行：跑完就能看到成败，不用滚到底部找。
		if entry.Kind == explorer.KindHTTP && (len(m.assertOut) > 0 || m.scriptErr != "") {
			m.assertSummary(c)
		}
		body := ui.Scroll(c).Grow(1).FillWidth()
		m.scrollToFind(body)
		body.Children(func() {
			switch {
			case entry.Kind != explorer.KindHTTP:
				ui.Text(c, "验证版只读展示 gRPC 元数据，不发起调用。").FontSize(fs(11)).Muted()
			case m.sending:
				ui.Text(c, "请求中…").Muted()
			case !m.hasResp:
				ui.Text(c, "尚未发送：点地址栏右侧「发送」运行这条请求；"+
					"发送后可在右上角「当前响应」下拉里把它存成示例（Mock 回放与写文档都用它）。").
					FontSize(12).Muted().MaxLines(2)
			case m.failText != "":
				// 失败原因常常很长（TLS 握手、DNS、超时链）：用 typography 截断到 3 行，
				// 需要时展开，或一键复制整段给同事看。typography 只有 Muted、没有危险色，
				// 所以警示色留在「请求失败：」这个前缀上，原因用正文色更好读。
				// 前缀单独一行：右侧列宽很窄，挤在同一行会把「请求失败」折成两三行。
				ui.Column(c).FillWidth().Gap(px(2)).Children(func() {
					ui.Text(c, "请求失败").FontSize(fs(11)).Color(theme.Danger)
					m.ui.failView.View(c, m.failText, typography.Spec{
						Rows: 3, Expandable: true, Copyable: true,
						Labels: typography.Labels{More: "展开", Less: "收起", Copy: "复制"},
					}).FillWidth()
				})
				if m.ui.failView.Copied() {
					m.copyNote = "已复制失败原因"
				}
			case m.respTab == 1:
				m.responseHeaders(c)
			default:
				if m.missing != "" {
					ui.Text(c, m.missing).FontSize(fs(11)).Color(theme.Danger)
				}
				if m.resp.Truncated {
					ui.Text(c, truncationNote(m.resp)).FontSize(fs(11)).Color(theme.Danger)
				}
				if len(m.resp.Body) > rawDisplayLimit {
					ui.Text(c, "正文较大，这里只渲染前 "+humanBytes(rawDisplayLimit)+
						"（「复制正文」仍是完整内容）").FontSize(fs(11)).Muted()
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

// assertSummary 是响应区里的断言结果：一条汇总 + 失败明细（最多 5 条）。
//
// 明细只列失败的：全通过时一行「断言 3/3 通过」就够，不占正文的可见高度。
func (m *Model) assertSummary(c ui.C) {
	theme := c.Theme()
	passed := 0
	for _, item := range m.assertOut {
		if item.Passed {
			passed++
		}
	}
	if len(m.assertOut) > 0 {
		tint := theme.Success
		if passed != len(m.assertOut) {
			tint = theme.Danger
		}
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			ui.Text(c, "断言").FontSize(fs(11)).Muted().Shrink(0)
			ui.Text(c, strconv.Itoa(passed)+"/"+strconv.Itoa(len(m.assertOut))+" 通过").
				FontSize(fs(11)).Bold().Color(tint).Shrink(0)
			if len(m.assertOut) > passed {
				ui.Text(c, "有失败项").FontSize(fs(11)).Color(theme.Danger).Shrink(0)
			}
		})
	}
	if m.scriptErr != "" {
		ui.Text(c, "脚本错误："+m.scriptErr).FontSize(fs(11)).Color(theme.Danger).MaxLines(2).Ellipsis()
	}
	shown := 0
	for _, item := range m.assertOut {
		if item.Passed || shown >= 5 {
			continue
		}
		shown++
		line := "✗ "
		if item.Name != "" {
			line += item.Name + "："
		}
		line += item.Expr
		if item.Error != "" {
			line += "（" + item.Error + "）"
		}
		ui.Text(c, line).FontSize(fs(11)).Color(theme.Danger).MaxLines(1).Ellipsis()
	}
	if len(m.assertOut)-passed > shown {
		ui.Text(c, "还有 "+strconv.Itoa(len(m.assertOut)-passed-shown)+" 条失败未展开").
			FontSize(fs(10)).Muted().Ellipsis()
	}
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
	if err := ui.SetClipboardText(value); err != nil {
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
	ui.Row(c).FillWidth().Gap(px(12)).PadY(px(4)).Children(func() {
		ui.Text(c, "名称").FontSize(fs(11)).Muted().Width(px(220))
		ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(1)
	})
	for _, header := range m.tables.respHeaders {
		row := ui.Row(c).FillWidth().Gap(px(12)).PadY(px(2)).Items(ui.Center).
			Hover(ui.Look{}.Background(m.pal.hover)).
			Hit(ui.ID(c), "响应头 "+header.Name).
			Tooltip("点击复制这一条")
		row.Children(func() {
			ui.Text(c, header.Name).Mono().FontSize(fs(11)).Width(px(220))
			ui.Text(c, header.Value).Mono().FontSize(fs(11)).Grow(1).Ellipsis()
		})
		if row.Clicked() {
			m.copyValue(header.Name + ": " + header.Value)
		}
	}
}

// grpcTab 是 gRPC 请求页：连接信息只读，元数据与请求消息可编辑，右上角「调用」。
func (m *Model) grpcTab(c ui.C, entry *explorer.Entry) {
	if entry.Payload.GRPC == nil {
		ui.Text(c, "缺少 grpc 段").Muted()
		return
	}
	block := entry.Payload.GRPC
	if ui.Shortcut(c, ui.Primary, "s") && m.grpcDirty {
		c.After(m.saveGRPC)
	}
	if ui.Shortcut(c, ui.Primary, "enter") && !m.grpcSending {
		c.After(func() { m.sendGRPC(entry) })
	}
	ui.Column(c).FillWidth().Gap(px(8)).Children(func() {
		// 连接信息是两条只读的「标签-值」，用 ui/descriptions 排（地址保留等宽）。
		info := m.ui.grpcInfo[:0]
		info = append(info, descriptions.Item{Label: "地址", Content: func(c ui.C) {
			ui.Text(c, m.tables.grpcEndpoint).Mono().FontSize(fs(12))
		}})
		if m.tables.grpcMeta != "" {
			info = append(info, descriptions.Item{Label: "说明", Value: m.tables.grpcMeta})
		}
		m.ui.grpcInfo = info
		descriptions.View(c, "", descriptions.Spec{
			Items: info, Columns: 1, LabelWidth: px(48), Size: descriptions.Small,
		}).FillWidth()
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			ui.Text(c, "元数据").FontSize(fs(11)).Muted()
			ui.Spacer(c)
			if ui.Button(c, "添加元数据").Icon(IconPlus).Ghost().XSmall().Clicked() {
				c.After(func() {
					m.grpcMetaRows = append(m.grpcMetaRows, &kvRow{enabled: true})
					m.grpcDirty = true
					m.invalidator.Invalidate()
				})
			}
		})
		for index, row := range m.grpcMetaRows {
			slot := index
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				if ui.Checkbox(c, &row.enabled, "").Changed() {
					m.grpcDirty = true
				}
				if ui.TextField(c, &row.name, "名称", "x-trace-id").Width(px(200)).Changed() {
					m.grpcDirty = true
				}
				metaValue := ui.TextField(c, &row.value, "值", "{{trace}}").Grow(1)
				if m.varTipField(metaValue, row.value).Changed() {
					m.grpcDirty = true
				}
				if ui.Button(c, "删除元数据").Icon(IconTrash).Ghost().IconOnly().
					Tooltip("删除这一条").Clicked() {
					c.After(func() {
						m.grpcMetaRows = append(m.grpcMetaRows[:slot], m.grpcMetaRows[slot+1:]...)
						m.grpcDirty = true
						m.invalidator.Invalidate()
					})
				}
			})
		}
		ui.Text(c, "请求消息（protojson）").FontSize(fs(11)).Muted()
		grpcMessage := ui.TextArea(c, &m.grpcMessage, "请求消息", `{"name":"neo"}`).Height(px(150)).FillWidth()
		if m.varTipBox(grpcMessage, m.grpcMessage).Changed() {
			m.grpcDirty = true
		}
		if line := m.grpcResultLine(); line != "" {
			tint := c.Theme().Success
			if m.grpcStatus != "OK" {
				tint = c.Theme().Danger
			}
			ui.Text(c, line).FontSize(fs(11)).Mono().Color(tint)
		}
		if m.grpcNote != "" {
			ui.Text(c, m.grpcNote).FontSize(fs(11)).Muted()
		}
		if strings.TrimSpace(m.grpcBody) != "" {
			// 外层已经是 Scroll（page.go 的详情区）：这里再套一层会把高度算成 0。
			ui.Paragraph(c, m.grpcBody, 0).Mono().FontSize(fs(11))
		}
		if block.Stream == "client" || block.Stream == "bidi" {
			ui.Text(c, "客户端流 / 双工暂不支持，调用时会明确报错。").FontSize(fs(11)).Muted()
		}
	})
}

// docDetail 是一篇 Markdown 的编辑 / 预览。
func (m *Model) docDetail(c ui.C, entry *explorer.Entry) {
	if ui.Shortcut(c, ui.Primary, "s") {
		c.After(m.saveDoc)
	}
	// 源码区拿着焦点时接管粘贴：剪贴板里是截图就存成 assets 里的图，
	// 否则按文本插入（见 docimage.go）。只管这一个控件，别抢别的输入框。
	if m.ui.docSplit && !m.docCodeID.IsZero() && c.Canvas().Focused() == m.docCodeID &&
		ui.Shortcut(c, ui.Primary, "v") {
		c.After(m.pasteDocClipboard)
	}
	dirty := m.docDirty()
	ui.Column(c).Fill().Pad(px(20)).Gap(px(12)).Children(func() {
		ui.Row(c).FillWidth().Gap(px(10)).Items(ui.Center).Children(func() {
			methodBadge(c, entry)
			ui.Text(c, entry.Name).FontSize(fs(18)).Bold()
			ui.Spacer(c)
			switch {
			case dirty && m.editor.status != "":
				// 有改动时也要能看到「已插入图片」这类提示，不能只显示「未保存」。
				ui.Text(c, m.editor.status+" · 未保存").FontSize(fs(11)).Color(c.Theme().Danger)
			case dirty:
				ui.Text(c, "未保存").FontSize(fs(11)).Color(c.Theme().Danger)
			case m.editor.status != "":
				ui.Text(c, m.editor.status).FontSize(fs(11)).Muted()
			}
			viewLabel := "编辑"
			if m.ui.docSplit {
				viewLabel = "预览"
			}
			if ui.Button(c, viewLabel).Ghost().Small().Tooltip("切换文档视图（默认值在设置里改）").Clicked() {
				c.After(func() { m.setDocSplit(!m.ui.docSplit) })
			}
			save := ui.Button(c, "保存")
			if !dirty {
				save.Disabled(true)
			}
			if save.Clicked() {
				c.After(m.saveDoc)
			}
		})
		ui.Text(c, m.editor.metaLine).FontSize(fs(11)).Muted().Mono()
		if m.ui.docSplit {
			// 双屏：左写右看。
			column := (m.contentWidth(c) - px(24) - px(1)) / 2
			text := column - 2*px(12)
			ui.Row(c).FillWidth().Grow(1).FillHeight().Gap(px(12)).Children(func() {
				m.docSourcePane(c)
				ui.Divider(c)
				m.docPreviewPane(c, text)
			})
		} else {
			// 默认全宽预览（设置里可以改成默认双屏）。
			m.docPreviewPane(c, m.contentWidth(c)-2*px(12))
		}
	})
}

// docSourcePane 是源码编辑栏：用 viva 的 codeeditor——行号栏、当前行高亮、
// 选区与滚动条都由组件自带；正文真源仍是 m.editor.text（见 docode.go）。
//
// 这里没有用 ui/codeeditor.View 的 wrapper，而是自己声明控件、自己调
// components.CodeEditor.Paint，原因是 wrapper 的控件命中会把文本框的角色抢在你前面：
// 它先以 RoleTextField 声明了一个**没有文本几何**的命中（Multiline=false、
// Placement=nil），而 interaction.TextInputHit 取的是「第一个 RoleTextField 命中」。
// 于是平台文本层把这份多行 Markdown 当单行：按下只按 x 换算插入符（点哪都跑到开头几行）、
// 还会把整篇的第一行另画一遍。把控件的角色让成 RoleGroup，命中里就只剩组件自己那一个
// （Multiline=true、Placement=CodeEditor），平台既不抢指针、也不抢插入符。
func (m *Model) docSourcePane(c ui.C) {
	m.docCodeDocument()
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		snap := &m.docCode.Snapshot
		snap.Placeholder = "在此编写 Markdown…"
		snap.LineNumbers = true
		snap.CurrentLine = true
		snap.MatchBrackets = false
		snap.Wrap = true
		snap.ShowCaret = true
		snap.TabWidth = 4
		// 占位：docCodeDraw 在绘制时换成组件自己的滚动条身份（parts.At(0)）。
		snap.ScrollbarID = gfx.Item(1)
		// 角色是 RoleGroup：文本服务由页面级 TextInput 按控件身份回答（见 model.config）。
		m.docHandlers.Role = gfx.RoleGroup
		m.docHandlers.Key = m.docCodeKey
		m.docHandlers.Pointer = m.docCodePointer
		m.docHandlers.Scroll = m.docCodeScroll
		m.docCodeID = ui.Control(c, "文档内容", &m.docHandlers).Fill().Draw(m.docCodeDraw).N().ID
		m.syncDocTextFromCode()
	})
}

// docCodeDraw 把正文与光标交给 components.CodeEditor 画：这一步本来在
// ui/codeeditor 的 wrapper 里做（它从 d.Node 取身份、把滚动条换成 Part 身份）。
func (m *Model) docCodeDraw(d *ui.DrawContext) {
	snap := m.docCode.Snapshot
	if m.docText != nil {
		snap.Value = m.docText.String()
		snap.Caret = m.docText.ByteOffset(m.docText.Caret())
		selection := m.docText.Selection()
		snap.SelectionStart = m.docText.ByteOffset(selection.Start)
		snap.SelectionEnd = m.docText.ByteOffset(selection.End)
		snap.CompositionStart, snap.CompositionEnd = 0, 0
		if composition, ok := m.docText.Composition(); ok {
			snap.CompositionStart = m.docText.ByteOffset(composition.Start)
			snap.CompositionEnd = m.docText.ByteOffset(composition.End)
		}
	}
	// 记下编辑器的实拍矩形：平台文本层要按它裁剪自己那一份文字（见 docCodeInput）。
	m.docCodeRect = d.Rect
	snap.ID = d.Node.ID
	snap.Label = d.Label
	parts := m.docHandlers.Parts()
	if !snap.ScrollbarID.IsZero() {
		snap.ScrollbarID = parts.At(0)
	}
	if !snap.HorizontalScrollbarID.IsZero() {
		snap.HorizontalScrollbarID = parts.At(1)
	}
	m.docCode.Code.Paint(d.Canvas, *d.Theme, d.Rect, snap)
}

// docPreviewPane 是预览栏：卡片外框 + 可滚动正文（v0.6.9 的 ui/card）。
func (m *Model) docPreviewPane(c ui.C, textWidth float32) {
	card.View(c, card.Spec{
		FooterBorder: true,
		// 页脚给一眼规模：行数口径与状态栏的行号一致（只数逻辑行）。
		Footer: func() {
			ui.Text(c, "共 "+strconv.Itoa(lineCount(m.editor.text))+" 行 · "+
				strconv.Itoa(len([]rune(m.editor.text)))+" 字").FontSize(fs(10)).Muted()
		},
	}, func() {
		if strings.TrimSpace(m.editor.text) == "" {
			ui.Text(c, "（空文档）").Muted()
			return
		}
		ui.Scroll(c).Grow(1).Scrollbars().Children(func() {
			m.previewBlock(c, m.editor.text, m.editor.previewKey, docBase(m.editor.file), textWidth)
		})
	}).Grow(1).FillWidth().FillHeight()
}

// previewBlock 把一段 Markdown 排好版画进当前布局（不加滚动容器，由调用方决定）。
// base 是源文本所在目录：图片的相对路径按它解析。
func (m *Model) previewBlock(c ui.C, source, key, base string, width float32) {
	height := m.previewHeight(c, key, source, base, width)
	ui.Slot(c).FillWidth().Height(height).Draw(func(d *ui.DrawContext) {
		m.docs.view.Paint(d.Canvas, components.MarkdownPaint{
			Look: m.docs.look,
			X:    d.Rect.X,
			Y:    d.Rect.Y,
		})
	})
}
