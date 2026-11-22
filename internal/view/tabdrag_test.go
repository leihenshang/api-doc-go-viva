package view

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
)

// threeTabCollection 造一个有三条请求的集合。
func threeTabCollection(t *testing.T) string {
	t.Helper()
	dir := writeCollection(t, "http://127.0.0.1:1")
	for index, name := range []string{"第二", "第三"} {
		body := fmt.Sprintf(`info:
  name: %s
  type: http
  seq: %d
meta:
  uid: tab-%d
http:
  method: GET
  url: http://127.0.0.1:1/%s
  body:
    type: none
`, name, index+2, index+2, name)
		if err := os.WriteFile(filepath.Join(dir, name+".yml"), []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	return dir
}

// openThreeTabs 打开集合里的三条请求，返回模型与窗口驱动。
func openThreeTabs(t *testing.T) (*Model, *app.Driver) {
	t.Helper()
	col, err := explorer.Load(threeTabCollection(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	pump(t, driver)
	for _, entry := range col.Entries {
		if entry.Kind == explorer.KindHTTP {
			model.openEntry(entry)
		}
	}
	// openEntry 是直接改模型（不经过界面事件），要自己叫一次重绘，
	// 否则 pump 什么都不做，标签条根本没画过——槽位身份也就没登记。
	model.invalidator.Invalidate()
	pump(t, driver)
	if len(model.tabs) < 3 {
		t.Fatalf("应打开 3 个标签，实际 %d", len(model.tabs))
	}
	return model, driver
}

// tabFiles 取标签当前顺序（按文件路径比较）。
func tabFiles(model *Model) []string {
	out := make([]string, 0, len(model.tabs))
	for _, tab := range model.tabs {
		out = append(out, tab.file)
	}
	return out
}

// TestMoveTabReorders 换位：顺序变，当前标签跟着同一条走。
func TestMoveTabReorders(t *testing.T) {
	model, driver := openThreeTabs(t)
	before := tabFiles(model)
	model.activateTab(0)
	pump(t, driver)
	first := model.tabs[0]

	// 语义是「占住目标标签原来的位置」：0 → 2 得到 b1,b2,b0,b3。
	model.moveTab(0, 2)
	after := tabFiles(model)
	want := []string{before[1], before[2], before[0], before[3]}
	if len(after) != len(want) {
		t.Fatalf("标签数变了：%v", after)
	}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("顺序不对：%v，期望 %v", after, want)
		}
	}
	if model.tabs[model.curTab] != first {
		t.Fatalf("当前标签该跟着走，现在 curTab=%d", model.curTab)
	}

	// 再把挪过去的那个拖回第 1 位。
	model.moveTab(2, 1)
	again := tabFiles(model)
	want = []string{before[1], before[0], before[2], before[3]}
	for i := range want {
		if again[i] != want[i] {
			t.Fatalf("回拖后顺序不对：%v，期望 %v", again, want)
		}
	}
	if model.tabs[model.curTab] != first {
		t.Fatal("回拖后当前标签也不该变")
	}
}

// TestDropTabByID 拖拽回调只给身份：靠本帧登记的身份反查槽位。
func TestDropTabByID(t *testing.T) {
	model, _ := openThreeTabs(t)
	before := tabFiles(model)

	// 身份由真实绘制产生，不能自己造（gfx.ID 的字段是私有的）。
	if len(model.tabSlotIDs) < 3 {
		t.Fatalf("一帧之后应登记 3 个槽位身份，实际 %d", len(model.tabSlotIDs))
	}
	first, third := model.tabSlotIDs[0], model.tabSlotIDs[2]
	if first.IsZero() || third.IsZero() || first == third {
		t.Fatalf("槽位身份不合法：%v / %v", first, third)
	}
	model.dropTab(first, third)
	got := tabFiles(model)
	want := []string{before[1], before[2], before[0], before[3]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("按身份拖拽后顺序不对：%v，期望 %v", got, want)
		}
	}
}

// TestCloseAllTabs 菜单里的「关闭全部」：标签清空、回到概览。
func TestCloseAllTabs(t *testing.T) {
	model, _ := openThreeTabs(t)
	model.closeAll()
	if len(model.tabs) != 0 || model.curTab != -1 || !model.overview {
		t.Fatalf("关闭全部后状态不对：tabs=%d cur=%d overview=%v",
			len(model.tabs), model.curTab, model.overview)
	}
}

// TestMoveTabMenuFallback 菜单里的「左移 / 右移」：文字上按不出拖拽时的兜底。
func TestMoveTabMenuFallback(t *testing.T) {
	model, _ := openThreeTabs(t)
	before := tabFiles(model)

	model.moveTab(1, 0)
	if got := tabFiles(model); got[0] != before[1] || got[1] != before[0] {
		t.Fatalf("左移不对：%v", got)
	}
	model.moveTab(0, 1)
	if got := tabFiles(model); got[0] != before[0] || got[1] != before[1] {
		t.Fatalf("右移没还原：%v", got)
	}
	// 越界不动。
	model.moveTab(0, -1)
	model.moveTab(len(model.tabs)-1, len(model.tabs))
	if got := tabFiles(model); got[0] != before[0] {
		t.Fatalf("越界不该改动：%v", got)
	}
}
