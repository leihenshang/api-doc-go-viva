package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/ui"
)

// 集合公共参数弹窗：默认请求头 + 默认认证。
//
// 落点是集合清单的 defaults 段（与 Wails 客户端同一处），所以在这儿改完，
// 另一版打开同一个集合也能读到。请求头同名时以请求为准，认证要靠请求选「继承集合」。

// openDefaults 打开弹窗，把磁盘上的 defaults 装进表单。
func (m *Model) openDefaults() {
	if !m.hasCollection() {
		return
	}
	def := explorer.LoadDefaults(m.col.Dir)
	m.ui.defaultsOpen = true
	m.ui.defNote = ""
	m.ui.defHeaders = newKVEditor(def.Headers)
	if len(m.ui.defHeaders) == 0 {
		m.ui.defHeaders = []*kvRow{{enabled: true}}
	}
	m.ui.defAuthType, m.ui.defAuthUser, m.ui.defAuthPass = "none", "", ""
	m.ui.defAuthToken, m.ui.defAuthKey, m.ui.defAuthValue, m.ui.defAuthIn = "", "", "", "header"
	if auth := def.Auth; auth != nil {
		m.ui.defAuthType = auth.Type
		m.ui.defAuthUser, m.ui.defAuthPass = auth.Username, auth.Password
		m.ui.defAuthToken = auth.Token
		m.ui.defAuthKey, m.ui.defAuthValue = auth.Key, auth.Value
		if auth.In != "" {
			m.ui.defAuthIn = auth.In
		}
	}
	m.invalidator.Invalidate()
}

// closeDefaults 关掉弹窗（不保存）。
func (m *Model) closeDefaults() {
	m.ui.defaultsOpen = false
	m.invalidator.Invalidate()
}

// saveDefaults 把表单写回集合清单，并让发送立即用上新的默认值。
func (m *Model) saveDefaults() {
	if !m.hasCollection() {
		return
	}
	def := collection.Defaults{}
	for _, kv := range kvFromEditor(m.ui.defHeaders) {
		if strings.TrimSpace(kv.Name) == "" {
			continue
		}
		def.Headers = append(def.Headers, kv)
	}
	switch m.ui.defAuthType {
	case "basic":
		def.Auth = &collection.Auth{
			Type: "basic", Username: strings.TrimSpace(m.ui.defAuthUser), Password: m.ui.defAuthPass,
		}
	case "bearer":
		def.Auth = &collection.Auth{Type: "bearer", Token: strings.TrimSpace(m.ui.defAuthToken)}
	case "apikey":
		def.Auth = &collection.Auth{
			Type: "apikey", Key: strings.TrimSpace(m.ui.defAuthKey),
			Value: strings.TrimSpace(m.ui.defAuthValue), In: m.ui.defAuthIn,
		}
	}
	if err := explorer.SaveDefaults(m.col.Dir, def); err != nil {
		m.ui.defNote = "保存失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.defaults = def
	m.ui.defaultsOpen = false
	m.ui.exportNote = "已保存集合公共参数"
	m.invalidator.Invalidate()
}

// defaultsDialog 是「公共参数」弹窗。
func (m *Model) defaultsDialog(c ui.C) {
	ui.Modal(c, &m.ui.defaultsOpen, "集合公共参数", ui.ModalSpec{Width: 720, Height: 520}, func() {
		ui.Column(c).FillWidth().Height(px(440)).Gap(px(8)).Children(func() {
			ui.Text(c, "下面的请求头每条请求都会带上；同名时以请求自己的为准。"+
				"认证要生效，请把请求的认证类型选成「继承集合」。").
				FontSize(11).Muted().MaxLines(2)
			ui.Text(c, "默认请求头").FontSize(fs(12)).Bold()
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				for index, row := range m.ui.defHeaders {
					slot := index
					ui.Row(c).FillWidth().Gap(px(8)).PadY(px(2)).Items(ui.Center).Children(func() {
						ui.Checkbox(c, &row.enabled, "").Changed()
						ui.TextField(c, &row.name, "名称", "X-Team").Width(px(200)).Changed()
						defValue := ui.TextField(c, &row.value, "值", "{{team}}").Grow(1)
						m.varTipField(defValue, row.value).Changed()
						if ui.Button(c, "删除头").Icon(IconTrash).Ghost().IconOnly().
							Tooltip("删除这一行").Clicked() {
							c.After(func() {
								m.ui.defHeaders = append(m.ui.defHeaders[:slot], m.ui.defHeaders[slot+1:]...)
								m.invalidator.Invalidate()
							})
						}
					})
				}
			})
			if ui.Button(c, "添加请求头").Icon(IconPlus).Outline().Clicked() {
				c.After(func() {
					m.ui.defHeaders = append(m.ui.defHeaders, &kvRow{enabled: true})
					m.invalidator.Invalidate()
				})
			}
			ui.Text(c, "默认认证").FontSize(fs(12)).Bold()
			ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
				ui.Text(c, "类型").FontSize(fs(11)).Muted()
				ui.Select(c, &m.ui.defAuthType, defaultsAuthTypes, nil).Width(px(140)).Changed()
				ui.Spacer(c)
			})
			m.defaultsAuthFields(c)
			if m.ui.defNote != "" {
				ui.Text(c, m.ui.defNote).FontSize(fs(11)).Color(c.Theme().Danger)
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Outline().Clicked() {
				c.After(m.closeDefaults)
			}
			if ui.Button(c, "保存").Clicked() {
				c.After(m.saveDefaults)
			}
		})
	})
}

// defaultsAuthFields 按类型给出对应的输入框。
func (m *Model) defaultsAuthFields(c ui.C) {
	switch m.ui.defAuthType {
	case "basic":
		ui.Row(c).FillWidth().Gap(px(8)).Children(func() {
			user := ui.TextField(c, &m.ui.defAuthUser, "用户名", "{{username}}").Grow(1)
			m.varTipField(user, m.ui.defAuthUser).Changed()
			// 口令是密文控件，悬停不显示取值。
			ui.TextField(c, &m.ui.defAuthPass, "口令", "{{password}}").Grow(1).Secure().Changed()
		})
	case "bearer":
		token := ui.TextField(c, &m.ui.defAuthToken, "Token", "{{token}}").Grow(1)
		m.varTipField(token, m.ui.defAuthToken).Changed()
	case "apikey":
		ui.Row(c).FillWidth().Gap(px(8)).Items(ui.Center).Children(func() {
			key := ui.TextField(c, &m.ui.defAuthKey, "Key", "X-API-Key").Grow(1)
			m.varTipField(key, m.ui.defAuthKey).Changed()
			value := ui.TextField(c, &m.ui.defAuthValue, "Value", "{{apiKey}}").Grow(1)
			m.varTipField(value, m.ui.defAuthValue).Changed()
			ui.Select(c, &m.ui.defAuthIn, authPlaces, nil).Width(px(110)).Changed()
		})
	default:
		ui.Text(c, "不配置默认认证：请求选「继承集合」时不会带上任何认证信息。").FontSize(fs(11)).Muted()
	}
}

// defaultsAuthTypes 是公共参数里可选的认证类型（没有 inherit：自己继承自己没有意义）。
var defaultsAuthTypes = []string{"none", "basic", "bearer", "apikey"}
