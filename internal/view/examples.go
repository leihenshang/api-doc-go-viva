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
	m.exMenuOpen = false
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

// exampleItems 把「响应示例」那一组挂进地址栏的「更多操作」菜单。
//
// 为什么不放响应工具条：那一行本来就满了（状态 / 分段 / 格式化 / 复制），
// 再多一个按钮在窄栏下会把「原始」顶出去——T1 修的就是这个问题。
func (m *Model) exampleItems(c ui.C, menu *ui.MenuItems) {
	menu.Label("响应示例")
	if menu.Item("实时响应").Checked(m.exSel == "").Clicked() {
		c.After(m.showLive)
	}
	for _, item := range m.exRows {
		row := item
		if menu.Item(row.Name).Checked(m.exSel == row.File).Clicked() {
			c.After(func() { m.showExample(row) })
		}
	}
	menu.Separator()
	save := menu.Item("存为示例").Disabled(!m.hasResp || m.failText != "" || m.exSel != "")
	if save.Clicked() {
		c.After(m.saveExample)
	}
	if m.exSel != "" && menu.Item("删除这份示例").Destructive().Clicked() {
		c.After(m.deleteExample)
	}
}

// exampleTag 是「正在看示例」的小标签，贴在状态码后面。
func (m *Model) exampleTag(c ui.C) {
	if m.exSel == "" {
		return
	}
	// 只写「示例」：这一行还要留给状态码与分段控件，名字在菜单里已经能看见。
	ui.Text(c, "示例").FontSize(10).Mono().Shrink(0).
		Color(m.theme.MutedForeground)
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
