package view

import (
	"path"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
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
}
