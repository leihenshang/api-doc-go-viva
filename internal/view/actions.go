package view

import (
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
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

// 主色取 Wails 客户端的绿色令牌（frontend/src/styles/base.css）：
// 浅色 --app-accent/#18a058，暗色 --app-accent/#36ad6a，按钮文字白色。
var (
	accentLight = ui.RGB(0x18, 0xa0, 0x58) // #18a058
	accentDark  = ui.RGB(0x36, 0xad, 0x6a) // #36ad6a
	accentText  = ui.RGB(0xff, 0xff, 0xff)
)

// applyTheme 把设置里的主题落到界面与 Markdown 配色上。
func (m *Model) applyTheme() {
	if m.settings.Theme == settings.ThemeLight {
		m.theme = ui.LightTheme()
		m.theme.Primary = accentLight
		m.theme.Selection = ui.RGB(0xe8, 0xf5, 0xee) // --app-accent-tint
		m.theme.SelectionForeground = ui.RGB(0x20, 0x21, 0x24)
	} else {
		m.theme = ui.DarkTheme()
		m.theme.Primary = accentDark
		m.theme.Selection = ui.RGBA(0x36, 0xad, 0x6a, 0.4) // --app-selection
		m.theme.SelectionForeground = ui.RGB(0xfa, 0xfa, 0xfa)
	}
	m.theme.PrimaryForeground = accentText
	m.theme.Ring = m.theme.Primary
	m.pal = paletteFor(m.settings.Theme == settings.ThemeLight)
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

// chooseCollection 打开系统目录选择框；失败（没有可用的对话框后端）就退回手输路径。
func (m *Model) chooseCollection() {
	path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: "打开集合目录"})
	if err != nil {
		m.ui.openErr = err.Error()
		m.ui.pathText = ""
		m.ui.pathOpen = true
		m.invalidator.Invalidate()
		return
	}
	if path == "" {
		return // 用户取消
	}
	m.openCollection(path)
}

// openPathInput 是手输路径弹窗的「打开」：复用 openCollection 的校验与闸门。
func (m *Model) openPathInput() {
	m.openCollection(m.ui.pathText)
}

// openCollection 打开一个集合目录；缺清单时先问用户，确认后再初始化（与客户端同一条闸门）。
func (m *Model) openCollection(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	col, err := explorer.Load(dir)
	if errors.Is(err, explorer.ErrNeedsInit) {
		m.ui.pendingDir = dir
		m.ui.initOpen = true
		m.ui.openErr = ""
		return
	}
	if err != nil {
		m.ui.openErr = err.Error()
		m.ui.pathOpen = true
		m.ui.pathText = dir
		m.invalidator.Invalidate()
		return
	}
	m.ui.openErr = ""
	m.ui.pathOpen = false
	m.ui.initOpen = false
	m.adoptCollection(col)
}

// confirmInit 用户确认后写清单并打开；失败就把错误留在手输路径弹窗里。
func (m *Model) confirmInit() {
	dir := m.ui.pendingDir
	m.ui.initOpen = false
	if err := explorer.InitCollection(dir, ""); err != nil {
		m.ui.openErr = err.Error()
		m.ui.pathOpen = true
		m.ui.pathText = dir
		m.invalidator.Invalidate()
		return
	}
	m.openCollection(dir)
}

// syncRoot 把当前界面状态存回活动根（切走前调用，切换回来能接着用）。
func (m *Model) syncRoot() {
	if m.active < 0 || m.active >= len(m.roots) {
		return
	}
	root := m.roots[m.active]
	root.col = m.col
	root.env = m.envName
	root.filter = m.filter
	root.file = m.entryFile
}

// resetCollectionState 清空所有按「当前集合 / 当前条目」派生的状态。
func (m *Model) resetCollectionState() {
	m.entryFile = ""
	m.pendingSelect = ""
	m.tab = tabRequest
	m.req = requestDraft{}
	m.editor = docEditor{}
	m.drafts = map[string]string{}
	m.docs = docState{look: m.docs.look}
	m.resetResponse()
	m.sending = false
	m.gen++
	m.createErr = ""
	m.shownOnce = false
}

// installRoot 装上一个根：集合、过滤词、环境、选中项都来自它自己的状态。
func (m *Model) installRoot(root *rootState) {
	m.col = root.col
	m.filter = root.filter
	m.envName = root.env
	m.vars = map[string]string{}
	if m.envName != "" {
		if vars, err := explorer.LoadEnv(m.col.Dir, m.envName); err == nil {
			m.vars = vars
		}
	}
	m.resetCollectionState()
	m.pendingSelect = root.file
	m.settings.LastCollection = m.col.Dir
	_ = settings.Save(m.settings)
	m.refreshEnvs()
	m.ui.envPick = m.envName
	m.ui.envErr = ""
	m.refreshShown()
	m.invalidator.Invalidate()
}

// activateRoot 切到第 i 个集合；先存旧的过滤 / 环境 / 选中，再装新的。
func (m *Model) activateRoot(i int) {
	if i < 0 || i >= len(m.roots) || i == m.active {
		return
	}
	m.syncRoot()
	m.active = i
	m.installRoot(m.roots[i])
}

// closeRoot 关掉第 i 个集合（至少留一个）；关的是活动的就切到邻位。
func (m *Model) closeRoot(i int) {
	if i < 0 || i >= len(m.roots) || len(m.roots) <= 1 {
		return
	}
	closingActive := m.active == i
	m.roots = append(m.roots[:i], m.roots[i+1:]...)
	switch {
	case closingActive:
		if i >= len(m.roots) {
			i = len(m.roots) - 1
		}
		m.active = i
		m.installRoot(m.roots[i])
	case m.active > i:
		m.active--
	}
	m.invalidator.Invalidate()
}

// adoptCollection 打开一个集合：已打开过就切过去，否则追加成一个新的根。
func (m *Model) adoptCollection(col *explorer.Collection) {
	for i, root := range m.roots {
		if root.col.Dir == col.Dir {
			m.activateRoot(i)
			return
		}
	}
	m.syncRoot()
	m.roots = append(m.roots, &rootState{col: col, env: m.settings.Envs[col.Dir]})
	m.active = len(m.roots) - 1
	m.installRoot(m.roots[m.active])
}

// renameEnv 重命名当前环境（两个文件一起改），随后切到新名字。
func (m *Model) renameEnv() {
	oldName := m.ui.envPick
	newName := strings.TrimSpace(m.ui.envRenameName)
	if err := explorer.RenameEnv(m.col.Dir, oldName, newName); err != nil {
		m.ui.envErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.envRenameOpen = false
	m.ui.envErr = ""
	m.refreshEnvs()
	m.applyEnv(newName)
	m.invalidator.Invalidate()
}

// deleteEnv 删除当前环境（移进 .trash）；删的是正在用的环境就切回「无」。
func (m *Model) deleteEnv() {
	name := m.ui.envPick
	if err := explorer.DeleteEnv(m.col.Dir, name); err != nil {
		m.ui.envErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.envDeleteOpen = false
	if m.envName == name {
		m.applyEnv("")
	} else {
		m.refreshEnvs()
	}
	m.invalidator.Invalidate()
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
