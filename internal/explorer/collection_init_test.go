package explorer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCollectionWritesManifestAndRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := InitCollection(dir, "新集合"); err != nil {
		t.Fatalf("InitCollection: %v", err)
	}
	col, err := Load(dir)
	if err != nil {
		t.Fatalf("初始化后应能打开: %v", err)
	}
	if col.Name != "新集合" {
		t.Fatalf("集合名 = %q", col.Name)
	}
	data, err := os.ReadFile(filepath.Join(dir, "opencollection.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("清单不应为空")
	}
	// 已有清单时拒绝，绝不覆盖别人的集合。
	if err := InitCollection(dir, "覆盖"); err == nil {
		t.Fatal("已有清单时应拒绝初始化")
	}
	if err := InitCollection(filepath.Join(dir, "不存在"), ""); err == nil {
		t.Fatal("不存在的目录应报错")
	}
}
