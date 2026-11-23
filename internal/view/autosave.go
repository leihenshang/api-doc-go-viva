package view

import (
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/drafts"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"gopkg.in/yaml.v3"
)

// 自动保存与草稿：
//
//	自动保存（默认开）  最后一次编辑停 N 秒后，直接写回集合文件——与 Ctrl+S 等价，
//	                    只是不用按。写成功后删掉草稿。
//	自动保存（关）      改动只进草稿（配置目录），重开还在，但不动集合文件。
//
// 两条路都经过同一个 `applyDraftToFile`，所以「自动保存」写出来的内容和手动保存
// 一字不差；草稿存的也是同一份 YAML，恢复时按打开请求的同一套逻辑装回编辑器。
//
// 只在窗口线程做磁盘写：定时器只负责叫醒窗口（Invalidate），真正落盘发生在
// 下一帧的 pumpAutoSave 里，避免和绘制并发改模型。

// noteEdit 在每次编辑后调用：重置「静默计时器」。
func (m *Model) noteEdit() {
	if m.col == nil || m.req.file == "" || m.scratch {
		return
	}
	interval := m.settings.AutoSaveInterval()
	m.autoSaveAt = time.Now().Add(interval)
	m.autoSavePending = true
	// 停手后没有新事件，得有人叫醒窗口才会来下一帧。
	time.AfterFunc(interval+30*time.Millisecond, m.invalidator.Invalidate)
}

// pumpAutoSave 每帧开头调用：到点了就把该写的写掉。
func (m *Model) pumpAutoSave() {
	if m.autoSavePending && !time.Now().Before(m.autoSaveAt) {
		m.autoSavePending = false
		m.flushEdits()
	}
	// 文档没有「请求草稿」那套：直接看正文有没有变，静默到点就落盘。
	if m.tab == tabEdit && m.col != nil && m.editor.file != "" && m.docDirty() {
		if m.autoSaveDocAt.IsZero() {
			m.autoSaveDocAt = time.Now().Add(m.settings.AutoSaveInterval())
			time.AfterFunc(m.settings.AutoSaveInterval()+30*time.Millisecond, m.invalidator.Invalidate)
		} else if !time.Now().Before(m.autoSaveDocAt) {
			m.autoSaveDocAt = time.Time{}
			m.flushDoc()
		}
	} else {
		m.autoSaveDocAt = time.Time{}
	}
}

// flushEdits 把请求的改动落盘（自动保存开）或写成草稿（关）。
func (m *Model) flushEdits() {
	if m.req.file == "" || m.scratch || !m.req.dirty {
		return
	}
	entry := m.entryByFile(m.req.file)
	if entry == nil || entry.Payload == nil {
		return
	}
	if m.settings.AutoSave() {
		m.autoSaveRequest(entry)
		return
	}
	m.writeRequestDraft(entry)
}

// autoSaveRequest 直接写回集合文件：与 Ctrl+S 同一条路，只是不刷新整棵树。
func (m *Model) autoSaveRequest(entry *explorer.Entry) {
	file := entry.Payload
	m.applyDraftToFile(file)
	if err := explorer.SaveRequest(m.col.Dir, entry.File, file); err != nil {
		m.req.note = "自动保存失败：" + err.Error()
		return
	}
	m.req.dirty = false
	m.req.note = "已自动保存 " + time.Now().Format("15:04")
	drafts.Delete(m.draftDir(), drafts.Request, m.col.Dir, entry.File)
	m.syncTreeName(entry)
}

// writeRequestDraft 把当前编辑写成草稿文件（不碰集合）。
func (m *Model) writeRequestDraft(entry *explorer.Entry) {
	file := *entry.Payload // 浅拷贝：只为了拿到一份可序列化的快照
	m.applyDraftToFile(&file)
	data, err := yaml.Marshal(file)
	if err != nil {
		return
	}
	if err := drafts.Save(m.draftDir(), drafts.Request, m.col.Dir, entry.File, data); err != nil {
		m.req.note = "草稿保存失败：" + err.Error()
		return
	}
	m.req.note = "改动已存为草稿（未写入集合）"
}

// flushDoc 把文档正文落盘或写成草稿。
func (m *Model) flushDoc() {
	if m.col == nil || m.editor.file == "" {
		return
	}
	if m.settings.AutoSave() {
		if err := explorer.SaveDoc(m.col.Dir, m.editor.file, m.editor.front, m.editor.text); err != nil {
			m.editor.status = "自动保存失败：" + err.Error()
			return
		}
		m.editor.saved = m.editor.text
		m.editor.status = "已自动保存 " + time.Now().Format("15:04")
		drafts.Delete(m.draftDir(), drafts.Doc, m.col.Dir, m.editor.file)
		return
	}
	if err := drafts.Save(m.draftDir(), drafts.Doc, m.col.Dir, m.editor.file, []byte(m.editor.text)); err != nil {
		m.editor.status = "草稿保存失败：" + err.Error()
		return
	}
	m.editor.status = "改动已存为草稿（未写入集合）"
}

// restoreRequestDraft 打开请求时把草稿盖回去；没有草稿就什么都不做。
func (m *Model) restoreRequestDraft(entry *explorer.Entry) {
	if m.col == nil || entry == nil {
		return
	}
	data, ok := drafts.Load(m.draftDir(), drafts.Request, m.col.Dir, entry.File)
	if !ok {
		return
	}
	var file collection.RequestFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return
	}
	restored := *entry
	restored.Payload = &file
	m.loadRequestDraft(&restored)
	m.req.dirty = true
	m.req.note = "已恢复上次未保存的改动（Ctrl+S 保存）"
}

// restoreDocDraft 打开文档时读草稿；返回是否真的恢复过。
func (m *Model) restoreDocDraft(entry *explorer.Entry) bool {
	if m.col == nil || entry == nil {
		return false
	}
	data, ok := drafts.Load(m.draftDir(), drafts.Doc, m.col.Dir, entry.File)
	if !ok || string(data) == entry.Content {
		return false
	}
	m.editor.text = string(data)
	return true
}

// draftDir 是配置目录下的 drafts；读不到配置目录就不落草稿（不影响功能）。
func (m *Model) draftDir() string {
	dir, err := settings.Dir()
	if err != nil {
		return ""
	}
	return dir + "/drafts"
}

// syncTreeName 让树上的名字跟着自动保存变（名称只在「重命名」里改，这里只处理这种情况）。
//
// 只在内存里改行标签，避免每次自动保存都整树重扫。
func (m *Model) syncTreeName(entry *explorer.Entry) {
	if entry.Payload == nil || entry.Name == entry.Payload.Info.Name {
		return
	}
	entry.Name = entry.Payload.Info.Name
	m.treeDirty = true
}
