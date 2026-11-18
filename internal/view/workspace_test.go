package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// makeCollection 造一个只有一个请求的集合。
func makeCollection(t *testing.T, name, requestName string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"),
		"opencollection: 1.0.0\ninfo:\n  name: "+name+"\n")
	writeFile(t, filepath.Join(dir, requestName+".yml"), `info:
  name: `+requestName+`
  type: http
  seq: 1
meta:
  uid: u-`+requestName+`
http:
  method: GET
  url: '{{host}}/`+requestName+`'
  params: []
  headers: []
  body:
    type: none
`)
	return dir
}

// TestOpenCollectionSwitchesTree 打开另一个集合后，树换成新集合的条目并记住路径。
func TestOpenCollectionSwitchesTree(t *testing.T) {
	first := makeCollection(t, "甲", "旧接口")
	second := makeCollection(t, "乙", "新接口")

	col, err := explorer.Load(first)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	if !hasControl(driver, "GET 旧接口") {
		t.Fatalf("初始集合不对：%v", painted(driver))
	}

	model.openCollection(second)
	pump(t, driver)
	if !hasControl(driver, "GET 新接口") || hasControl(driver, "GET 旧接口") {
		t.Fatalf("打开后树没换：%v", painted(driver))
	}
	if settings.Load().LastCollection != second {
		t.Fatalf("上次打开的集合没记住：%q", settings.Load().LastCollection)
	}
}

// TestOpenEmptyDirectoryAsksBeforeInit 缺清单时先确认，确认后才写清单并打开。
func TestOpenEmptyDirectoryAsksBeforeInit(t *testing.T) {
	base := makeCollection(t, "起点", "接口")
	col, err := explorer.Load(base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openWindow(t, model)

	empty := t.TempDir()
	model.openCollection(empty)
	if !model.ui.initOpen {
		t.Fatal("缺清单应先弹初始化确认，不能直接打开")
	}
	if _, err := os.Stat(filepath.Join(empty, "opencollection.yml")); !os.IsNotExist(err) {
		t.Fatal("确认前不应写盘")
	}
	model.confirmInit()
	if model.ui.initOpen || model.col.Dir == base {
		t.Fatalf("确认后应切到新集合：%q", model.col.Dir)
	}
	if _, err := os.Stat(filepath.Join(empty, "opencollection.yml")); err != nil {
		t.Fatalf("初始化没有写清单: %v", err)
	}
}

// TestEnvRenameAndDelete 环境重命名会带走密钥文件；删除移进 .trash 并切回「无」。
func TestEnvRenameAndDelete(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 环境\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.yml"), "info:\n  name: dev\nuid: e1\nvars: []\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.secrets.yml"), "secrets:\n  token: t\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openWindow(t, model)
	model.applyEnv("dev")

	model.ui.envPick = "dev"
	model.ui.envRenameName = "staging"
	model.renameEnv()
	if model.envName != "staging" {
		t.Fatalf("重命名后当前环境 = %q", model.envName)
	}
	if _, err := os.Stat(filepath.Join(dir, "environments", "staging.secrets.yml")); err != nil {
		t.Fatalf("密钥文件应一起重命名: %v", err)
	}

	model.ui.envPick = "staging"
	model.deleteEnv()
	if model.envName != "" {
		t.Fatalf("删掉正在用的环境后应切回「无」：%q", model.envName)
	}
	names, err := explorer.ListEnvs(dir)
	if err != nil || len(names) != 0 {
		t.Fatalf("环境应已删除：%v %v", names, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".trash"))
	if err != nil || len(entries) < 2 {
		t.Fatalf("环境文件应移进 .trash：%v %v", entries, err)
	}
}
