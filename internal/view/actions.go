package view

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	curlc "github.com/leihenshang/api-doc-go-viva/internal/curl"
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
	splitOptions = []int{0, 1}
	splitNames   = []string{"上下", "左右"}
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
	// 参考图的树：选中行信息蓝浅底、悬停整行浅灰（组件 Tree 用 Accent / Muted 画）。
	m.theme.Accent = m.pal.active
	m.theme.Muted = m.pal.hover
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
	m.installCol(col)
	m.filter = ""
	m.shownOnce = false
	m.createErr = ""
	m.pendingSelect = file
	m.refreshShown()
	m.refreshTabs()
	if tab >= 0 && m.curTab >= 0 {
		m.tab = tab
		m.tabs[m.curTab].tab = tab
	}
	m.invalidator.Invalidate()
}

// reloadCollection 只重扫磁盘、保留过滤词与标签（保存请求后调用）。
func (m *Model) reloadCollection() bool {
	col, err := explorer.Load(m.col.Dir)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return false
	}
	m.installCol(col)
	m.shownOnce = false
	m.refreshShown()
	m.refreshTabs()
	return true
}

// installCol 换上新的集合快照，同时更新当前根：树是从 roots 里读条目的。
func (m *Model) installCol(col *explorer.Collection) {
	m.col = col
	if m.active >= 0 && m.active < len(m.roots) {
		m.roots[m.active].col = col
	}
}

// newRequestFolder 是新建请求要落的目录：树选中条目的所在分组，其次当前标签。
func (m *Model) newRequestFolder() string {
	if row := m.selectedRow(); row != nil && row.kind == nodeEntry {
		return requestDir(row.entry.File)
	}
	if tab := m.activeTab(); tab != nil {
		return requestDir(tab.file)
	}
	return ""
}

// openNewRequest 打开新建请求弹窗；newFolder 为空时落点是当前选中条目所在分组。
func (m *Model) openNewRequest() { m.openNewRequestIn("") }

// openNewRequestIn 在指定分组里新建请求（分组右键菜单）。
func (m *Model) openNewRequestIn(dir string) {
	m.ui.newReqOpen = true
	m.ui.newErr = ""
	m.ui.newName = ""
	m.ui.newURL = ""
	m.ui.newMethod = "GET"
	m.ui.newFolder = dir
}

// createRequest 落盘一条新请求，然后重扫并选中它。
func (m *Model) createRequest() {
	folder := m.ui.newFolder
	if folder == "" {
		folder = m.newRequestFolder()
	}
	file, err := explorer.CreateRequest(m.col.Dir, folder, m.ui.newName, m.ui.newMethod, m.ui.newURL)
	if err != nil {
		m.ui.newErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.newReqOpen = false
	m.ui.newErr = ""
	if m.ui.savingScratch {
		m.ui.savingScratch = false
		m.closeScratchTab()
	}
	m.reload(file, tabParams)
}

// openRenameEntry 打开重命名弹窗（文件名不动，只改显示名）。
// openNewFolderIn 打开「新建分组」弹窗；parent 是父目录（空=集合根）。
func (m *Model) openNewFolderIn(parent string) {
	m.ui.folderOpen = true
	m.ui.folderName = ""
	m.ui.folderParent = parent
	m.ui.folderErr = ""
}

// createFolder 落盘新分组：展开父级、选中新行。
func (m *Model) createFolder() {
	path, err := explorer.CreateFolder(m.col.Dir, m.ui.folderParent, m.ui.folderName)
	if err != nil {
		m.ui.folderErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.folderOpen = false
	m.expandDir(m.ui.folderParent)
	if m.reloadCollection() {
		m.selectFolderDir(path)
	}
	m.invalidator.Invalidate()
}

// openRenameFolder 打开分组重命名：只改 folder.yml 的 info.name。
func (m *Model) openRenameFolder(folder *explorer.Folder) {
	m.ui.renameOpen = true
	m.ui.renameFile = ""
	m.ui.renameFolder = folder.Dir
	m.ui.renameName = folder.Name
	m.ui.renameErr = ""
}

func (m *Model) openRenameEntry(entry *explorer.Entry) {
	if entry == nil {
		return
	}
	m.ui.renameOpen = true
	m.ui.renameErr = ""
	m.ui.renameName = entry.Name
	m.ui.renameFile = entry.File
	m.ui.renameFolder = ""
}

// renameEntry 把显示名写回文件（请求改 info.name，文档改 frontmatter name，分组改 folder.yml）。
func (m *Model) renameEntry() {
	if m.ui.renameFolder != "" {
		m.renameFolder()
		return
	}
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

// renameFolder 把分组显示名写回 folder.yml，目录名不动。
func (m *Model) renameFolder() {
	dir := m.ui.renameFolder
	if err := explorer.RenameFolder(m.col.Dir, &explorer.Folder{Dir: dir}, m.ui.renameName); err != nil {
		m.ui.renameErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.renameOpen = false
	m.ui.renameFolder = ""
	if m.reloadCollection() {
		m.selectFolderDir(dir)
	}
	m.invalidator.Invalidate()
}

// cloneFolder 深拷贝分组，展开父级并选中副本。
func (m *Model) cloneFolder(folder *explorer.Folder) {
	path, err := explorer.CloneFolder(m.col.Dir, folder)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.expandDir(parentDir(folder.Dir))
	if m.reloadCollection() {
		m.selectFolderDir(path)
	}
	m.invalidator.Invalidate()
}

// askDeleteFolder 打开删除确认：整个分组会进回收站。
func (m *Model) askDeleteFolder(folder *explorer.Folder) {
	m.ui.folderDeleteOpen = true
	m.ui.folderDeleteDir = folder.Dir
	m.ui.folderDeleteName = folder.Name
}

// deleteFolder 把分组目录整体移进 .trash；标签随重扫自动丢弃。
func (m *Model) deleteFolder() {
	dir := m.ui.folderDeleteDir
	m.ui.folderDeleteOpen = false
	if err := explorer.TrashFolder(m.col.Dir, &explorer.Folder{Dir: dir, Name: m.ui.folderDeleteName}); err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.tree.Select(-1)
	m.selKey = ""
	if m.reloadCollection() {
		m.pendingSelect = ""
		m.pendingKey = ""
	}
	m.invalidator.Invalidate()
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
	m.closeTabOf(file)
	m.reload("", tabParams)
}

// curlRootOption 是「导入到」下拉里代表集合根的那一项。
const curlRootOption = "(集合根)"

// openCurlImport 打开「导入 cURL」弹窗；folder 是预设的分组（空=集合根）。
func (m *Model) openCurlImport(folder string) {
	m.ui.curlOpen = true
	m.ui.curlText = ""
	m.ui.curlErr = ""
	m.ui.curlName = ""
	m.ui.curlFolder = curlRootOption
	if folder != "" {
		m.ui.curlFolder = folder
	}
	m.ui.curlFolders = m.curlFolderOptions()
	m.invalidator.Invalidate()
}

// curlFolderOptions 列出「导入到」选项：集合根 + 当前集合的分组相对路径。
func (m *Model) curlFolderOptions() []string {
	options := make([]string, 0, len(m.col.Folders)+1)
	options = append(options, curlRootOption)
	dirs := make([]string, 0, len(m.col.Folders))
	for _, folder := range m.col.Folders {
		dirs = append(dirs, folder.Dir)
	}
	sort.Strings(dirs)
	return append(options, dirs...)
}

// importCurl 解析粘贴的 curl 并落盘成一条新请求，随后重扫并打开。
func (m *Model) importCurl() {
	file, err := curlc.Parse(m.ui.curlText)
	if err != nil {
		m.ui.curlErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	name := strings.TrimSpace(file.Info.Name)
	if name == "" {
		name = "导入的请求"
	}
	folder := m.ui.curlFolder
	if folder == curlRootOption {
		folder = ""
	}
	if explicit := strings.TrimSpace(m.ui.curlName); explicit != "" {
		name = explicit
	}
	path, err := explorer.CreateRequest(m.col.Dir, folder, name, file.HTTP.Method, file.HTTP.URL)
	if err != nil {
		m.ui.curlErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	col, err := explorer.Load(m.col.Dir)
	if err != nil {
		m.ui.curlErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	var saved *explorer.Entry
	for _, entry := range col.Entries {
		if entry.File == path {
			saved = entry
			break
		}
	}
	if saved == nil || saved.Payload == nil {
		m.ui.curlErr = "新建的请求读不回来"
		m.invalidator.Invalidate()
		return
	}
	saved.Payload.HTTP.Headers = file.HTTP.Headers
	saved.Payload.HTTP.Body = file.HTTP.Body
	saved.Payload.HTTP.Auth = file.HTTP.Auth
	saved.Payload.Settings = file.Settings
	if err := explorer.SaveRequest(m.col.Dir, path, saved.Payload); err != nil {
		m.ui.curlErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.curlOpen = false
	m.ui.curlErr = ""
	// 导入到折叠的分组后展开它，避免用户看不到新请求。
	m.expandDir(folder)
	m.reload(path, tabParams)
}

// copyText 把一段文本写进剪贴板（标签菜单复制路径用）。
func (m *Model) copyText(text string) {
	_ = app.SetClipboardText(text)
	m.invalidator.Invalidate()
}

// copyCurl 把当前请求导出成 cURL 写进剪贴板。
func (m *Model) copyCurl(entry *explorer.Entry) {
	if entry == nil || entry.Payload == nil {
		return
	}
	if err := app.SetClipboardText(curlc.Generate(entry.Payload)); err != nil {
		m.req.note = "导出失败：" + err.Error()
	} else {
		m.req.note = "cURL 已复制"
	}
	m.invalidator.Invalidate()
}

// openTrash 列出回收站文件并打开弹窗。
func (m *Model) openTrash() {
	items, err := explorer.ListTrash(m.col.Dir)
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.trashRows = items
	m.ui.trashMsg = ""
	m.ui.trashOpen = true
	m.invalidator.Invalidate()
}

// restoreTrash 把回收站文件恢复回集合根，然后重扫并刷新列表。
func (m *Model) restoreTrash(file string) {
	restored, err := explorer.RestoreTrash(m.col.Dir, file)
	if err != nil {
		m.ui.trashMsg = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.trashMsg = "已恢复：" + restored
	m.reloadCollection()
	if entry := m.entryByFile(restored); entry != nil {
		m.openEntry(entry)
	}
	m.openTrash()
	m.ui.trashMsg = "已恢复：" + restored
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
	m.closeAllTabs()
	m.entryFile = ""
	m.pendingSelect = ""
	m.tab = tabParams
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
	m.menuOpen = false
	m.expanded["root:"+root.col.Dir] = true
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

// AddRoot 追加一个已打开的集合根，不切过去：命令行一次开多个集合时用。
func (m *Model) AddRoot(col *explorer.Collection) {
	if col == nil {
		return
	}
	for i, root := range m.roots {
		if root.col.Dir == col.Dir {
			m.activateRoot(i)
			return
		}
	}
	m.syncRoot()
	m.roots = append(m.roots, &rootState{col: col, env: m.settings.Envs[col.Dir]})
	m.treeDirty = true
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
	if len(m.roots) == 1 && m.roots[0].col.Dir == "" {
		m.roots[0] = &rootState{col: col, env: m.settings.Envs[col.Dir]}
		m.active = 0
		m.installRoot(m.roots[0])
		return
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
	m.ui.envOpen = false
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
	draft.form = newKVEditor(block.Body.Form)
	draft.fields = newFieldEditor(block.Body.Fields)
	for _, row := range draft.form {
		if row.typ == "" {
			row.typ = "text"
		}
	}
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
		rows = append(rows, &kvRow{enabled: kv.Enabled, name: kv.Name, value: kv.Value, desc: kv.Description, typ: kv.Type})
	}
	return rows
}

// kvFromEditor 把可编辑行写回落盘结构。
func kvFromEditor(rows []*kvRow) []collection.KV {
	out := make([]collection.KV, 0, len(rows))
	for _, row := range rows {
		out = append(out, collection.KV{
			Name: row.name, Value: row.value, Enabled: row.enabled, Description: row.desc, Type: row.typ,
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
	if m.scratch {
		m.saveScratch()
		return
	}
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
	file.HTTP.Body.Form = kvFromEditor(m.req.form)
	file.HTTP.Body.Fields = fieldsFromEditor(m.req.fields)
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
	if m.reloadCollection() {
		m.reloadActive()
	}
	m.invalidator.Invalidate()
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
	if m.col.Dir == "" {
		m.ui.envs = nil
		return
	}
	names, err := explorer.ListEnvs(m.col.Dir)
	if err != nil {
		m.ui.envErr = err.Error()
		return
	}
	m.ui.envs = names
}

// openEnvEditor 打开环境变量弹窗：默认编辑当前环境。
func (m *Model) openEnvEditor() { m.openEnvEditorFor(m.ui.envPick) }

// openEnvEditorFor 打开某个环境的变量表（含敏感值，敏感值只写 *.secrets.yml）。
func (m *Model) openEnvEditorFor(name string) {
	if name == "" {
		m.ui.envErr = "先选一个环境"
		return
	}
	vars, err := explorer.LoadEnvVars(m.col.Dir, name)
	if err != nil {
		m.ui.envErr = err.Error()
		return
	}
	rows := make([]*kvRow, 0, len(vars))
	for _, v := range vars {
		rows = append(rows, &kvRow{enabled: v.Enabled, name: v.Name, value: v.Value, secret: v.Secret})
	}
	m.ui.envRows = rows
	m.ui.envSel = name
	m.ui.envPick = name
	m.ui.envNameEdit = name
	m.ui.envErr = ""
	m.ui.envOpen = true
}

// saveEnv 保存环境变量：名称改了先重命名，再按敏感标记分别写主文件与密钥文件。
func (m *Model) saveEnv() {
	oldName := m.ui.envPick
	name := strings.TrimSpace(m.ui.envNameEdit)
	if name == "" {
		name = oldName
	}
	if name != oldName {
		if err := explorer.RenameEnv(m.col.Dir, oldName, name); err != nil {
			m.ui.envErr = err.Error()
			m.invalidator.Invalidate()
			return
		}
	}
	vars := make([]collection.EnvVar, 0, len(m.ui.envRows))
	for _, row := range m.ui.envRows {
		if strings.TrimSpace(row.name) == "" {
			continue
		}
		vars = append(vars, collection.EnvVar{
			Name: row.name, Value: row.value, Enabled: row.enabled, Secret: row.secret,
		})
	}
	if err := explorer.SaveEnvAll(m.col.Dir, name, vars); err != nil {
		m.ui.envErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.envOpen = false
	m.ui.envErr = ""
	m.refreshEnvs()
	m.applyEnv(name)
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
	m.openEnvEditorFor(name)
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
	if m.ui.insecure != m.settings.InsecureSSL {
		m.settings.InsecureSSL = m.ui.insecure
		changed = true
	}
	if side := m.ui.splitSide == 1; side != m.settings.SplitSideBySide {
		m.settings.SplitSideBySide = side
		changed = true
	}
	if changed {
		_ = settings.Save(m.settings)
	}
}

// syncSettingsUI 把落盘的偏好装进设置表单（打开面板 / 启动时各一次）。
func (m *Model) syncSettingsUI() {
	m.ui.timeoutText = strconv.Itoa(m.settings.TimeoutSec)
	m.ui.follow = m.settings.FollowRedirects()
	m.ui.insecure = m.settings.InsecureSSL
	m.ui.splitSide = 0
	if m.settings.SplitSideBySide {
		m.ui.splitSide = 1
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
	m.syncSettingsUI()
	m.refreshEnvs()
	m.ui.envPick = m.envName
}
