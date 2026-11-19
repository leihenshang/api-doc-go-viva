package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// tabState 是一条已打开内容（请求或文档）的全部界面状态。
//
// 切换标签时把 Model 上的同名字段存回旧标签、再从新标签装回来：这样既有的
// 编辑 / 发送 / Markdown 排版逻辑都不用知道标签的存在，多标签只是状态快照的搬运。
type tabState struct {
	id    int
	entry *explorer.Entry
	file  string
	tab   detailTab
	// scratch 是「临时请求」标签：不来自磁盘，保存时走另存为。
	scratch bool

	req     requestDraft
	tables  requestTables
	respTab int
	docs    docState
	editor  docEditor

	resp     send.Outcome
	hasResp  bool
	sending  bool
	statusOK bool
	status   string
	copyNote string
	reason   string
	elapsed  string
	size     string
	bodyText string
	missing  string
	failText string
	gen      int
}

// activeTab 是当前标签；没有打开任何内容时返回 nil。
func (m *Model) activeTab() *tabState {
	if m.curTab < 0 || m.curTab >= len(m.tabs) {
		return nil
	}
	return m.tabs[m.curTab]
}

// activeEntry 是当前标签对应的条目（右侧详情只读它）。
func (m *Model) activeEntry() *explorer.Entry {
	if tab := m.activeTab(); tab != nil {
		return tab.entry
	}
	return nil
}

// findTab 找同一条目是否已经打开；标签只属于当前集合，所以只按文件路径找。
func (m *Model) findTab(file string) int {
	for i, tab := range m.tabs {
		if tab.file == file {
			return i
		}
	}
	return -1
}

// findTabID 按标签代号找位置（后台发送结果回来时可能已经关掉了）。
func (m *Model) findTabID(id int) int {
	for i, tab := range m.tabs {
		if tab.id == id {
			return i
		}
	}
	return -1
}

// tabTitle 是标签上的文字；条目被重命名后跟着变。
func (m *Model) tabTitle(tab *tabState) string {
	if tab.scratch {
		return "临时请求"
	}
	if tab.entry != nil && tab.entry.Name != "" {
		return tab.entry.Name
	}
	if name := strings.TrimSuffix(tab.file, ".yml"); name != "" {
		return name
	}
	return "未命名"
}

// openEntry 打开一条内容：已经打开就切过去，否则在末尾新建标签。
// 调用方通常是列表选中变化，或新建 / 保存后的 pendingSelect。
func (m *Model) openEntry(entry *explorer.Entry) {
	if index := m.findTab(entry.File); index >= 0 {
		m.activateTab(index)
		return
	}
	m.storeTab()
	m.overview = false
	tab := &tabState{id: m.tabSeq, entry: entry, file: entry.File}
	m.tabSeq++
	m.tabs = append(m.tabs, tab)
	m.curTab = len(m.tabs) - 1
	m.clearTabState()
	m.ensureEntry(entry)
	m.storeTab()
	m.selectInList(entry.File)
}

// activateTab 切到第 i 个标签：先存当前，再装目标。
func (m *Model) activateTab(i int) {
	if i < 0 || i >= len(m.tabs) || i == m.curTab {
		return
	}
	m.storeTab()
	m.curTab = i
	m.overview = false
	m.applyFromTab(m.tabs[i])
	m.selectInList(m.tabs[i].file)
	m.invalidator.Invalidate()
}

// closeTab 关掉第 i 个标签；关的是当前标签就切到邻位，全关掉后回到空态。
func (m *Model) closeTab(i int) {
	if i < 0 || i >= len(m.tabs) {
		return
	}
	closingActive := i == m.curTab
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	switch {
	case len(m.tabs) == 0:
		m.curTab = -1
		m.overview = true
		m.clearTabState()
		m.tree.Select(-1)
	case i < m.curTab:
		m.curTab--
	case closingActive:
		if i >= len(m.tabs) {
			i = len(m.tabs) - 1
		}
		m.curTab = i
		m.overview = false
		m.applyFromTab(m.tabs[i])
		m.selectInList(m.tabs[i].file)
	}
	m.invalidator.Invalidate()
}

// closeOthers 只留下第 i 个标签。
func (m *Model) closeOthers(i int) {
	if i < 0 || i >= len(m.tabs) {
		return
	}
	tab := m.tabs[i]
	m.tabs = []*tabState{tab}
	m.curTab = 0
	m.overview = false
	m.applyFromTab(tab)
	m.selectInList(tab.file)
	m.invalidator.Invalidate()
}

// closeRight 关掉第 i 个标签右侧的所有标签。
func (m *Model) closeRight(i int) {
	if i < 0 || i >= len(m.tabs)-1 {
		return
	}
	m.tabs = m.tabs[:i+1]
	if m.curTab > i {
		m.curTab = i
		m.overview = false
		m.applyFromTab(m.tabs[i])
		m.selectInList(m.tabs[i].file)
	}
	m.invalidator.Invalidate()
}

// closeTabOf 关掉某条内容的标签（删除到回收站时用）。
func (m *Model) closeTabOf(file string) {
	if i := m.findTab(file); i >= 0 {
		m.closeTab(i)
	}
}

// closeAllTabs 在切换集合根时清空标签：标签里的条目不跨集合保留。
func (m *Model) closeAllTabs() {
	m.tabs = nil
	m.curTab = -1
	// tabSeq 不重置：在途请求的结果带着旧代号回来时，不会撞上新标签。
	m.clearTabState()
	m.tree.Select(-1)
}

// storeTab 把 Model 的派生状态存回当前标签。
func (m *Model) storeTab() {
	if m.curTab < 0 || m.curTab >= len(m.tabs) {
		return
	}
	m.captureInto(m.tabs[m.curTab])
}

// captureInto 把 Model 的派生状态写进 t（id / entry 由调用方维护）。
func (m *Model) captureInto(t *tabState) {
	t.file = m.entryFile
	if t.file == "" && t.entry != nil {
		t.file = t.entry.File
	}
	t.tab = m.tab
	t.scratch = m.scratch
	t.req = m.req
	t.tables = m.tables

	t.respTab = m.respTab
	t.docs = m.docs
	t.editor = m.editor
	t.resp = m.resp
	t.hasResp, t.sending, t.statusOK = m.hasResp, m.sending, m.statusOK
	t.status, t.reason, t.elapsed, t.size = m.status, m.reason, m.elapsed, m.size
	t.copyNote = m.copyNote
	t.bodyText, t.missing, t.failText = m.bodyText, m.missing, m.failText
	t.gen = m.gen
}

// applyFromTab 把标签里的状态装回 Model。
func (m *Model) applyFromTab(t *tabState) {
	m.entryFile = t.file
	if m.entryFile == "" && t.entry != nil {
		m.entryFile = t.entry.File
	}
	m.tab = t.tab
	m.scratch = t.scratch
	m.req = t.req
	m.tables = t.tables

	m.respTab = t.respTab
	m.docs = t.docs
	m.editor = t.editor
	m.resp = t.resp
	m.hasResp, m.sending, m.statusOK = t.hasResp, t.sending, t.statusOK
	m.status, m.reason, m.elapsed, m.size = t.status, t.reason, t.elapsed, t.size
	m.copyNote = t.copyNote
	m.bodyText, m.missing, m.failText = t.bodyText, t.missing, t.failText
	m.gen = t.gen
}

// clearTabState 把 Model 的派生状态清空（新建标签 / 关掉最后一个标签时）。
func (m *Model) clearTabState() {
	m.entryFile = ""
	m.tab = tabParams
	m.scratch = false
	m.req = requestDraft{}
	m.tables = requestTables{}
	m.respTab = 0
	m.docs = docState{look: m.docs.look}
	m.editor = docEditor{}
	m.resetResponse()
	m.sending = false
	m.gen++
}

// refreshTabs 在重扫集合后刷新标签指向的条目，并关掉已经不存在的标签。
func (m *Model) refreshTabs() {
	if len(m.tabs) == 0 {
		return
	}
	active := m.activeTab()
	kept := m.tabs[:0]
	for _, tab := range m.tabs {
		if tab.scratch {
			kept = append(kept, tab)
			continue
		}
		entry := m.entryByFile(tab.file)
		if entry == nil {
			continue
		}
		tab.entry = entry
		kept = append(kept, tab)
	}
	m.tabs = kept
	m.curTab = -1
	for i, tab := range m.tabs {
		if tab == active {
			m.curTab = i
			break
		}
	}
	switch {
	case m.curTab >= 0:
		// 当前标签还在：条目指针换了，Model 里的派生状态不用重建。
		m.entryFile = m.tabs[m.curTab].file
	case len(m.tabs) > 0:
		m.curTab = 0
		m.applyFromTab(m.tabs[0])
	case active != nil:
		m.clearTabState()
		m.tree.Select(-1)
	}
}

// reloadActive 让当前标签按磁盘内容重建（保存请求后调用），保留当前分段页签。
func (m *Model) reloadActive() {
	tab := m.activeTab()
	if tab == nil {
		return
	}
	entry := m.entryByFile(tab.file)
	if entry == nil {
		return
	}
	section := m.tab
	tab.entry = entry
	m.clearTabState()
	m.ensureEntry(entry)
	if entry.Kind == explorer.KindHTTP {
		m.tab = section
	}
	m.storeTab()
}

// selectInList 让集合树选中某个条目；条目被过滤掉就清掉选择。
func (m *Model) selectInList(file string) { m.selectEntryRow(file) }
