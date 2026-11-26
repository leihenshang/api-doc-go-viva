package view

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/codegen"
	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/bundle"
	curlc "github.com/leihenshang/api-doc-go-viva/internal/curl"
	"github.com/leihenshang/api-doc-go-viva/internal/drafts"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/history"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	"github.com/leihenshang/api-doc-go-viva/internal/importers"
	"github.com/leihenshang/api-doc-go-viva/internal/secrets"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/leihenshang/api-doc-go-viva/internal/updater"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/validate"
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
	docOptions   = []int{0, 1}
	docNames     = []string{"预览", "编辑"}
	// 界面缩放：百分数，100 是设计稿大小。
	scaleOptions = []int{80, 90, 100, 110, 125}
	scaleNames   = []string{"80%", "90%", "100%", "110%", "125%"}
)

// scaleIndex 把百分比换算成 scaleOptions 的下标（未知值取最接近的一档）。
func scaleIndex(pct int) int {
	best := 2
	for i, v := range scaleOptions {
		if v == pct {
			return i
		}
		if absInt(v-pct) < absInt(scaleOptions[best]-pct) {
			best = i
		}
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// scaleLabel 给选项值（百分比）配显示名；夹过范围的取值也能显示。
func scaleLabel(pct int) string {
	if i := scaleIndex(pct); scaleOptions[i] == pct {
		return scaleNames[i]
	}
	return strconv.Itoa(pct) + "%"
}

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
		// 与 Wails 版（--app-text: #202124）对齐：默认前景接近纯黑，看着又粗又黑。
		m.theme.Foreground = ui.RGB(0x20, 0x21, 0x24)
		m.theme.CardForeground = m.theme.Foreground
	} else {
		m.theme = ui.DarkTheme()
		m.theme.Primary = accentDark
		m.theme.Selection = ui.RGBA(0x36, 0xad, 0x6a, 0.4) // --app-selection
		m.theme.SelectionForeground = ui.RGB(0xfa, 0xfa, 0xfa)
		// 同上：深色也用 Wails 的 #e6e7e9，比纯白柔和。
		m.theme.Foreground = ui.RGB(0xe6, 0xe7, 0xe9)
		m.theme.CardForeground = m.theme.Foreground
	}
	m.theme.Metrics = scaleMetrics(m.theme.Metrics)
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

// setUIScale 换界面缩放：重算布局常量与主题度量，按比例搬三处分隔条，落盘。
//
// 分隔条位置是像素值，不跟着乘就会在放大后挤成一条缝（或把右栏撑出去）。
func (m *Model) setUIScale(pct int) {
	pct = scaleOptions[scaleIndex(pct)]
	old := uiScale
	applyUIScale(pct)
	if old > 0 && uiScale != old {
		k := uiScale / old
		m.split.Pos *= k
		m.rsplit.Pos *= k
		m.hsplit.Pos *= k
	}
	m.settings.UIScale = pct
	m.ui.scalePick = pct
	m.tree.RowHeight = px(30)
	m.tree.Indent = px(16)
	m.applyTheme()
	m.docs.key = "" // 字号变了，Markdown 要重排
	m.shownOnce = false
	m.treeDirty = true
	_ = settings.Save(m.settings)
	m.invalidator.Invalidate()
}

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
	m.refreshMock() // 集合变了，桩路由跟着重建
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
	// 每次打开都清掉上一次的校验痕迹（错误行跟着弹窗一起消失）。
	m.ui.newNameField = ui.FormFieldState{}
	m.invalidator.Invalidate()
}

// createRequest 落盘一条新请求，然后重扫并选中它。
func (m *Model) createRequest() {
	// 兜底：任何入口进到落盘前都要过一遍名称校验（按钮那条已经拦了一次）。
	if !validate.Apply(&m.ui.newNameField, m.ui.newName, m.ui.newNameRules) {
		m.invalidator.Invalidate()
		return
	}
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
		// 临时请求的变量 / 脚本 / 断言 / 响应字段一起写进新文件，别让另存为丢配置。
		if err := explorer.SaveRequestExtras(m.col.Dir, file, m.extrasFromDraft()); err != nil {
			m.ui.newErr = err.Error()
		}
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
	_ = ui.SetClipboardText(text)
	m.invalidator.Invalidate()
}

// copyCurl 把当前请求导出成 cURL 写进剪贴板。
func (m *Model) copyCurl(entry *explorer.Entry) {
	if entry == nil || entry.Payload == nil {
		return
	}
	if err := ui.SetClipboardText(curlc.Generate(entry.Payload)); err != nil {
		m.req.note = "导出失败：" + err.Error()
	} else {
		m.req.note = "cURL 已复制"
	}
	m.invalidator.Invalidate()
}

// exportContent 生成整份导出（全选），供无头测试与「一键导出」使用。
func (m *Model) exportContent(format string) (string, string, app.FileFilter) {
	return m.exportContentFor(format, nil)
}

// exportCollection 把当前集合导出一份文件：format 是 "html" 或 "md"。
// 生成与保存都在界面线程做——导出是一次性的，产物大小也就几百 KB。
func (m *Model) exportCollection(format string) {
	if m.col == nil || m.col.Dir == "" {
		return
	}
	content, ext, filter := m.exportContent(format)
	name := m.col.Name
	if strings.TrimSpace(name) == "" {
		name = "collection"
	}
	path, err := app.SaveFile(app.SaveFileOptions{
		Title:            "导出集合",
		SuggestedName:    name + "." + ext,
		DefaultExtension: ext,
		Filters:          []app.FileFilter{filter},
	})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			m.ui.exportNote = "导出失败：" + err.Error()
			m.invalidator.Invalidate()
		}
		return
	}
	if path == "" {
		return // 用户取消
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.ui.exportNote = "导出失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.exportNote = "已导出 " + filepath.Base(path) + "（" + humanBytes(len(content)) + "）"
	m.invalidator.Invalidate()
}

// openImport 打开「导入」弹窗：0 = Postman 集合 JSON，1 = 另一个 OpenCollection 集合。
func (m *Model) openImport(mode int) {
	m.ui.importOpen = true
	m.ui.importMode = mode
	m.ui.importText = ""
	m.ui.importDir = ""
	m.ui.importNote = ""
	m.ui.importErr = ""
	m.ui.importFolder = curlRootOption
	m.ui.importFolders = m.curlFolderOptions()
	m.ui.importBase = "{{host}}"
	m.invalidator.Invalidate()
}

// pickImportFile 选一个 Postman 导出的集合 JSON 读进文本框。
func (m *Model) pickImportFile() {
	title := "选择 Postman 集合 JSON"
	filters := []app.FileFilter{{Name: "集合 JSON", Pattern: "*.json"}}
	if m.ui.importMode == 2 {
		// OpenAPI 既可能是 JSON 也可能是 YAML：过滤器要一起给，否则选 yaml 根本看不见。
		title = "选择 OpenAPI 文档（JSON / YAML）"
		filters = []app.FileFilter{
			{Name: "OpenAPI 文档", Pattern: "*.json;*.yaml;*.yml"},
			{Name: "所有文件", Pattern: "*"},
		}
	} else if m.ui.importMode == 5 {
		title = "选择 Insomnia 导出文件（insomnia.json）"
	}
	files, err := app.OpenFiles(app.OpenFileOptions{Title: title, Filters: filters})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			m.ui.importErr = err.Error()
		}
		return
	}
	if len(files) == 0 {
		return
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		m.ui.importErr = "读文件: " + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.importText = string(data)
	m.ui.importErr = ""
	m.invalidator.Invalidate()
}

// pickImportDir 选一个要被导入的 OpenCollection 集合目录。
func (m *Model) pickImportDir() {
	// 有已打开的集合就从它的上一级开始找：导入的通常是与它并排的另一个目录。
	start := ""
	if m.col != nil {
		start = filepath.Dir(m.col.Dir)
	}
	path, err := app.OpenDirectory(app.OpenDirectoryOptions{
		Title: "选择要导入的集合目录", InitialDirectory: start,
	})
	if err != nil {
		if !errors.Is(err, app.ErrCanceled) {
			m.ui.importErr = err.Error()
			m.invalidator.Invalidate()
		}
		return
	}
	if path == "" {
		return
	}
	m.ui.importDir = path
	m.ui.importErr = ""
	m.invalidator.Invalidate()
}

// runImport 按当前模式导入；成功就重扫并把结果留在弹窗里，失败只改提示。
func (m *Model) runImport() {
	folder := m.ui.importFolder
	if folder == curlRootOption {
		folder = ""
	}
	var (
		summary importers.Summary
		err     error
	)
	if m.ui.importMode == 3 {
		text := strings.TrimSpace(m.ui.importText)
		if text == "" {
			m.ui.importErr = "把集合包 JSON 粘进来，或选一个文件"
			m.invalidator.Invalidate()
			return
		}
		pack, parseErr := bundle.Decode([]byte(text))
		if parseErr != nil {
			m.ui.importErr = parseErr.Error()
			m.invalidator.Invalidate()
			return
		}
		written, renamed, writeErr := bundle.Write(m.col.Dir, pack)
		if writeErr != nil {
			m.ui.importErr = writeErr.Error()
			m.invalidator.Invalidate()
			return
		}
		summary.Requests, summary.Docs = written, renamed
		summary.Notes = append(summary.Notes, "集合包：写入 "+strconv.Itoa(written)+" 个文件")
	} else if m.ui.importMode == 2 {
		text := strings.TrimSpace(m.ui.importText)
		if text == "" {
			m.ui.importErr = "把 OpenAPI 的 JSON / YAML 粘进来，或选一个文件"
			m.invalidator.Invalidate()
			return
		}
		items, parsed, parseErr := importers.OpenAPI([]byte(text), m.ui.importBase)
		if parseErr != nil {
			m.ui.importErr = parseErr.Error()
			m.invalidator.Invalidate()
			return
		}
		summary, err = importers.Write(m.col.Dir, folder, items)
		summary.Notes = append(parsed.Notes, summary.Notes...)
	} else if m.ui.importMode == 4 {
		dir := strings.TrimSpace(m.ui.importDir)
		if dir == "" {
			m.ui.importErr = "先选一个 Bruno 集合目录"
			m.invalidator.Invalidate()
			return
		}
		items, parsed, parseErr := importers.Bruno(dir, folder)
		if parseErr != nil {
			m.ui.importErr = parseErr.Error()
			m.invalidator.Invalidate()
			return
		}
		written, writeErr := importers.Write(m.col.Dir, folder, items)
		if writeErr != nil {
			m.ui.importErr = writeErr.Error()
			m.invalidator.Invalidate()
			return
		}
		written.Notes = append(parsed.Notes, written.Notes...)
		summary = written
	} else if m.ui.importMode == 1 {
		dir := strings.TrimSpace(m.ui.importDir)
		if dir == "" {
			m.ui.importErr = "先选一个要导入的集合目录"
			m.invalidator.Invalidate()
			return
		}
		summary, err = importers.OpenCollection(m.col.Dir, folder, dir)
	} else if m.ui.importMode == 5 {
		text := strings.TrimSpace(m.ui.importText)
		if text == "" {
			m.ui.importErr = "把 Insomnia 导出的 insomnia.json 粘进来，或选一个文件"
			m.invalidator.Invalidate()
			return
		}
		items, parsed, parseErr := importers.Insomnia([]byte(text))
		if parseErr != nil {
			m.ui.importErr = parseErr.Error()
			m.invalidator.Invalidate()
			return
		}
		summary, err = importers.Write(m.col.Dir, folder, items)
		summary.Notes = append(parsed.Notes, summary.Notes...)
		summary.Vars = parsed.Vars
	} else {
		text := strings.TrimSpace(m.ui.importText)
		if text == "" {
			m.ui.importErr = "把 Postman 导出的集合 JSON 粘进来，或选一个文件"
			m.invalidator.Invalidate()
			return
		}
		items, parsed, parseErr := importers.Postman([]byte(text))
		if parseErr != nil {
			m.ui.importErr = parseErr.Error()
			m.invalidator.Invalidate()
			return
		}
		summary, err = importers.Write(m.col.Dir, folder, items)
		summary.Notes = append(parsed.Notes, summary.Notes...)
		summary.Vars = parsed.Vars
	}
	if err != nil {
		m.ui.importErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.importErr = ""
	m.ui.importNote = fmt.Sprintf("已导入 %d 个分组、%d 条请求、%d 篇文档（同名自动加 -2）",
		summary.Folders, summary.Requests, summary.Docs)
	if len(summary.Vars) > 0 {
		m.ui.importNote += fmt.Sprintf("；集合变量 %d 个没有自动建环境，可在「环境设置」里补", len(summary.Vars))
	}
	if len(summary.Notes) > 0 {
		m.ui.importNote += "。" + strings.Join(summary.Notes, "；")
	}
	m.expandDir(folder)
	m.reload("", tabParams)
}

// openCookies 打开 Cookie Jar 面板：从共享 jar 取一次快照。
func (m *Model) openCookies() {
	m.ui.cookieOpen = true
	m.ui.cookieNote = ""
	m.reloadCookies()
	m.invalidator.Invalidate()
}

// reloadCookies 把 jar 里的 cookie 抄进界面缓存（按域名、名称排序）。
func (m *Model) reloadCookies() {
	m.ui.cookieRows = send.Cookies()
}

// deleteCookie 删单条：同域名下其它 cookie 不受影响。
func (m *Model) deleteCookie(item send.Cookie) {
	if err := send.DeleteCookie(item.Domain, item.Name); err != nil {
		m.ui.cookieNote = "删除失败：" + err.Error()
	} else {
		m.ui.cookieNote = "已删除 " + item.Name
	}
	m.reloadCookies()
	m.invalidator.Invalidate()
}

// clearCookies 清空共享 jar：之后的请求不再自动带 cookie。
func (m *Model) clearCookies() {
	send.ClearCookies()
	m.ui.cookieRows = nil
	m.ui.cookieNote = "已清空 Cookie Jar"
	m.invalidator.Invalidate()
}

// openHistory 打开请求历史：每次打开都从磁盘重读，发送过的请求才有记录。
func (m *Model) openHistory() {
	m.ui.historyOpen = true
	m.ui.historyQuery = ""
	m.ui.historyNote = ""
	m.reloadHistory()
	m.invalidator.Invalidate()
}

// reloadHistory 把历史读进界面缓存（最新的在前）。
func (m *Model) reloadHistory() { m.ui.historyRows = history.Load() }

// clearHistory 清空历史文件与缓存。
func (m *Model) clearHistory() {
	if err := history.Clear(); err != nil {
		m.ui.historyNote = "清空失败：" + err.Error()
	} else {
		m.ui.historyRows = nil
		m.ui.historyNote = "已清空"
	}
	m.invalidator.Invalidate()
}

// reopenHistory 点一条历史：请求还在当前集合里就打开它；不在就把地址复制出去。
func (m *Model) reopenHistory(item history.Entry) {
	if item.File != "" && m.col != nil && item.Dir == m.col.Dir {
		if entry := m.entryByFile(item.File); entry != nil && entry.Kind == explorer.KindHTTP {
			m.openEntry(entry)
			m.ui.historyOpen = false
			m.invalidator.Invalidate()
			return
		}
	}
	m.ui.historyNote = "这条记录对应的请求不在当前集合里，地址已复制到剪贴板"
	m.copyValue(item.URL)
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
	// 勾选按文件重建：文件已经不在了就不再算勾着。
	sel := make(map[string]bool, len(items))
	for _, item := range items {
		if m.ui.trashSel[item.File] {
			sel[item.File] = true
		}
	}
	m.ui.trashSel = sel
	m.ui.trashArmed = false
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

// selectedTrash 是回收站里勾中的文件（没有勾选返回空）。
func (m *Model) selectedTrash() []string {
	files := make([]string, 0, len(m.ui.trashSel))
	for _, item := range m.ui.trashRows {
		if m.ui.trashSel[item.File] {
			files = append(files, item.File)
		}
	}
	return files
}

// restoreSelectedTrash 批量恢复：逐个回集合根，最后统一重扫并刷新弹窗。
func (m *Model) restoreSelectedTrash() {
	files := m.selectedTrash()
	if len(files) == 0 {
		m.ui.trashMsg = "先在左边勾选文件"
		m.invalidator.Invalidate()
		return
	}
	restored := 0
	var firstErr string
	for _, file := range files {
		if _, err := explorer.RestoreTrash(m.col.Dir, file); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		restored++
	}
	m.reloadCollection()
	m.openTrash()
	switch {
	case firstErr != "" && restored == 0:
		m.ui.trashMsg = firstErr
	case firstErr != "":
		m.ui.trashMsg = "恢复了 " + strconv.Itoa(restored) + " 个，另有失败：" + firstErr
	default:
		m.ui.trashMsg = "恢复了 " + strconv.Itoa(restored) + " 个"
	}
	m.invalidator.Invalidate()
}

// purgeSelectedTrash 彻底删除勾选的文件：第一次点击只是「上膛」，第二次才真删。
func (m *Model) purgeSelectedTrash() {
	files := m.selectedTrash()
	if len(files) == 0 {
		m.ui.trashMsg = "先在左边勾选文件"
		m.invalidator.Invalidate()
		return
	}
	if !m.ui.trashArmed {
		m.ui.trashArmed = true
		m.ui.trashMsg = "再点一次「确认彻底删除」：这 " + strconv.Itoa(len(files)) + " 个文件将无法恢复"
		m.invalidator.Invalidate()
		return
	}
	removed, err := explorer.PurgeTrash(m.col.Dir, files)
	m.ui.trashArmed = false
	m.openTrash()
	if err != nil {
		m.ui.trashMsg = "已删除 " + strconv.Itoa(removed) + " 个，但有失败：" + err.Error()
	} else {
		m.ui.trashMsg = "已彻底删除 " + strconv.Itoa(removed) + " 个文件"
	}
	m.invalidator.Invalidate()
}

// toggleAllTrash 全选 / 全不选回收站里的文件。
func (m *Model) toggleAllTrash(on bool) {
	for _, item := range m.ui.trashRows {
		m.ui.trashSel[item.File] = on
	}
	m.ui.trashArmed = false
	m.invalidator.Invalidate()
}

// chooseCollection 打开系统目录选择框；失败（没有可用的对话框后端）就退回手输路径。
// 用户点「取消」不算失败：直接什么都不做 —— 不要再弹一个「请输入路径」的框。
func (m *Model) chooseCollection() {
	start := ""
	if m.col != nil {
		start = filepath.Dir(m.col.Dir)
	}
	path, err := app.OpenDirectory(app.OpenDirectoryOptions{Title: "打开集合目录", InitialDirectory: start})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		m.ui.openErr = err.Error()
		m.ui.pathText = ""
		m.ui.pathOpen = true
		m.invalidator.Invalidate()
		return
	}
	if path == "" {
		return // 取消（后端没区分取消/空选时也走这里）
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

// rememberRecent 把集合目录记进「最近打开」：最新在前、去重、最多 8 条。
func (m *Model) rememberRecent(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	list := []string{dir}
	for _, path := range m.settings.RecentCollections {
		if path != dir && len(list) < 8 {
			list = append(list, path)
		}
	}
	m.settings.RecentCollections = list
	_ = settings.Save(m.settings)
}

// clearRecentCollections 清空「最近打开」的历史（不影响已经打开的集合）。
func (m *Model) clearRecentCollections() {
	m.settings.RecentCollections = nil
	_ = settings.Save(m.settings)
	m.copyNote = "已清除最近打开记录"
	m.invalidator.Invalidate()
}

// persistRoots 把「已打开的集合根 + 当前根」写进偏好，下次启动照此恢复。
func (m *Model) persistRoots() {
	paths := make([]string, 0, len(m.roots))
	for _, root := range m.roots {
		if root.col != nil && root.col.Dir != "" {
			paths = append(paths, root.col.Dir)
		}
	}
	m.settings.LastCollections = paths
	if m.col != nil && m.col.Dir != "" {
		m.settings.LastCollection = m.col.Dir
		m.settings.ActiveCollection = m.col.Dir
	}
	_ = settings.Save(m.settings)
	// 根增删后同步给 MCP 服务：AI 只该看到「当前打开」的那几个集合。
	m.refreshMCPRoots()
}

// ActivateCollection 按目录切到已打开的集合根（启动恢复上次的活动根用）。
func (m *Model) ActivateCollection(dir string) {
	for i, root := range m.roots {
		if root.col != nil && root.col.Dir == dir {
			m.activateRoot(i)
			return
		}
	}
}

// syncRoot 把当前界面状态存回活动根（切走前调用，切换回来能接着用）。
func (m *Model) syncRoot() {
	if m.active < 0 || m.active >= len(m.roots) {
		return
	}
	root := m.roots[m.active]
	// 先把当前标签里的编辑态落回标签，再整份存到根上：切回来才是离开时的样子。
	m.storeTab()
	root.col = m.col
	root.env = m.envName
	root.filter = m.filter
	root.file = m.entryFile
	root.tabs = m.tabs
	root.curTab = m.curTab
	root.overview = m.overview
	root.tabSeq = m.tabSeq
	root.tabScroll = m.tabScrollX
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
	// 公共参数跟着集合走：切换集合根时一起换掉，否则会拿上一个集合的默认头。
	m.defaults = explorer.LoadDefaults(root.col.Dir)
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
	m.restoreRootTabs(root)
	m.pendingSelect = root.file
	m.persistRoots()
	m.refreshEnvs()
	m.ui.envPick = m.envName
	m.ui.envErr = ""
	m.refreshShown()
	m.invalidator.Invalidate()
}

// restoreRootTabs 把根上存着的那份标签装回来。
//
// 多根并行看的是「一棵树里几个根」，标签也按根各留一份：从甲切到乙再切回甲，
// 甲的标签、当前标签、页签条滚到哪都还在。之前这里直接 closeAllTabs()，
// 于是切一次根等于把手上正在编辑的东西全丢了。
func (m *Model) restoreRootTabs(root *rootState) {
	// 文档草稿按文件名记，跨根会串：换根时清掉（标签自己的 editor 不受影响）。
	m.drafts = map[string]string{}
	m.tabMenuOpen = false
	// 树要按新根重算：shownOnce 留着旧值的话 refreshShown 会直接早退，
	// 屏幕上仍是上一个集合的条目。
	m.shownOnce = false
	m.treeDirty = true
	m.tabScrollWant = root.tabScroll
	m.tabScrollRestore = true
	m.tabs = root.tabs
	m.tabSeq = root.tabSeq
	switch {
	case len(m.tabs) == 0:
		m.curTab = -1
		m.overview = true
		m.clearTabState()
	case root.curTab < 0 || root.curTab >= len(m.tabs):
		m.curTab = 0
		m.overview = false
		m.applyFromTab(m.tabs[m.curTab])
		m.selectInList(m.tabs[m.curTab].file)
	default:
		m.curTab = root.curTab
		m.overview = false
		m.applyFromTab(m.tabs[m.curTab])
		m.selectInList(m.tabs[m.curTab].file)
	}
	m.gen++
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
	m.persistRoots()
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
	// 托盘提示里写着集合名与规模：换集合后要跟着变。
	m.refreshTray()
}

// closeRoot 关掉第 i 个集合（至少留一个）；关的是活动的就切到邻位。
func (m *Model) closeRoot(i int) {
	if i < 0 || i >= len(m.roots) || len(m.roots) <= 1 {
		return
	}
	closingActive := m.active == i
	m.roots = append(m.roots[:i], m.roots[i+1:]...)
	m.persistRoots()
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
	m.rememberRecent(col.Dir)
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
		m.persistRoots()
		return
	}
	m.syncRoot()
	m.roots = append(m.roots, &rootState{col: col, env: m.settings.Envs[col.Dir]})
	m.active = len(m.roots) - 1
	m.installRoot(m.roots[m.active])
	m.persistRoots()
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
	draft.loadExtras(explorer.ReadExtras(entry.Payload))
	m.req = draft
	// 记下磁盘基线：保存前靠它发现「文件被别人改过」。
	if m.col != nil {
		if hash, err := explorer.HashRequest(m.col.Dir, entry.File); err == nil {
			m.req.base = hash
		}
	}
	m.resetUndo()
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
// applyDraftToFile 把编辑器里的内容写进请求文件（不落盘）。
//
// 手动保存与自动保存共用它：两条路写出来的文件必须一字不差，
// 否则「自动保存过、再按 Ctrl+S」会莫名其妙地又改一遍。
func (m *Model) applyDraftToFile(file *collection.RequestFile) {
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
	explorer.WriteExtras(file, m.extrasFromDraft())
}

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
	m.applyDraftToFile(file)
	if err := explorer.SaveRequestChecked(m.col.Dir, entry.File, file, m.req.base); err != nil {
		if errors.Is(err, explorer.ErrConflict) {
			m.req.conflict = true
			m.req.err = "这个文件已被其它程序修改，先决定怎么处理再保存"
		} else {
			m.req.err = err.Error()
		}
		m.invalidator.Invalidate()
		return
	}
	m.req.conflict = false
	m.req.err = ""
	m.req.dirty = false
	if hash, hashErr := explorer.HashRequest(m.col.Dir, entry.File); hashErr == nil {
		m.req.base = hash
	}
	// 真保存成功：这份草稿没用了，删掉免得下次打开又"恢复"出旧内容。
	drafts.Delete(m.draftDir(), drafts.Request, m.col.Dir, entry.File)
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
		m.syncEnvName()
		return
	}
	names, err := explorer.ListEnvs(m.col.Dir)
	if err != nil {
		m.ui.envErr = err.Error()
		return
	}
	m.ui.envs = names
}

// syncEnvName 让「当前环境」始终属于刚刷新的这份列表。
//
// 删光环境、或者切到一个没有同名环境的集合之后，旧名字不该还挂着：
// 留着会让按钮显示别处的环境名，发送时还会拿旧变量去替换。
func (m *Model) syncEnvName() {
	if m.envName == "" {
		return
	}
	for _, name := range m.ui.envs {
		if name == m.envName {
			return
		}
	}
	next := ""
	if len(m.ui.envs) > 0 {
		next = m.ui.envs[0]
	}
	m.applyEnv(next)
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
	// 变量表可以拖拽重排：List 的 Reorder 在拖动结束时回调，交换后重绘。
	m.ui.envList.Label = func(index int) string {
		if index < 0 || index >= len(m.ui.envRows) {
			return ""
		}
		return m.ui.envRows[index].name
	}
	m.ui.envList.Reorder = func(rows []int, to int) {
		if reorderItems(m.ui.envRows, rows, to) {
			m.invalidator.Invalidate()
		}
	}
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

// checkUpdate 查一次发布（只在用户点按钮时联网，不在启动时偷偷发请求）。
func (m *Model) checkUpdate() {
	if m.ui.updateBusy {
		return
	}
	repo := strings.TrimSpace(m.settings.UpdateRepo)
	if repo == "" {
		repo = updater.Repo
	}
	m.ui.updateBusy = true
	m.ui.updateNote = "正在检查 " + repo + "…"
	m.ui.updateURL = ""
	m.invalidator.Invalidate()
	go func() {
		release, err := updater.Check(context.Background(), nil, repo, updater.Version)
		switch {
		case errors.Is(err, updater.ErrUpToDate):
			m.ui.updateNote = "已是最新版本（当前 v" + updater.Version + "）"
		case err != nil:
			m.ui.updateNote = "检查失败：" + err.Error()
		default:
			m.ui.updateNote = "有新版本 " + release.Version + "（当前 v" + updater.Version + "）"
			m.ui.updateURL = release.URL
		}
		m.ui.updateBusy = false
		m.invalidator.Invalidate()
	}()
}

// openUpdatePage 在系统浏览器里打开发布页。
func (m *Model) openUpdatePage() {
	if m.ui.updateURL == "" {
		return
	}
	if err := app.OpenURL(m.ui.updateURL); err != nil {
		m.ui.updateNote = "打不开浏览器：" + err.Error()
	}
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
	if proxy := strings.TrimSpace(m.ui.proxyText); proxy != m.settings.Proxy {
		m.settings.Proxy = proxy
		changed = true
	}
	if cert := strings.TrimSpace(m.ui.clientCertText); cert != m.settings.ClientCert {
		m.settings.ClientCert = cert
		changed = true
	}
	if key := strings.TrimSpace(m.ui.clientKeyText); key != m.settings.ClientKey {
		m.settings.ClientKey = key
		changed = true
	}
	if m.ui.persistCookies != m.settings.PersistCookies() {
		m.settings.NoCookiePersist = !m.ui.persistCookies
		m.applyCookiePersist()
		changed = true
	}
	if m.ui.autoSave != m.settings.AutoSave() {
		m.settings.NoAutoSave = !m.ui.autoSave
		changed = true
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(m.ui.autoSaveSec)); err == nil {
		if seconds < 1 {
			seconds = 1
		}
		if seconds > 30 {
			seconds = 30
		}
		if seconds != m.settings.AutoSaveSec {
			m.settings.AutoSaveSec = seconds
			changed = true
		}
	}
	if m.ui.closeToTray != m.settings.CloseToTray {
		m.settings.CloseToTray = m.ui.closeToTray
		changed = true
	}
	if m.ui.startMaximized != m.settings.StartMaximized() {
		m.settings.StartWindowed = !m.ui.startMaximized
		changed = true
	}
	if repo := strings.TrimSpace(m.ui.updateRepo); repo != m.settings.UpdateRepo {
		m.settings.UpdateRepo = repo
		changed = true
	}
	if m.ui.sealSecrets != m.settings.SealSecrets() {
		m.settings.NoSealSecrets = !m.ui.sealSecrets
		// 只影响之后的写入：已经加密的值仍然按密文读，改回明文要重新保存环境。
		if dir, err := settings.Dir(); err == nil {
			secrets.Configure(dir, m.settings.SealSecrets())
		}
		changed = true
	}
	if side := m.ui.splitSide == 1; side != m.settings.SplitSideBySide {
		m.settings.SplitSideBySide = side
		changed = true
	}
	if split := m.ui.docSplitPick == 1; split != m.settings.DocSplit {
		m.settings.DocSplit = split
		m.ui.docSplit = split
		changed = true
	}
	if changed {
		_ = settings.Save(m.settings)
	}
}

// applyCookiePersist 按当前偏好打开 / 关掉 Cookie 落盘。
// 关掉时不动内存里的 jar：本次会话仍共享，只是不再写文件。
func (m *Model) applyCookiePersist() {
	if !m.settings.PersistCookies() {
		send.Close()
		return
	}
	if dir, err := settings.Dir(); err == nil {
		_ = send.Open(dir)
	}
}

// applyMCPEdits 把 MCP 弹窗里的输入落到偏好（只在弹窗开着时调用）。
// 开关或地址变化即重启内嵌服务；地址先做一次规范化，空值回默认。
func (m *Model) applyMCPEdits() {
	addr := strings.TrimSpace(m.ui.mcpAddr)
	if addr == "" {
		addr = settings.DefaultMCPAddr
		m.ui.mcpAddr = addr
	}
	changed, restart := false, false
	if m.ui.mcpEnabled != m.settings.MCPEnabled {
		m.settings.MCPEnabled = m.ui.mcpEnabled
		changed, restart = true, true
	}
	if m.ui.mcpReadOnly != m.settings.MCPReadOnly {
		m.settings.MCPReadOnly = m.ui.mcpReadOnly
		changed, restart = true, true
	}
	if groups := m.pickedMCPGroups(); !sameStrings(groups, m.settings.MCPGroups) {
		m.settings.MCPGroups = groups
		changed, restart = true, true
	}
	if addr != m.settings.MCPAddr {
		m.settings.MCPAddr = addr
		changed, restart = true, true
	}
	if off := m.ui.mcpConsentOff; off != m.settings.MCPConsentDisabled {
		m.settings.MCPConsentDisabled = off
		changed = true
	}
	// 浏览器来源白名单：逗号分隔，空 = 拒绝一切带 Origin 的请求。
	if origins := parseMCPOrigins(m.ui.mcpOrigins); !sameStrings(origins, m.settings.MCPAllowOrigins) {
		m.settings.MCPAllowOrigins = origins
		changed, restart = true, true
	}
	// 访问令牌可编辑：改过 / 重新生成了就落盘并重启（服务按旧令牌在校验）。
	if token := strings.TrimSpace(m.ui.mcpToken); token != m.settings.MCPToken {
		m.settings.MCPToken = token
		changed, restart = true, true
	}
	// 工作区授权：勾选 = 出现在白名单；写权限随行一起存。
	allow := m.mcpAllowFromRows()
	if !sameMCPAllow(allow, m.settings.MCPAllow) {
		m.settings.MCPAllow = allow
		changed = true
		m.refreshMCPRoots()
	}
	if changed {
		_ = settings.Save(m.settings)
	}
	if restart {
		m.restartMCP()
		// 首次启用会生成令牌：弹窗里立刻显示出来。
		m.ui.mcpToken = m.settings.MCPToken
	}
}

// mcpAllowFromRows 把弹窗里的勾选状态收成偏好里的白名单。
func (m *Model) mcpAllowFromRows() []settings.MCPAllowEntry {
	allow := make([]settings.MCPAllowEntry, 0, len(m.ui.mcpAllow))
	for _, row := range m.ui.mcpAllow {
		if row.Allowed {
			allow = append(allow, settings.MCPAllowEntry{Dir: row.Dir, Writable: row.Writable})
		}
	}
	return allow
}

// sameMCPAllow 比较两份白名单（顺序无关的集合比较）。
func sameMCPAllow(a, b []settings.MCPAllowEntry) bool {
	if len(a) != len(b) {
		return false
	}
	index := make(map[string]bool, len(a))
	for _, entry := range a {
		index[entry.Dir+"|"+boolMark(entry.Writable)] = true
	}
	for _, entry := range b {
		if !index[entry.Dir+"|"+boolMark(entry.Writable)] {
			return false
		}
	}
	return true
}

// boolMark 是白名单比较用的可写标记。
func boolMark(writable bool) string {
	if writable {
		return "w"
	}
	return "r"
}

// 代码生成弹窗的语言顺序。
var (
	codeLangs     = []int{0, 1, 2, 3, 4}
	codeLangNames = []string{"cURL", "fetch", "axios", "Go", "Python"}
	codeLangKinds = []codegen.Lang{
		codegen.LangCurl, codegen.LangFetch, codegen.LangAxios, codegen.LangGo, codegen.LangPython,
	}
)

// openCodeDialog 用当前草稿生成代码并打开弹窗。
func (m *Model) openCodeDialog() {
	m.codeOpen = true
	m.codeLang = 0
	m.refreshCode()
	m.invalidator.Invalidate()
}

// refreshCode 按当前语言重算代码片段；只在打开弹窗或切换语言时算。
func (m *Model) refreshCode() {
	if m.codeLang < 0 || m.codeLang >= len(codeLangKinds) {
		m.codeLang = 0
	}
	text, err := codegen.Snippet(codeLangKinds[m.codeLang], m.codeRequest())
	if err != nil {
		m.codeText = "生成失败：" + err.Error()
		return
	}
	m.codeText = text
}

// codeRequest 把当前草稿（含未保存改动）整理成 codegen 的中立入参。
func (m *Model) codeRequest() codegen.Request {
	file := m.scratchFile()
	headers := make([]codegen.KV, 0, len(file.HTTP.Headers)+1)
	for _, header := range file.HTTP.Headers {
		if header.Enabled && strings.TrimSpace(header.Name) != "" {
			headers = append(headers, codegen.KV{Name: header.Name, Value: header.Value})
		}
	}
	target := file.HTTP.URL
	if len(file.HTTP.Params) > 0 {
		pairs := make([]string, 0, len(file.HTTP.Params))
		for _, row := range file.HTTP.Params {
			if row.Enabled && strings.TrimSpace(row.Name) != "" {
				pairs = append(pairs, row.Name+"="+row.Value)
			}
		}
		if len(pairs) > 0 {
			sep := "?"
			if strings.Contains(target, "?") {
				sep = "&"
			}
			target += sep + strings.Join(pairs, "&")
		}
	}
	if auth := file.HTTP.Auth; auth != nil {
		switch auth.Type {
		case "basic":
			token := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
			headers = append(headers, codegen.KV{Name: "Authorization", Value: "Basic " + token})
		case "bearer":
			if auth.Token != "" {
				headers = append(headers, codegen.KV{Name: "Authorization", Value: "Bearer " + auth.Token})
			}
		case "apikey":
			if auth.Key != "" {
				if auth.In == "query" {
					sep := "?"
					if strings.Contains(target, "?") {
						sep = "&"
					}
					target += sep + url.QueryEscape(auth.Key) + "=" + url.QueryEscape(auth.Value)
				} else {
					headers = append(headers, codegen.KV{Name: auth.Key, Value: auth.Value})
				}
			}
		}
	}
	body := file.HTTP.Body.Raw
	if body == "" {
		body = file.HTTP.Body.Data
	}
	if body == "" && len(file.HTTP.Body.Form) > 0 {
		parts := make([]string, 0, len(file.HTTP.Body.Form))
		for _, row := range file.HTTP.Body.Form {
			if row.Enabled {
				parts = append(parts, row.Name+"="+row.Value)
			}
		}
		body = strings.Join(parts, "&")
	}
	return codegen.Request{Method: file.HTTP.Method, URL: target, Headers: headers, Body: body}
}

// exportCurl 把当前草稿生成 cURL 写进剪贴板。
func (m *Model) exportCurl() {
	text, err := codegen.Snippet(codegen.LangCurl, m.codeRequest())
	if err != nil {
		m.copyNote = "生成失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	if err := ui.SetClipboardText(text); err != nil {
		m.copyNote = "复制失败：" + err.Error()
	} else {
		m.copyNote = "已复制 cURL"
	}
	m.storeTab()
	m.invalidator.Invalidate()
}

// formatURL 只做安全整理：去空白、丢掉空查询段，不重新编码（避免破坏 {{变量}}）。
func (m *Model) formatURL() {
	raw := strings.TrimSpace(m.req.url)
	if raw == "" {
		return
	}
	base, query, hasQuery := strings.Cut(raw, "?")
	base = strings.TrimSpace(base)
	if hasQuery {
		parts := make([]string, 0, 4)
		for _, part := range strings.Split(query, "&") {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		raw = base
		if len(parts) > 0 {
			raw += "?" + strings.Join(parts, "&")
		}
	} else {
		raw = base
	}
	m.req.url = raw
	m.markEdited()
	m.req.err = ""
	m.invalidator.Invalidate()
}

// setDocSplit 记住文档页的默认视图（false = 全宽预览，true = 编辑（左写右看））。
func (m *Model) setDocSplit(split bool) {
	m.settings.DocSplit = split
	m.ui.docSplit = split
	m.ui.docSplitPick = 0
	if split {
		m.ui.docSplitPick = 1
	}
	_ = settings.Save(m.settings)
	m.invalidator.Invalidate()
}

// setSplitLayout 切换请求 / 响应分栏方向（工具条按钮与设置面板共用）。
func (m *Model) setSplitLayout(side bool) {
	m.settings.SplitSideBySide = side
	m.ui.splitSide = 0
	if side {
		m.ui.splitSide = 1
	}
	_ = settings.Save(m.settings)
	m.invalidator.Invalidate()
}

// syncSettingsUI 把落盘的偏好装进设置表单（打开面板 / 启动时各一次）。
func (m *Model) syncSettingsUI() {
	m.ui.timeoutText = strconv.Itoa(m.settings.TimeoutSec)
	m.ui.proxyText = m.settings.Proxy
	m.ui.clientCertText = m.settings.ClientCert
	m.ui.clientKeyText = m.settings.ClientKey
	m.ui.follow = m.settings.FollowRedirects()
	m.ui.insecure = m.settings.InsecureSSL
	m.ui.persistCookies = m.settings.PersistCookies()
	m.ui.closeToTray = m.settings.CloseToTray
	m.ui.startMaximized = m.settings.StartMaximized()
	m.ui.sealSecrets = m.settings.SealSecrets()
	m.ui.scalePick = m.settings.UIScalePercent()
	m.ui.docSplit = m.settings.DocSplit
	m.ui.docSplitPick = 0
	if m.settings.DocSplit {
		m.ui.docSplitPick = 1
	}
	m.ui.autoSave = m.settings.AutoSave()
	m.ui.autoSaveSec = strconv.Itoa(int(m.settings.AutoSaveInterval().Seconds()))
	m.ui.updateRepo = m.settings.UpdateRepo
	m.ui.splitSide = 0
	if m.settings.SplitSideBySide {
		m.ui.splitSide = 1
	}
	m.ui.mcpEnabled = m.settings.MCPEnabled
	m.ui.mcpReadOnly = m.settings.MCPReadOnly
	m.syncMCPGroups()
	m.ui.mcpAddr = m.settings.MCPAddr
	m.ui.mcpOrigins = strings.Join(m.settings.MCPAllowOrigins, ", ")
	m.ui.mcpAuditRows = m.loadMCPAudit()
	m.ui.mcpConsentOff = m.settings.MCPConsentDisabled
	m.ui.mcpToken = m.settings.MCPToken
}

// openSettings 打开设置面板（软件设置），并把每次打开都要重新探测的状态抓一次。
func (m *Model) openSettings() {
	m.ui.settingsOpen = true
	// 常规入口一律落在第一个分区：上一次可能是从底栏 MCP 直达进来的。
	m.ui.settingsSection = 0
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

// openMCPSettings 打开 MCP 服务的独立弹窗（底栏 MCP 入口）。
// 和软件设置完全分开：没有左侧分区菜单，与 Wails 版的状态栏入口一致。
func (m *Model) openMCPSettings() {
	// 每次打开都按落盘的偏好刷新表单，不沿用上一次没保存的编辑。
	m.ui.mcpEnabled = m.settings.MCPEnabled
	m.ui.mcpReadOnly = m.settings.MCPReadOnly
	m.syncMCPGroups()
	m.ui.mcpAddr = m.settings.MCPAddr
	m.ui.mcpToken = m.settings.MCPToken
	m.refreshMCPAllowRows()
	// 每次打开都重新读一遍调用审计：AI 的调用发生在面板关着的时候，
	// 只在打开时载入一次快照的话，用户看到的是上一轮的旧记录。
	m.ui.mcpAuditRows = m.loadMCPAudit()
	m.ui.mcpOpen = true
	m.invalidator.Invalidate()
}

// OpenMCPSettings 打开 MCP 面板（状态栏入口之外，供 main 的 -panel 调试参数或将来的菜单使用）。
func (m *Model) OpenMCPSettings() { m.openMCPSettings() }

// setSortMode 换树的排序（磁盘顺序 / 名称升降），落盘并重建。
func (m *Model) setSortMode(mode string) {
	switch mode {
	case "", "name-asc", "name-desc":
	default:
		return
	}
	m.sortMode = mode
	if m.settings.SortMode != mode {
		m.settings.SortMode = mode
		_ = settings.Save(m.settings)
	}
	m.treeDirty = true
	m.invalidator.Invalidate()
}

// forceSaveRequest 无视外部改动直接覆盖（冲突提示里的「覆盖」）。
func (m *Model) forceSaveRequest() {
	m.req.base = "" // 没有基线 = 不检查
	m.req.conflict = false
	m.saveRequest()
	m.req.note = "已覆盖磁盘上的版本"
	m.invalidator.Invalidate()
}

// reloadRequestFromDisk 丢掉本地改动，重新从磁盘装载（冲突提示里的「重新加载」）。
func (m *Model) reloadRequestFromDisk() {
	m.req.conflict = false
	m.req.err = ""
	if m.reloadCollection() {
		m.reloadActive()
	}
	if entry := m.entryByFile(m.req.file); entry != nil {
		m.loadRequestDraft(entry)
		m.req.note = "已重新加载磁盘上的版本"
	}
	m.invalidator.Invalidate()
}
