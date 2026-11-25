package view

import (
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestToolbarSearch 顶栏搜索框点一下打开命令面板。
func TestToolbarSearch(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "全局搜索")
	pump(t, driver)
	if !model.ui.paletteOpen {
		t.Fatalf("点搜索框应打开命令面板：%v", painted(driver))
	}
}
