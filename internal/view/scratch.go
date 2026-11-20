package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// scratchEntry 是临时请求的占位条目：不落盘，只给详情页标题与方法用。
var scratchEntry = &explorer.Entry{Name: "临时请求", Kind: explorer.KindHTTP}

// openScratch 新开一个临时请求标签：不落盘，保存时走另存为。
func (m *Model) openScratch() {
	m.storeTab()
	tab := &tabState{id: m.tabSeq, scratch: true, entry: scratchEntry}
	m.tabSeq++
	m.tabs = append(m.tabs, tab)
	m.curTab = len(m.tabs) - 1
	m.overview = false
	m.clearTabState()
	m.scratch = true
	m.req = requestDraft{method: "GET", bodyType: "none", authType: "none", authIn: "header"}
	m.req.params = []*kvRow{{enabled: true}}
	m.req.headers = []*kvRow{{enabled: true, name: "Content-Type", value: "application/json"}}
	m.tables = requestTables{metaLine: "临时请求 · 未保存"}
	m.resetUndo()
	m.tree.Select(-1)
	m.storeTab()
}

// scratchFile 把草稿拼成一次发送用的请求文件。
func (m *Model) scratchFile() *collection.RequestFile {
	file := &collection.RequestFile{Info: collection.FileInfo{Name: "临时请求", Type: "http"}}
	file.HTTP.Method = strings.ToUpper(strings.TrimSpace(m.req.method))
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	file.HTTP.URL = strings.TrimSpace(m.req.url)
	file.HTTP.Params = kvFromEditor(m.req.params)
	file.HTTP.Headers = kvFromEditor(m.req.headers)
	file.HTTP.Body.Form = kvFromEditor(m.req.form)
	file.HTTP.Body.Type = m.req.bodyType
	file.HTTP.Body.Raw = m.req.bodyRaw
	file.HTTP.Auth = m.authFromDraft()
	return file
}

// saveScratch 打开另存为弹窗，预填当前方法 / 地址。
func (m *Model) saveScratch() {
	m.ui.savingScratch = true
	m.ui.newReqOpen = true
	m.ui.newErr = ""
	m.ui.newName = "临时请求"
	m.ui.newMethod = m.req.method
	m.ui.newURL = m.req.url
	m.ui.newFolder = m.newRequestFolder()
}

func (m *Model) closeScratchTab() {
	if tab := m.activeTab(); tab != nil && tab.scratch {
		m.closeTab(m.curTab)
	}
}
