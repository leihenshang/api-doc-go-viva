package view

import (
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestEmptyStateClearsSearch 树没有匹配项时用 v0.6.9 的 ui/empty，并在原地给出「清空搜索」。
func TestEmptyStateClearsSearch(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	if !shows(driver, "健康检查") {
		t.Fatalf("初始应列出请求：%v", painted(driver))
	}
	clickText(t, driver, "搜索请求…")
	pump(t, driver)
	if err := driver.Type("zzz"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if !shows(driver, "没有匹配的请求") || !shows(driver, "换个关键字") {
		t.Fatalf("应显示空状态：%v", painted(driver))
	}
	if !hasControl(driver, "清空搜索") {
		t.Fatalf("空状态应带「清空搜索」按钮：%v", painted(driver))
	}

	click(t, driver, "清空搜索")
	pump(t, driver)
	if model.filter != "" {
		t.Fatalf("清空搜索后 filter 应为空：%q", model.filter)
	}
	if !shows(driver, "健康检查") {
		t.Fatalf("清空后请求应回来：%v", painted(driver))
	}
}
