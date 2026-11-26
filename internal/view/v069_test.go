package view

import (
	"os"
	"path/filepath"
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

// TestNewRequestValidatesName 新建请求的名称用 v0.6.9 的 validate 校验：
// 错误画在字段下方（FormField），空名不落盘，输入后错误消失。
func TestNewRequestValidatesName(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "新建请求")
	pump(t, driver)
	click(t, driver, "创建")
	pump(t, driver)
	if !shows(driver, "请求名不能为空") {
		t.Fatalf("空名应在字段下方报错：%v", painted(driver))
	}
	if !model.ui.newReqOpen {
		t.Fatal("校验没过时弹窗不该关")
	}
	if _, err := os.Stat(filepath.Join(col.Dir, ".yml")); err == nil {
		t.Fatal("空名不该写出文件")
	}

	clickText(t, driver, "例如：获取用户列表")
	pump(t, driver)
	if err := driver.Type("校验通过"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if shows(driver, "请求名不能为空") {
		t.Fatalf("输入后错误应消失：%v", painted(driver))
	}

	click(t, driver, "创建")
	pump(t, driver)
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(col.Dir, "校验通过.yml")); err != nil {
		t.Fatalf("校验通过后应落盘：%v", err)
	}
}
