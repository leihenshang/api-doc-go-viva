package view

import (
	"path"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// 编辑器里的下拉选项（包级切片，避免每帧分配）。
var (
	bodyTypes  = []string{"none", "json", "text", "form", "multipart"}
	authTypes  = []string{"none", "inherit", "basic", "bearer", "apikey"}
	authPlaces = []string{"header", "query"}
)

// httpMethods 是新建请求可选的方法（顺序与客户端一致：最常用的在前）。
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// themeOptions / themeNames 是设置面板里的主题选项：0 深色、1 浅色。
var (
	themeOptions = []int{0, 1}
	themeNames   = []string{"深色", "浅色"}
)

// applyTheme 把设置里的主题落到界面与 Markdown 配色上。
func (m *Model) applyTheme() {
	if m.settings.Theme == settings.ThemeLight {
		m.theme = ui.LightTheme()
	} else {
		m.theme = ui.DarkTheme()
	}
	m.docs.look = components.MarkdownLookFor(m.theme)
}

// isLight 报告当前是不是浅色主题。
func (m *Model) isLight() bool { return m.settings.Theme == settings.ThemeLight }

// setTheme 切换并持久化主题；偏好写失败不影响界面（下次启动回默认值）。
func (m *Model) setTheme(light bool) {
	if light {
		m.settings.Theme = settings.ThemeLight
	} else {
		m.settings.Theme = settings.ThemeDark
	}
	m.applyTheme()
	_ = settings.Save(m.settings)
	m.docs.key = "" // Markdown 配色变了，重排
	m.invalidator.Invalidate()
}

// reload 重扫集合并把选中项落到 file（空则交给列表自己收敛）。
func (m *Model) reload(file string, tab detailTab) {
	col, err := explorer.Load(m.col.Dir)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.col = col
	m.filter = ""
	m.shownOnce = false
	m.entryFile = ""
	m.tab = tab
	m.createErr = ""
	m.pendingSelect = file
	m.invalidator.Invalidate()
}

// newRequestFolder 是新建请求要落的目录：选中条目的所在分组；没选中就是根目录。
func (m *Model) newRequestFolder() string {
	entry := m.current()
	if entry == nil {
		return ""
	}
	if dir := path.Dir(entry.File); dir != "." && dir != "" {
		return dir
	}
	return ""
}

// openNewRequest 打开新建请求弹窗（index>=0 时先把该行选中，决定落哪个分组）。
func (m *Model) openNewRequest(index int) {
	if index >= 0 && index < len(m.shown) {
		m.list.Select(index)
	}
	m.ui.newReqOpen = true
	m.ui.newErr = ""
	m.ui.newName = ""
	m.ui.newURL = ""
	m.ui.newMethod = "GET"
}

// createRequest 落盘一条新请求，然后重扫并选中它。
func (m *Model) createRequest() {
	file, err := explorer.CreateRequest(m.col.Dir, m.newRequestFolder(), m.ui.newName, m.ui.newMethod, m.ui.newURL)
	if err != nil {
		m.ui.newErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.newReqOpen = false
	m.ui.newErr = ""
	m.reload(file, tabRequest)
}

// openRename 打开重命名弹窗（文件名不动，只改显示名）。
func (m *Model) openRename(index int) {
	if index < 0 || index >= len(m.shown) {
		return
	}
	entry := m.shown[index]
	m.ui.renameOpen = true
	m.ui.renameErr = ""
	m.ui.renameName = entry.Name
	m.ui.renameFile = entry.File
}

// renameEntry 把显示名写回文件（请求改 info.name，文档改 frontmatter name）。
func (m *Model) renameEntry() {
	entry := m.entryByFile(m.ui.renameFile)
	if entry == nil {
		m.ui.renameOpen = false
		return
	}
	if err := explorer.RenameEntry(m.col.Dir, entry, m.ui.renameName); err != nil {
		m.ui.renameErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.renameOpen = false
	m.reload(entry.File, m.tab)
}

// trashEntry 把条目移进 <集合>/.trash/（客户端同一个回收站），随后重扫。
func (m *Model) trashEntry(file string) {
	entry := m.entryByFile(file)
	if entry == nil {
		return
	}
	if err := explorer.TrashEntry(m.col.Dir, entry); err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.reload("", tabRequest)
}

// loadRequestDraft 把请求文件装进编辑草稿；body 正文保留原来的字段形态。
func (m *Model) loadRequestDraft(entry *explorer.Entry) {
	block := entry.Payload.HTTP
	draft := requestDraft{
		file:      entry.File,
		method:    block.Method,
		url:       block.URL,
		bodyType:  block.Body.Type,
		summary:   entry.Summary,
		docs:      entry.Payload.Docs,
		bodyField: "raw",
	}
	if draft.method == "" {
		draft.method = "GET"
	}
	if draft.bodyType == "" {
		draft.bodyType = "none"
	}
	switch {
	case block.Body.Raw != "":
		draft.bodyRaw = block.Body.Raw
	case block.Body.Data != "":
		draft.bodyRaw, draft.bodyField = block.Body.Data, "data"
	}
	draft.params = newKVEditor(block.Params)
	draft.headers = newKVEditor(block.Headers)
	if auth := block.Auth; auth != nil {
		draft.authType = auth.Type
		draft.authUser, draft.authPass = auth.Username, auth.Password
		draft.authToken = auth.Token
		draft.authKey, draft.authValue, draft.authIn = auth.Key, auth.Value, auth.In
	}
	if draft.authType == "" {
		draft.authType = "none"
	}
	if draft.authIn == "" {
		draft.authIn = "header"
	}
	m.req = draft
}

// newKVEditor 把落盘的行转成可编辑行。
func newKVEditor(items []collection.KV) []*kvRow {
	rows := make([]*kvRow, 0, len(items))
	for _, kv := range items {
		rows = append(rows, &kvRow{enabled: kv.Enabled, name: kv.Name, value: kv.Value, desc: kv.Description})
	}
	return rows
}

// kvFromEditor 把可编辑行写回落盘结构。
func kvFromEditor(rows []*kvRow) []collection.KV {
	out := make([]collection.KV, 0, len(rows))
	for _, row := range rows {
		out = append(out, collection.KV{
			Name: row.name, Value: row.value, Enabled: row.enabled, Description: row.desc,
		})
	}
	return out
}

// authFromDraft 把认证草稿转成落盘结构；none 返回 nil（文件里不写 auth 段）。
func (m *Model) authFromDraft() *collection.Auth {
	switch m.req.authType {
	case "", "none":
		return nil
	}
	return &collection.Auth{
		Type:     m.req.authType,
		Username: m.req.authUser,
		Password: m.req.authPass,
		Token:    m.req.authToken,
		Key:      m.req.authKey,
		Value:    m.req.authValue,
		In:       m.req.authIn,
	}
}

// saveRequest 把草稿写回请求文件：Meta/Settings/未知字段原样保留，只改用户能编辑的部分。
func (m *Model) saveRequest() {
	entry := m.entryByFile(m.req.file)
	if entry == nil || entry.Payload == nil {
		m.req.err = "请求已不存在，请重试"
		m.invalidator.Invalidate()
		return
	}
	file := entry.Payload
	file.HTTP.Method = strings.ToUpper(strings.TrimSpace(m.req.method))
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	file.HTTP.URL = strings.TrimSpace(m.req.url)
	file.HTTP.Params = kvFromEditor(m.req.params)
	file.HTTP.Headers = kvFromEditor(m.req.headers)
	file.HTTP.Body.Type = m.req.bodyType
	if m.req.bodyField == "data" {
		file.HTTP.Body.Data = m.req.bodyRaw
	} else {
		file.HTTP.Body.Raw = m.req.bodyRaw
	}
	file.HTTP.Auth = m.authFromDraft()
	file.Summary = strings.TrimSpace(m.req.summary)
	file.Docs = m.req.docs
	if err := explorer.SaveRequest(m.col.Dir, entry.File, file); err != nil {
		m.req.err = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.req.err = ""
	m.req.dirty = false
	m.reload(entry.File, tabRequest)
}

// applyEnv 切换当前环境：重新读变量、记住到偏好（按集合根记），发送立即生效。
func (m *Model) applyEnv(name string) {
	vars := map[string]string{}
	if name != "" {
		loaded, err := explorer.LoadEnv(m.col.Dir, name)
		if err != nil {
			m.ui.envErr = err.Error()
			return
		}
		vars = loaded
	}
	m.ui.envErr = ""
	m.envName = name
	m.vars = vars
	m.settings.Envs[m.col.Dir] = name
	_ = settings.Save(m.settings)
	m.ui.envPick = name
	m.subtitle = m.countText + " · 环境 " + envLabel(m.envName)
}

// refreshEnvs 重新列出环境（新建/保存后调用）。
func (m *Model) refreshEnvs() {
	names, err := explorer.ListEnvs(m.col.Dir)
	if err != nil {
		m.ui.envErr = err.Error()
		return
	}
	m.ui.envs = names
}

// openEnvEditor 打开「编辑环境变量」：只编辑非密钥变量，密钥由客户端管理。
func (m *Model) openEnvEditor() {
	name := m.ui.envPick
	if name == "" {
		m.ui.envErr = "先选一个环境"
		return
	}
	env, err := explorer.LoadEnvFile(m.col.Dir, name)
	if err != nil {
		m.ui.envErr = err.Error()
		return
	}
	rows := make([]*kvRow, 0, len(env.Vars))
	secrets := 0
	for _, v := range env.Vars {
		if v.Secret {
			secrets++
			continue
		}
		rows = append(rows, &kvRow{enabled: v.Enabled, name: v.Name, value: v.Value})
	}
	m.ui.envRows = rows
	m.ui.envSecretCount = secrets
	m.ui.envSecretLine = ""
	if secrets > 0 {
		m.ui.envSecretLine = "另有 " + strconv.Itoa(secrets) + " 个密钥变量由桌面客户端管理，这里不显示也不修改。"
	}
	m.ui.envErr = ""
	m.ui.envOpen = true
}

// saveEnv 保存环境变量，然后按新值刷新发送用的变量表。
func (m *Model) saveEnv() {
	vars := make([]collection.EnvVar, 0, len(m.ui.envRows))
	for _, row := range m.ui.envRows {
		if strings.TrimSpace(row.name) == "" {
			continue
		}
		vars = append(vars, collection.EnvVar{Name: row.name, Value: row.value, Enabled: row.enabled})
	}
	if err := explorer.SaveEnvVars(m.col.Dir, m.ui.envPick, vars); err != nil {
		m.ui.envErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.envOpen = false
	m.applyEnv(m.ui.envPick)
	m.invalidator.Invalidate()
}

// createEnv 新建一个空环境并切过去。
func (m *Model) createEnv() {
	if err := explorer.CreateEnv(m.col.Dir, m.ui.newEnvName); err != nil {
		m.ui.newEnvErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	name := strings.TrimSpace(m.ui.newEnvName)
	m.ui.newEnvOpen = false
	m.ui.newEnvErr = ""
	m.refreshEnvs()
	m.applyEnv(name)
	m.invalidator.Invalidate()
}

// applySettingsEdits 把设置面板里的输入落到偏好（只在面板开着时调用）。
func (m *Model) applySettingsEdits() {
	changed := false
	if seconds, err := strconv.Atoi(strings.TrimSpace(m.ui.timeoutText)); err == nil {
		seconds = min(max(seconds, 1), settings.MaxTimeoutSec)
		if seconds != m.settings.TimeoutSec {
			m.settings.TimeoutSec = seconds
			changed = true
		}
	}
	if m.ui.follow != m.settings.FollowRedirects() {
		m.settings.NoFollowRedirects = !m.ui.follow
		changed = true
	}
	if changed {
		_ = settings.Save(m.settings)
	}
}

// openSettings 打开设置面板，并把每次打开都要重新探测的状态抓一次。
func (m *Model) openSettings() {
	m.ui.settingsOpen = true
	if m.isLight() {
		m.ui.themePick = 1
	} else {
		m.ui.themePick = 0
	}
	m.ui.imeDetail = ime.Detect().Detail
	m.ui.dirLine = "集合：" + m.col.Dir
	m.ui.envLine = "环境：" + envLabel(m.envName) + " · " + m.countText
	m.ui.collectionName = m.col.Name
	m.ui.timeoutText = strconv.Itoa(m.settings.TimeoutSec)
	m.ui.follow = m.settings.FollowRedirects()
	m.refreshEnvs()
	m.ui.envPick = m.envName
}
