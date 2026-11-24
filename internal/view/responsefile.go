package view

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

// 文件类响应：图片 / PDF / 二进制 / 带 Content-Disposition 的下载。
//
// 这类响应体当文本渲染既看不清也没意义（二进制会被当成乱码），
// 所以给一张卡片：文件名、类型、体积，加一个保存按钮。

// fileResponse 是响应区里那张「这是一份文件」的卡片。
func (m *Model) fileResponse(c ui.C) {
	theme := c.Theme()
	name := m.resp.SuggestedFileName()
	size := m.resp.Size
	ui.Column(c).FillWidth().Gap(px(6)).Pad(px(12)).Radius(theme.Metrics.RadiusControl).
		Background(m.pal.panel).Border(1, m.pal.border).Children(func() {
		ui.Row(c).Gap(px(8)).Items(ui.Center).Children(func() {
			ui.Icon(c, IconImport).Size(px(16), px(16)).Color(m.pal.accent)
			ui.Text(c, name).FontSize(fs(12)).Bold().Ellipsis()
		})
		ui.Text(c, strings.TrimSpace(m.resp.ContentType)+" · "+humanBytes(size)).FontSize(fs(11)).Muted().Mono()
		if m.resp.Truncated {
			ui.Text(c, "正文超过上限，只保存了前 "+humanBytes(m.resp.Size)+"；"+
				"要完整内容请把上限调大后重发。").FontSize(fs(11)).Color(theme.Danger)
		}
		ui.Row(c).Gap(px(8)).Children(func() {
			if ui.Button(c, "保存为文件").Icon(IconImport).Outline().Clicked() {
				c.After(m.saveResponseBody)
			}
		})
	})
}

// saveResponseBody 弹保存对话框，把响应体原样写成文件。
// 截断过的响应只写前 1 MB，并明确提示，不假装存全了。
func (m *Model) saveResponseBody() {
	if !m.hasResp {
		return
	}
	name := m.resp.SuggestedFileName()
	extension := strings.TrimPrefix(filepath.Ext(name), ".")
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            "保存响应正文",
		SuggestedName:    name,
		DefaultExtension: extension,
	})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			m.copyNote = "保存失败：" + err.Error()
			m.invalidator.Invalidate()
		}
		return
	}
	if path == "" {
		return // 用户取消
	}
	if err := m.writeResponseBody(path); err != nil {
		m.copyNote = "保存失败：" + err.Error()
	} else {
		m.copyNote = "已保存 " + filepath.Base(path) + "（" + humanBytes(len(m.resp.Body)) + "）"
	}
	m.storeTab()
	m.invalidator.Invalidate()
}

// writeResponseBody 是保存的实体部分（拆出来便于用例直接调用，绕开文件对话框）。
func (m *Model) writeResponseBody(path string) error {
	return os.WriteFile(path, []byte(m.resp.Body), 0o644)
}

// connInfo 是响应区那行「连接信息」：协议 · TLS · 跳数 · 地址。
// narrow 时只留前两项（地址最长，先舍它）。
func (m *Model) connInfo(narrow bool) string {
	parts := make([]string, 0, 4)
	if m.resp.Proto != "" {
		parts = append(parts, m.resp.Proto)
	}
	if m.resp.TLS != "" {
		parts = append(parts, m.resp.TLS)
	} else if m.resp.Proto != "" {
		parts = append(parts, "明文")
	}
	if !narrow {
		if m.resp.Redirects > 0 {
			parts = append(parts, fmt.Sprintf("%d 跳", m.resp.Redirects))
		}
		if m.resp.Remote != "" {
			parts = append(parts, m.resp.Remote)
		}
	}
	return strings.Join(parts, " · ")
}
