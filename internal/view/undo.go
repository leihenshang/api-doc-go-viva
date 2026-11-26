package view

import (
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/vivaui/viva/ui"
)

// 请求级撤销 / 重做：把草稿整份拍快照放进栈里。
//
// 为什么按「整份草稿」而不是按字段：请求就几 KB，整份快照最不容易漏状态
// （参数表增删行、认证类型切换、正文换 raw/data 都能一次还原），
// 代价只是每次编辑停顿多一次小拷贝。
const (
	undoLimit  = 50                     // 栈深上限，超出丢最老的
	undoWindow = 400 * time.Millisecond // 同一段连续输入合并成一步
)

// markEdited 是所有编辑点唯一的口子：先标脏，再记一次编辑序号。
// 帧末由 captureUndo 按序号决定要不要压栈，避免每帧都序列化。
func (m *Model) markEdited() {
	m.req.dirty = true
	m.editSeq++
	m.noteEdit()
}

// captureUndo 在帧末调用：这一帧有过编辑就按「400ms 合并」的规则压栈。
func (m *Model) captureUndo() {
	if m.editSeq == m.snapSeq {
		return
	}
	now := time.Now()
	snapshot := m.req.clone()
	if len(m.undoStack) > 0 && now.Sub(m.lastSnap) < undoWindow {
		m.undoStack[len(m.undoStack)-1] = snapshot // 同一段输入：覆盖栈顶，最终状态才算一步
	} else {
		m.undoStack = append(m.undoStack, snapshot)
		if len(m.undoStack) > undoLimit {
			m.undoStack = m.undoStack[len(m.undoStack)-undoLimit:]
		}
	}
	m.lastSnap = now
	m.snapSeq = m.editSeq
	m.redoStack = nil // 新的编辑让重做失效，和编辑器一致
}

// undo 回到上一份快照。
func (m *Model) undo() {
	if len(m.undoStack) < 2 {
		return
	}
	current := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]
	m.redoStack = append(m.redoStack, current)
	m.restoreDraft(m.undoStack[len(m.undoStack)-1])
}

// redo 重做刚撤销的那一步。
func (m *Model) redo() {
	if len(m.redoStack) == 0 {
		return
	}
	next := m.redoStack[len(m.redoStack)-1]
	m.redoStack = m.redoStack[:len(m.redoStack)-1]
	m.undoStack = append(m.undoStack, next)
	m.restoreDraft(next)
}

// undoShortcuts 挂 Ctrl/Cmd+Z 与 Ctrl/Cmd+Shift+Z（兼容 Ctrl+Y）。
func (m *Model) undoShortcuts(c ui.C) {
	if ui.Shortcut(c, ui.Primary|ui.Shift, "z") || ui.Shortcut(c, ui.Primary, "y") {
		c.After(m.redo)
		return
	}
	if ui.Shortcut(c, ui.Primary, "z") {
		c.After(m.undo)
	}
}

// resetUndo 换请求 / 换标签时清空历史：撤销不能跨请求。
func (m *Model) resetUndo() {
	m.undoStack, m.redoStack = nil, nil
	m.editSeq, m.snapSeq = 0, 0
	m.lastSnap = time.Time{}
	// 栈底先放一份当前状态，撤销时才有「回到打开时」的那一步。
	m.undoStack = append(m.undoStack, m.req.clone())
}

// clone 深拷贝一份草稿：切片里的行是指针，浅拷贝会让撤销栈跟着后续编辑一起变。
func (d requestDraft) clone() requestDraft {
	out := d
	out.params = cloneRows(d.params)
	out.headers = cloneRows(d.headers)
	out.form = cloneRows(d.form)
	out.fields = cloneFields(d.fields)
	out.vars = cloneVars(d.vars)
	out.asserts = cloneAsserts(d.asserts)
	out.resp = d.resp.clone()
	return out
}

func cloneVars(rows []*varRow) []*varRow {
	if rows == nil {
		return nil
	}
	out := make([]*varRow, len(rows))
	for i, row := range rows {
		copied := *row
		out[i] = &copied
	}
	return out
}

func cloneAsserts(rows []*assertRow) []*assertRow {
	if rows == nil {
		return nil
	}
	out := make([]*assertRow, len(rows))
	for i, row := range rows {
		copied := *row
		out[i] = &copied
	}
	return out
}

// clone 深拷响应字段段：字段行与响应头快照都要独立，撤销才能整份还原。
func (d respDocDraft) clone() respDocDraft {
	out := d
	out.fields = cloneFields(d.fields)
	if d.headers != nil {
		out.headers = make([]collection.KV, len(d.headers))
		copy(out.headers, d.headers)
	}
	return out
}

func cloneRows(rows []*kvRow) []*kvRow {
	if rows == nil {
		return nil
	}
	out := make([]*kvRow, len(rows))
	for i, row := range rows {
		copied := *row
		out[i] = &copied
	}
	return out
}

func cloneFields(rows []*fieldRow) []*fieldRow {
	if rows == nil {
		return nil
	}
	out := make([]*fieldRow, len(rows))
	for i, row := range rows {
		copied := *row
		out[i] = &copied
	}
	return out
}

// restoreDraft 把一份快照装回编辑区，并让派生表重建、标脏。
func (m *Model) restoreDraft(d requestDraft) {
	d.dirty = m.req.dirty
	d.err = m.req.err
	m.req = d.clone()
	m.req.dirty = true
	m.tables.params = kvRows(kvFromEditor(m.req.params))
	m.tables.headers = kvRows(kvFromEditor(m.req.headers))
	// 记一拍序号：撤销本身不再产生新的撤销步骤。
	m.editSeq++
	m.snapSeq = m.editSeq
	m.invalidator.Invalidate()
}
