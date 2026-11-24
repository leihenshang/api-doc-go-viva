package view

import (
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// 请求级设置：超时、最大重定向、跟随重定向、跳过 TLS 校验、URL 自动编码。
//
// 这些字段 send 早就读（`file.settings.*`），但没有界面入口——只能用文本编辑器
// 手改 YAML。这里补一个弹窗，三态项用「默认 / 开 / 关」：默认 = 跟随全局设置，
// 落盘时写 nil，不把全局值抄进请求文件（否则改全局就对这些请求失效了）。

// 三态下拉的取值：0 = 默认（不写字段），1 = 开，2 = 关。
const (
	triDefault = iota
	triOn
	triOff
)

// triNames 是三态在两个场景下的文案（跟随 / 跳过各自不同）。
func triNames(on, off string) []string { return []string{"跟随全局", on, off} }

// openReqSettings 打开「请求设置」并回填当前值。
func (m *Model) openReqSettings() {
	entry := m.entryByFile(m.req.file)
	if entry == nil || entry.Payload == nil {
		m.req.note = "临时请求没有请求级设置，先保存到集合"
		m.invalidator.Invalidate()
		return
	}
	var settings collection.RequestSettings
	if entry.Payload.Settings != nil {
		settings = *entry.Payload.Settings
	}
	m.ui.reqSetTimeout = ""
	if settings.TimeoutSec > 0 {
		m.ui.reqSetTimeout = strconv.Itoa(settings.TimeoutSec)
	}
	m.ui.reqSetMaxRedirect = ""
	if settings.MaxRedirects > 0 {
		m.ui.reqSetMaxRedirect = strconv.Itoa(settings.MaxRedirects)
	}
	m.ui.reqSetFollow = triValue(settings.FollowRedirects)
	m.ui.reqSetInsecure = triValue(settings.InsecureSSL)
	m.ui.reqSetEncode = triValue(settings.EncodeURL)
	m.ui.reqSetErr = ""
	m.ui.reqSetOpen = true
	m.invalidator.Invalidate()
}

// triValue 把 *bool 翻成三态（nil = 跟随全局）。
func triValue(flag *bool) int {
	if flag == nil {
		return triDefault
	}
	if *flag {
		return triOn
	}
	return triOff
}

// triFlag 是 triValue 的反向：默认给 nil，别把全局值固化进请求文件。
func triFlag(value int) *bool {
	if value == triDefault {
		return nil
	}
	flag := value == triOn
	return &flag
}

// saveReqSettings 校验并写回请求文件。
func (m *Model) saveReqSettings() {
	entry := m.entryByFile(m.req.file)
	if entry == nil || entry.Payload == nil {
		m.ui.reqSetErr = "请求已不存在"
		m.invalidator.Invalidate()
		return
	}
	timeout, err := optionalSeconds(m.ui.reqSetTimeout, 1, 600)
	if err != "" {
		m.ui.reqSetErr = "超时：" + err
		m.invalidator.Invalidate()
		return
	}
	redirects, err := optionalSeconds(m.ui.reqSetMaxRedirect, 1, 50)
	if err != "" {
		m.ui.reqSetErr = "最大重定向：" + err
		m.invalidator.Invalidate()
		return
	}

	settings := collection.RequestSettings{
		TimeoutSec:      timeout,
		MaxRedirects:    redirects,
		FollowRedirects: triFlag(m.ui.reqSetFollow),
		InsecureSSL:     triFlag(m.ui.reqSetInsecure),
		EncodeURL:       triFlag(m.ui.reqSetEncode),
	}
	file := entry.Payload
	if settings == (collection.RequestSettings{}) {
		file.Settings = nil // 全默认：把整段删掉，别在文件里留空壳
	} else {
		file.Settings = &settings
	}
	if err := explorer.SaveRequest(m.col.Dir, entry.File, file); err != nil {
		m.ui.reqSetErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.reqSetErr = ""
	m.ui.reqSetNote = "已保存请求设置"
	m.ui.reqSetOpen = false
	if m.reloadCollection() {
		m.reloadActive()
	}
	m.invalidator.Invalidate()
}

// optionalSeconds 解析「留空 = 不设置」的整数输入，并检查范围。
func optionalSeconds(text string, low, high int) (int, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, ""
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, "不是整数"
	}
	if value < low || value > high {
		return 0, "应在 " + strconv.Itoa(low) + "~" + strconv.Itoa(high) + " 之间"
	}
	return value, ""
}

// reqSettingsDialog 是「请求设置」弹窗。
func (m *Model) reqSettingsDialog(c ui.C) {
	ui.ModalWith(c, &m.ui.reqSetOpen, "请求设置", ui.ModalSpec{Width: 520, Height: 460}, func() {
		ui.Column(c).FillWidth().Height(px(340)).Gap(px(10)).Children(func() {
			ui.Text(c, "这些值只作用于当前请求，留空或「跟随全局」就不写进文件——"+
				"以后改设置面板里的全局值仍然生效。").FontSize(fs(11)).Muted()

			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "超时（秒）")
				ui.TextField(c, &m.ui.reqSetTimeout, "超时", "留空跟随全局").Width(px(120))
				ui.Text(c, "1~600").FontSize(fs(11)).Muted()
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "最大重定向")
				ui.TextField(c, &m.ui.reqSetMaxRedirect, "最大重定向", "留空用默认 10").Width(px(120))
				ui.Text(c, "1~50").FontSize(fs(11)).Muted()
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "跟随重定向")
				ui.Choice(c, &m.ui.reqSetFollow, []int{triDefault, triOn, triOff},
					func(value int) string { return triNames("跟随", "不跟随")[value] })
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "TLS 校验")
				ui.Choice(c, &m.ui.reqSetInsecure, []int{triDefault, triOn, triOff},
					func(value int) string { return triNames("跳过校验", "正常校验")[value] })
			})
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				formLabel(c, "URL 编码")
				ui.Choice(c, &m.ui.reqSetEncode, []int{triDefault, triOn, triOff},
					func(value int) string { return triNames("自动编码", "原样发送")[value] })
			})
			if m.ui.reqSetErr != "" {
				ui.Text(c, m.ui.reqSetErr).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Outline().Clicked() {
				m.ui.reqSetOpen = false
			}
			if ui.Button(c, "保存").Clicked() {
				c.After(m.saveReqSettings)
			}
		})
	})
}
