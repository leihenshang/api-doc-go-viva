package view

import (
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// openNamed 打开当前集合里名字含 name 的请求（测试里的「点树上的行」）。
func openNamed(t *testing.T, model *Model, name string) {
	t.Helper()
	for _, entry := range model.col.Entries {
		if entry.Kind == explorer.KindHTTP && strings.Contains(entry.Name, name) {
			model.openEntry(entry)
			return
		}
	}
	t.Fatalf("集合 %s 里没有名字含 %q 的请求", model.col.Name, name)
}

// twoRoots 造两个各有一条请求的集合，返回模型与两个集合根。
func twoRoots(t *testing.T) (*Model, *explorer.Collection, *explorer.Collection) {
	t.Helper()
	first, err := explorer.Load(makeCollection(t, "甲", "甲请求"))
	if err != nil {
		t.Fatalf("Load 甲: %v", err)
	}
	second, err := explorer.Load(makeCollection(t, "乙", "乙请求"))
	if err != nil {
		t.Fatalf("Load 乙: %v", err)
	}
	model := New(first, "", nil)
	model.AddRoot(second)
	return model, first, second
}

// TestTabsSurviveRootSwitch 切到别的根再切回来，标签、当前标签、正文都还在。
func TestTabsSurviveRootSwitch(t *testing.T) {
	model, first, second := twoRoots(t)

	// 在甲里开一条请求。
	openNamed(t, model, "甲请求")
	if len(model.tabs) != 1 || !strings.HasSuffix(model.req.url, "/甲请求") {
		t.Fatalf("甲里应打开一条：tabs=%d url=%q", len(model.tabs), model.req.url)
	}
	甲URL := model.req.url

	// 切到乙：甲的标签不该被销毁，只是暂时不显示。
	model.ActivateCollection(second.Dir)
	if len(model.tabs) != 0 {
		t.Fatalf("乙里还没有标签，实际 %d 个", len(model.tabs))
	}
	openNamed(t, model, "乙请求")
	if len(model.tabs) != 1 || !strings.HasSuffix(model.req.url, "/乙请求") {
		t.Fatalf("乙里应打开一条：tabs=%d url=%q", len(model.tabs), model.req.url)
	}

	// 切回甲：刚才那条还在，且是当前标签。
	model.ActivateCollection(first.Dir)
	if len(model.tabs) != 1 {
		t.Fatalf("切回甲后标签应还在：%d", len(model.tabs))
	}
	if model.req.url != 甲URL {
		t.Fatalf("切回甲后正文不对：%q，期望 %q", model.req.url, 甲URL)
	}
	if model.overview {
		t.Fatal("有标签时不该回到概览页")
	}

	// 再切回乙：乙自己的标签也还在。
	model.ActivateCollection(second.Dir)
	if len(model.tabs) != 1 || !strings.HasSuffix(model.req.url, "/乙请求") {
		t.Fatalf("乙的标签丢了：tabs=%d url=%q", len(model.tabs), model.req.url)
	}
}

// TestEachRootKeepsItsCurrentTab 两个根各留自己的当前标签，互不覆盖。
func TestEachRootKeepsItsCurrentTab(t *testing.T) {
	model, first, second := twoRoots(t)

	// 甲里开三个（一条真请求 + 两个临时请求），停在第一个。
	openNamed(t, model, "甲请求")
	model.openScratch()
	model.openScratch()
	if len(model.tabs) != 3 {
		t.Fatalf("甲应有 3 个标签：%d", len(model.tabs))
	}
	model.activateTab(0)
	if model.curTab != 0 {
		t.Fatalf("甲当前标签应是 0：%d", model.curTab)
	}

	// 乙里开一个，停在它上面。
	model.ActivateCollection(second.Dir)
	openNamed(t, model, "乙请求")
	if model.curTab != 0 || len(model.tabs) != 1 {
		t.Fatalf("乙应是 1 个标签且当前 0：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}

	// 回甲：当前标签仍是 0，标签数仍是 3。
	model.ActivateCollection(first.Dir)
	if len(model.tabs) != 3 || model.curTab != 0 {
		t.Fatalf("甲的标签与当前项没恢复：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}
}

// TestCloseOtherTabs 溢出菜单里的「关闭其他」保留当前标签。
func TestCloseOtherTabs(t *testing.T) {
	model, _, _ := twoRoots(t)
	openNamed(t, model, "甲请求")
	model.openScratch()
	model.openScratch()
	model.activateTab(1)
	keep := model.tabs[1]

	model.closeOtherTabs()
	if len(model.tabs) != 1 || model.tabs[0] != keep || model.curTab != 0 {
		t.Fatalf("关闭其他后应只剩当前标签：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}
	if model.overview {
		t.Fatal("还剩一个标签时不该回到概览")
	}
}

// TestCloseRootDropsItsTabs 关掉一个根，它的标签跟着走；另一个根的标签不受影响。
func TestCloseRootDropsItsTabs(t *testing.T) {
	model, first, second := twoRoots(t)
	openNamed(t, model, "甲请求")
	model.ActivateCollection(second.Dir)
	openNamed(t, model, "乙请求")

	// 关掉甲（index 0）：应该切到乙，乙的标签还在。
	model.closeRoot(0)
	if len(model.roots) != 1 || model.col.Dir != second.Dir {
		t.Fatalf("关掉甲后应只剩乙：roots=%d col=%s", len(model.roots), model.col.Dir)
	}
	if len(model.tabs) != 1 || !strings.HasSuffix(model.req.url, "/乙请求") {
		t.Fatalf("乙的标签不该受影响：tabs=%d url=%q", len(model.tabs), model.req.url)
	}

	// 重新加回甲：它是新根，标签是空的。
	model.AddRoot(first)
	model.ActivateCollection(first.Dir)
	if len(model.tabs) != 0 || !model.overview {
		t.Fatalf("重新打开的根应没有标签：tabs=%d overview=%v", len(model.tabs), model.overview)
	}
}
