package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestTabTitlesNumberDuplicates 同名的标签按顺序加序号，唯一的保持原样。
func TestTabTitlesNumberDuplicates(t *testing.T) {
	model, _, _ := twoRoots(t)
	openNamed(t, model, "甲请求") // 唯一
	model.openScratch()
	model.openScratch()
	model.openScratch()

	labels := []string{
		model.tabTitleAt(0), model.tabTitleAt(1), model.tabTitleAt(2), model.tabTitleAt(3),
	}
	if labels[0] != "甲请求" {
		t.Fatalf("唯一的标签不该加序号：%q", labels[0])
	}
	for index := 1; index < 4; index++ {
		want := "临时请求 · " + string(rune('0'+index))
		if labels[index] != want {
			t.Fatalf("第 %d 个同名标签应是 %q，实际 %q", index, want, labels[index])
		}
	}
}

// TestCrossMoveKeepsTab 跨集合移动后，标签跟着到目标集合。
func TestCrossMoveKeepsTab(t *testing.T) {
	model, first, second := twoRoots(t)
	openNamed(t, model, "甲请求")
	if len(model.tabs) != 1 || model.col.Dir != first.Dir {
		t.Fatalf("前置状态不对：tabs=%d col=%s", len(model.tabs), model.col.Dir)
	}
	entry := model.col.Entries[0]
	moved, err := explorer.MoveEntryTo(first.Dir, second.Dir, entry, "")
	if err != nil {
		t.Fatalf("MoveEntryTo: %v", err)
	}

	model.followMovedTab(0, 1, "甲请求.yml", moved)
	if model.col.Dir != second.Dir || model.active != 1 {
		t.Fatalf("应切到目标集合：col=%s active=%d", model.col.Dir, model.active)
	}
	if len(model.roots[0].tabs) != 0 {
		t.Fatalf("源集合不该还留着这个标签：%d", len(model.roots[0].tabs))
	}
	if len(model.tabs) != 1 || model.tabs[0].file != moved {
		t.Fatalf("目标集合应打开搬过来的那条：%+v", model.tabs)
	}
}

// TestConflictDetectedAndResolved 外部改动先报冲突，再按用户选择覆盖或重载。
func TestConflictDetectedAndResolved(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openNamed(t, model, "健康检查")
	rel := model.req.file

	// 别处改了文件（模拟编辑器 / AI / 别的客户端）。
	external := "info:\n  name: 健康检查\n  type: http\n  seq: 1\nmeta:\n  uid: outside\nhttp:\n  method: GET\n  url: http://outside/other\n  body:\n    type: none\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(external), 0o644); err != nil {
		t.Fatalf("写外部改动: %v", err)
	}

	model.req.url = "http://127.0.0.1:1/mine"
	model.markEdited()
	model.saveRequest()
	if !model.req.conflict {
		t.Fatalf("应检测到冲突：err=%q", model.req.err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, rel))
	if strings.Contains(string(raw), "/mine") {
		t.Fatalf("冲突时不该写盘：\n%s", raw)
	}

	// 选「用我的覆盖」→ 磁盘变成本地版本，冲突标记清掉。
	model.forceSaveRequest()
	raw, _ = os.ReadFile(filepath.Join(dir, rel))
	if !strings.Contains(string(raw), "/mine") {
		t.Fatalf("覆盖后应是本地版本：\n%s", raw)
	}
	if model.req.conflict {
		t.Fatal("覆盖后不该还标着冲突")
	}
}

// TestReloadRequestFromDisk 冲突里选「重新加载」→ 编辑器回到磁盘内容。
func TestReloadRequestFromDisk(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openNamed(t, model, "健康检查")
	rel := model.req.file

	external := strings.Replace(readFileString(t, filepath.Join(dir, rel)),
		"health", "outside", 1)
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(external), 0o644); err != nil {
		t.Fatalf("写外部改动: %v", err)
	}
	model.req.url = "http://127.0.0.1:1/mine"
	model.markEdited()
	model.saveRequest()
	if !model.req.conflict {
		t.Fatal("应先报冲突")
	}
	model.reloadRequestFromDisk()
	if strings.Contains(model.req.url, "/mine") {
		t.Fatalf("重新加载后不该还是本地改动：%q", model.req.url)
	}
	if model.req.conflict {
		t.Fatal("重新加载后冲突标记应清掉")
	}
}

// readFileString 读文件成字符串（失败就返回空，测试里随后会断言失败）。
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s: %v", path, err)
	}
	return string(data)
}

// TestPaletteSearchesEntries 命令面板输入关键词能列出请求（不只是命令）。
func TestPaletteSearchesEntries(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openPalette()
	pump(t, driver)
	// 真的往输入框里打字：直接改模型会被控件在下一帧重置。
	clickText(t, driver, "输入命令名…")
	pump(t, driver)
	if err := driver.Type("健康"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)

	if !hasControl(driver, "打开 健康检查") {
		t.Fatalf("应列出匹配的请求：%v", painted(driver))
	}
	if !shows(driver, "打开") {
		t.Fatalf("应有「打开」分组：%v", painted(driver))
	}
}

// TestShortcutsDialog 快捷键卡能打开，且列的是真实和弦。
func TestShortcutsDialog(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openShortcuts()
	pump(t, driver)

	if !model.ui.shortcutsOpen {
		t.Fatal("应打开快捷键卡")
	}
	for _, want := range []string{"命令面板", "保存", "在响应正文里查找"} {
		if !shows(driver, want) {
			t.Fatalf("快捷键卡缺 %q：%v", want, painted(driver))
		}
	}
	// 主修饰键按平台显示（Linux/Windows 是 Ctrl）。
	if !shows(driver, "Ctrl+K") {
		t.Fatalf("应显示 Ctrl+K：%v", painted(driver))
	}
}
