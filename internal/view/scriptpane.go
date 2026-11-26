package view

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/ui"
)

// 请求页的四段「脚本侧」内容：变量 / 脚本 / 断言 / 响应字段。
//
// 布局与 Wails 版对齐（都是「一行说明 + 若干行 + 底部添加按钮」），
// 但按钮语义更直接：Wails 的「生成数据表格」二选一弹窗在这里拆成「生成追加 / 生成覆盖」两个按钮。

// varsTab 是「变量」分段：请求级临时变量，发送时覆盖环境同名值。
func (m *Model) varsTab(c ui.C) {
	ui.Text(c, "请求级变量：只在这次请求生效，发送时覆盖环境同名值；地址、参数、请求头里写 {{名字}} 即可引用。").
		FontSize(fs(11)).Muted().MaxLines(2)
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Slot(c).Width(px(24))
		ui.Text(c, "名称").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "值").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range m.req.vars {
		slot := index
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.Checkbox(c, &row.enabled, "").Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.name, "名称", "traceId").Grow(2).MinWidth(0).Changed() {
				m.markEdited()
			}
			value := ui.TextField(c, &row.value, "值", "{{$uuid}}").Grow(3).MinWidth(0)
			if m.varTipField(value, row.value).Changed() {
				m.markEdited()
			}
			if ui.Button(c, "删除变量").Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					m.req.vars = append(m.req.vars[:slot], m.req.vars[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加变量").Icon(IconPlus).Ghost().Clicked() {
			m.req.vars = append(m.req.vars, &varRow{enabled: true})
			m.markEdited()
		}
	})
}

// scriptTab 是「脚本」分段：发送前后各一段 JS，用 bru.* 读写变量。
func (m *Model) scriptTab(c ui.C) {
	ui.Text(c, "脚本用 JavaScript 编写：bru.setVar / bru.getVar 读写变量，后置脚本里可读 res.status / res.body。").
		FontSize(fs(11)).Muted().MaxLines(2)
	ui.Text(c, "发送前").FontSize(fs(11)).Bold()
	pre := ui.TextArea(c, &m.req.scriptPre, "发送前脚本", "bru.setVar('ts', Date.now())").
		FillWidth().Height(px(110))
	if pre.Changed() {
		m.markEdited()
	}
	ui.Text(c, "收到响应后").FontSize(fs(11)).Bold()
	post := ui.TextArea(c, &m.req.scriptPost, "收到响应后脚本", "bru.setVar('id', res.body.id)").
		FillWidth().Height(px(110))
	if post.Changed() {
		m.markEdited()
	}
	ui.Text(c, "脚本错误不会中断发送，会在响应区提示；单段脚本最长执行 5 秒。").
		FontSize(fs(11)).Muted()
}

// assertsTab 是「断言」分段：每条是一个 JS 布尔表达式，发送后逐条判定。
func (m *Model) assertsTab(c ui.C) {
	ui.Text(c, "断言在收到响应后执行，表达式写 JS：可读 res.status / res.body / res.headers / res.text / res.responseTime。").
		FontSize(fs(11)).Muted().MaxLines(2)
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "说明").FontSize(fs(11)).Muted().Grow(2).MinWidth(0).Ellipsis()
		ui.Text(c, "表达式").FontSize(fs(11)).Muted().Grow(3).MinWidth(0).Ellipsis()
		ui.Slot(c).Width(rowActionWidth)
	})
	for index, row := range m.req.asserts {
		slot := index
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			if ui.TextField(c, &row.name, "说明", "状态码为 200").Grow(2).MinWidth(0).Changed() {
				m.markEdited()
			}
			if ui.TextField(c, &row.expr, "表达式", "res.status === 200").Grow(3).MinWidth(0).Changed() {
				m.markEdited()
			}
			if ui.Button(c, "删除断言").Icon(IconTrash).Outline().IconOnly().Tooltip("删除这一行").Clicked() {
				c.After(func() {
					m.req.asserts = append(m.req.asserts[:slot], m.req.asserts[slot+1:]...)
					m.markEdited()
				})
			}
		})
	}
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		if ui.Button(c, "添加断言").Icon(IconPlus).Ghost().Clicked() {
			m.req.asserts = append(m.req.asserts, &assertRow{})
			m.markEdited()
		}
		// 最常见的两条断言一键填好，省得每次手敲表达式。
		if ui.Button(c, "填充示例断言").Ghost().Tooltip("状态码为 2xx 且内容和类型是 JSON").
			Clicked() {
			c.After(m.addExampleAsserts)
		}
	})
}

// addExampleAsserts 追加两条开箱可用的断言（已有的同名断言不重复添加）。
func (m *Model) addExampleAsserts() {
	want := []*assertRow{
		{name: "状态码为 2xx", expr: "res.status >= 200 && res.status < 300"},
		{name: "内容是 JSON", expr: `String(res.headers['Content-Type'] ?? '').includes('json')`},
	}
	for _, row := range want {
		exists := false
		for _, has := range m.req.asserts {
			if has != nil && strings.EqualFold(strings.TrimSpace(has.name), row.name) {
				exists = true
				break
			}
		}
		if !exists {
			m.req.asserts = append(m.req.asserts, row)
		}
	}
	m.markEdited()
	m.copyNote = "已补充示例断言（可自行改表达式）"
	m.invalidator.Invalidate()
}

// respFieldsTab 是「响应字段」分段：这张接口返回什么 —— 字段表 + 响应数据样例。
//
// 与请求体字段表分开存（顶层 respdoc），因为它描述的是返回值，不是要发出去的内容。
func (m *Model) respFieldsTab(c ui.C) {
	rows := &m.req.resp.fields
	ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
		ui.Text(c, "响应字段").FontSize(fs(12)).Bold()
		ui.Text(c, strconv.Itoa(len(*rows))).FontSize(fs(11)).Muted()
		ui.Spacer(c)
		if ui.Button(c, "从当前响应生成").Icon(IconImport).Outline().
			Tooltip("按当前响应推字段名与类型（追加到表尾）").
			Disabled(!m.hasResp).Clicked() {
			c.After(func() { m.generateRespFields(false) })
		}
		if len(*rows) > 0 {
			if ui.Button(c, "覆盖生成").Outline().
				Tooltip("按当前响应重建字段表：同名字段只保留说明").
				Disabled(!m.hasResp).Clicked() {
				c.After(func() { m.generateRespFields(true) })
			}
			if ui.Button(c, "清空").Icon(IconTrash).Outline().Clicked() {
				c.After(m.clearRespFields)
			}
		}
	})
	ui.Text(c, "字段表描述这条接口的返回结构，随集合文件保存；同名字段的说明在重新生成时会保留。").
		FontSize(fs(11)).Muted().MaxLines(2)
	m.fieldTable(c, rows, "字段")
	ui.Row(c).FillWidth().Children(func() {
		if ui.Button(c, "添加字段").Icon(IconPlus).Ghost().Clicked() {
			*rows = append(*rows, &fieldRow{required: true})
			m.markEdited()
		}
	})
	if snap := m.respSnapNote(); snap != "" {
		ui.Text(c, snap).FontSize(fs(10)).Muted().MaxLines(1).Ellipsis()
	}
	ui.Text(c, "响应数据").FontSize(fs(11)).Bold()
	sample := ui.TextArea(c, &m.req.resp.sample, "响应数据", `{"id": 1}`).
		FillWidth().Height(px(120))
	if sample.Changed() {
		m.markEdited()
	}
}

// respSnapNote 是生成字段表时留下的响应快照（状态码 / 内容类型 / 响应头条数）。
func (m *Model) respSnapNote() string {
	doc := m.req.resp
	if doc.status == 0 && doc.ctype == "" && len(doc.headers) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	if doc.status != 0 {
		parts = append(parts, "状态码 "+strconv.Itoa(doc.status))
	}
	if doc.ctype != "" {
		parts = append(parts, "内容类型 "+doc.ctype)
	}
	if len(doc.headers) > 0 {
		parts = append(parts, "响应头 "+strconv.Itoa(len(doc.headers))+" 项")
	}
	return "生成时的响应快照：" + strings.Join(parts, " · ")
}
