package view

import (
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
)

// burst 模拟「一次编辑停顿」：把上次压栈时间往前推，让下一次 capture 真的开新的一步。
func burst(m *Model) {
	m.lastSnap = time.Now().Add(-time.Second)
	m.captureUndo()
}

// TestUndoRedoAcrossBursts 连改三次 → 撤销两次回到中间态 → 重做一次。
func TestUndoRedoAcrossBursts(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	entry := model.col.Entries[0]
	for _, item := range model.col.Entries {
		if item.Kind == explorer.KindHTTP {
			entry = item
			break
		}
	}
	model.openEntry(entry)
	original := model.req.url

	setURL := func(url string) {
		model.req.url = url
		model.markEdited()
		burst(model)
	}
	setURL("http://a")
	setURL("http://b")
	setURL("http://c")

	if len(model.undoStack) != 4 { // 打开时那一步 + 三次编辑
		t.Fatalf("撤销栈应有 4 层，实际 %d", len(model.undoStack))
	}
	model.undo()
	if model.req.url != "http://b" {
		t.Fatalf("撤销一次应回到 b，实际 %q", model.req.url)
	}
	model.undo()
	if model.req.url != "http://a" {
		t.Fatalf("撤销两次应回到 a，实际 %q", model.req.url)
	}
	model.redo()
	if model.req.url != "http://b" {
		t.Fatalf("重做一次应回到 b，实际 %q", model.req.url)
	}
	// 一直撤到底应回到打开请求时的样子。
	model.undo()
	model.undo()
	if model.req.url != original {
		t.Fatalf("撤到底应是原地址 %q，实际 %q", original, model.req.url)
	}
	if !model.req.dirty {
		t.Fatal("撤销后应保持脏标记（还没保存）")
	}
}

// TestUndoCoalescesTypingBurst 400ms 内的连续编辑合并成一步。
func TestUndoCoalescesTypingBurst(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	for _, item := range model.col.Entries {
		if item.Kind == explorer.KindHTTP {
			model.openEntry(item)
			break
		}
	}
	model.req.url = "a"
	model.markEdited()
	model.captureUndo()
	model.req.url = "ab"
	model.markEdited()
	model.captureUndo() // 400ms 内：覆盖栈顶，不新增一步

	if len(model.undoStack) != 2 {
		t.Fatalf("连续输入应合并成一步（栈 2 层），实际 %d", len(model.undoStack))
	}
	model.undo()
	if model.req.url == "ab" || model.req.url == "a" {
		t.Fatalf("撤销应跳过整段输入回到打开时，实际 %q", model.req.url)
	}
}

// TestUndoRestoresTables 撤销要把参数行也还原（深拷贝：栈里的行不能被后续编辑改掉）。
func TestUndoRestoresTables(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	for _, item := range model.col.Entries {
		if item.Kind == explorer.KindHTTP {
			model.openEntry(item)
			break
		}
	}
	model.req.params = []*kvRow{{enabled: true, name: "a", value: "1"}}
	model.markEdited()
	burst(model)

	model.req.params[0].name = "changed"
	model.req.params = append(model.req.params, &kvRow{enabled: true, name: "b", value: "2"})
	model.markEdited()
	burst(model)

	model.undo()
	if len(model.req.params) != 1 || model.req.params[0].name != "a" {
		t.Fatalf("撤销后参数行应还原成只有 a，实际 %+v", model.req.params[0])
	}
	if len(model.tables.params) == 0 {
		t.Fatal("派生表格应跟着重建")
	}
}

// TestUndoShortcutFromTheKeyboard 真实按键路径：在地址框里打字 → Ctrl+Z 回到原样。
func TestUndoShortcutFromTheKeyboard(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	original := model.req.url

	click(t, driver, "地址")
	pump(t, driver)
	if err := driver.Type("xyz"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if model.req.url == original {
		t.Fatalf("输入没进到地址：%q", model.req.url)
	}

	typed := model.req.url
	if err := driver.Chord(gfx.ModifierControl, "z"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if model.req.url != original {
		t.Fatalf("Ctrl+Z 应回到 %q，实际 %q", original, model.req.url)
	}

	// 重做：Ctrl+Shift+Z 把刚敲的字放回去。
	if err := driver.Chord(gfx.ModifierControl|gfx.ModifierShift, "z"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if model.req.url != typed {
		t.Fatalf("Ctrl+Shift+Z 应回到 %q，实际 %q", typed, model.req.url)
	}
}
