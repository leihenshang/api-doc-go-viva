package view

import (
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/vivaui/viva/ui"
)

// 响应示例的界面部分：读取、切换、保存、删除。
//
// 数据落在 internal/explorer/examples.go；这里只负责「现在看哪一份」——
// m.resp 永远是当前**展示**的那一份（实时或示例），m.live 永远是最近一次真实响应。

// loadExamples 换到一条请求时读它的示例（不存在就是空），并回到实时响应。
func (m *Model) loadExamples(entry *explorer.Entry) {
	m.exRows, m.exSel, m.live = nil, "", send.Outcome{}
	if entry == nil || entry.Kind != explorer.KindHTTP || m.col == nil {
		return
	}
	m.exRows = explorer.Examples(m.col.Dir, entry.File)
}

// reloadExamples 重新扫一次当前请求的示例（保存 / 删除之后）。
func (m *Model) reloadExamples() {
	if m.entryFile == "" || m.col == nil {
		m.exRows = nil
		return
	}
	m.exRows = explorer.Examples(m.col.Dir, m.entryFile)
}

// showLive 切回实时响应：把最近一次真实结果重新铺上去。
func (m *Model) showLive() {
	m.exSel = ""
	if m.live.Status == 0 && m.live.Err == "" && m.live.Body == "" {
		return // 还没发过：保持「尚未发送」的空态
	}
	m.showOutcome(m.live)
}

// showExample 把一份示例铺到响应区（状态行、正文、响应头都跟着换）。
func (m *Model) showExample(item explorer.Example) {
	out := send.Outcome{
		Status:   item.Status,
		Duration: time.Duration(item.DurationMS) * time.Millisecond,
		Size:     item.Size,
		Body:     item.Body,
		URL:      item.URL,
	}
	for _, header := range item.Headers {
		if !header.Enabled || header.Name == "" {
			continue
		}
		out.Headers = append(out.Headers, send.Header{Name: header.Name, Value: header.Value})
	}
	m.exSel = item.File
	m.showOutcome(out)
	m.storeTab()
	m.invalidator.Invalidate()
}

// saveExample 把当前实时响应存成示例；失败只提示，不影响已有响应。
func (m *Model) saveExample() {
	if m.entryFile == "" || m.col == nil || !m.hasResp || m.failText != "" {
		return
	}
	if m.exSel != "" {
		m.copyNote = "先切回实时响应再保存"
		return
	}
	file, err := explorer.SaveExample(m.col.Dir, m.entryFile, explorer.Example{
		Method:      strings.ToUpper(strings.TrimSpace(m.req.method)),
		URL:         m.resp.URL,
		Status:      m.resp.Status,
		DurationMS:  m.resp.Duration.Milliseconds(),
		Size:        m.resp.Size,
		ContentType: headerValue(m.resp.Headers, "Content-Type"),
		Body:        m.resp.Body,
		Headers:     exampleHeaders(m.resp.Headers),
	})
	if err != nil {
		m.copyNote = "示例保存失败：" + err.Error()
	} else {
		m.copyNote = "已存为示例"
		m.reloadExamples()
		for _, item := range m.exRows {
			if item.File == file {
				m.copyNote = "已存为示例：" + item.Name
				break
			}
		}
	}
	m.storeTab()
	m.invalidator.Invalidate()
}

// deleteExample 删掉当前选中的示例，并回到实时响应。
func (m *Model) deleteExample() {
	if m.exSel == "" || m.col == nil {
		return
	}
	if err := explorer.DeleteExample(m.col.Dir, m.exSel); err != nil {
		m.copyNote = "删除示例失败：" + err.Error()
	} else {
		m.exSel = ""
		m.reloadExamples()
		m.showLive()
		m.copyNote = "已删除示例"
	}
	m.storeTab()
	m.invalidator.Invalidate()
}

// exampleTag 是「正在看示例」的小标签，贴在状态码后面。
//
// 它同时是个按钮：点一下回到实时响应。理由很实际——切到示例后，
// 用户第一反应是「怎么回到刚才那份响应」，不该再去翻菜单。
func (m *Model) exampleTag(c ui.C) {
	if m.exSel == "" {
		return
	}
	// 只写「示例」两个字：这一行还要留给状态码与分段控件，名字在菜单里能看见。
	if ui.Button(c, "示例").Ghost().XSmall().Tooltip("正在看已保存的示例，点一下回到实时响应").Clicked() {
		c.After(m.showLive)
	}
}

// exampleHeaders 把响应头转成示例里存的形态（全部按启用写入）。
//
// 跳过 Set-Cookie：示例是落在集合目录、会被 git 提交的纯文本，
// 把会话 cookie 抄进去等于把登录态提交到仓库里。
func exampleHeaders(headers []send.Header) []collection.KV {
	out := make([]collection.KV, 0, len(headers))
	for _, header := range headers {
		if strings.EqualFold(header.Name, "Set-Cookie") || strings.EqualFold(header.Name, "Set-Cookie2") {
			continue
		}
		out = append(out, collection.KV{Name: header.Name, Value: header.Value, Enabled: true})
	}
	return out
}

// headerValue 取某个响应头的值（大小写不敏感）。
func headerValue(headers []send.Header, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

// examplePicker 是响应区右上角的「当前响应」下拉：实时响应 / 已存示例。
//
// 用 ui.Select 而不是自绘菜单：它的展开列表自己管开合与滚动，窗口底部放不下时
// 会自己找位置；自绘 ui.Menu 在响应区这种靠下的锚点上，条目一多就整块画不出来。
func (m *Model) examplePicker(c ui.C) {
	choices := make([]string, 0, len(m.exRows)+1)
	choices = append(choices, "")
	for _, item := range m.exRows {
		choices = append(choices, item.File)
	}
	before := m.exSel
	ui.Select(c, &m.exSel, choices, m.exampleLabel).Shrink(0)
	if m.exSel != before {
		picked := m.exSel
		c.After(func() { m.applyExamplePick(picked) })
	}
}

// exampleLabel 是下拉里一项的显示名：空串代表实时响应，其余用示例名。
func (m *Model) exampleLabel(file string) string {
	if file == "" {
		return "当前响应"
	}
	for _, item := range m.exRows {
		if item.File == file {
			return item.Name
		}
	}
	return "当前响应"
}

// applyExamplePick 切换展示：选实时就把最近一次真实响应铺回来，选示例就铺那份。
func (m *Model) applyExamplePick(file string) {
	if file == "" {
		m.showLive()
		return
	}
	for _, item := range m.exRows {
		if item.File == file {
			m.showExample(item)
			return
		}
	}
	m.showLive()
}

// exampleActions 是「更多操作」里的响应示例动作：存一份、删当前这份、保存正文。
// 切换看哪一份在响应区右上角的下拉里，这里只放动作。
func (m *Model) exampleActions(c ui.C, menu *ui.MenuItems) {
	save := menu.Item("存为示例").Disabled(!m.hasResp || m.failText != "" || m.exSel != "")
	if save.Clicked() {
		c.After(m.saveExample)
	}
	if m.hasResp && menu.Item("保存响应体为文件").Clicked() {
		c.After(m.saveResponseBody)
	}
	if m.exSel != "" && menu.Item("删除这份示例").Destructive().Clicked() {
		c.After(m.deleteExample)
	}
}
