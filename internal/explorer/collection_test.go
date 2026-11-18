package explorer

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestLoadRejectsDirectoryWithoutManifest(t *testing.T) {
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrNeedsInit) {
		t.Fatalf("缺清单应返回 ErrNeedsInit，得到 %v", err)
	}
}

func TestLoadSampleCollection(t *testing.T) {
	col, err := Load(filepath.Join("testdata", "sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if col.Name != "验证集合" {
		t.Fatalf("集合名 = %q", col.Name)
	}
	// 分组按 seq 在前，请求在本组内按 seq；根目录的请求排在最后。
	want := []string{"列表@用户", "创建@用户", "删除@订单", "健康检查@"}
	if len(col.Entries) != len(want) {
		t.Fatalf("载入 %d 条请求，期望 %d：%+v", len(col.Entries), len(want), col.Entries)
	}
	for i, w := range want {
		got := col.Entries[i].Name + "@" + col.Entries[i].Group
		if got != w {
			t.Errorf("第 %d 条 = %q，期望 %q", i, got, w)
		}
	}
	// 坏 YAML 只跳过，不让整个集合打不开。
	if len(col.Warnings) != 1 {
		t.Fatalf("warnings = %v，期望 1 条", col.Warnings)
	}
}

func TestEntryTitleAndMethod(t *testing.T) {
	col, err := Load(filepath.Join("testdata", "sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := map[string]string{"列表": "GET 列表", "删除": "DELETE 删除"}
	for _, req := range col.Entries {
		if want, ok := cases[req.Name]; ok && req.Title() != want {
			t.Errorf("%s 的标题 = %q，期望 %q", req.Name, req.Title(), want)
		}
	}
}

func TestLoadEnv(t *testing.T) {
	dir := filepath.Join("testdata", "sample")
	vars, err := LoadEnv(dir, "dev")
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	if vars["host"] != "http://127.0.0.1:18080" || vars["username"] != "admin" {
		t.Fatalf("环境变量 = %v", vars)
	}
	if vars["password"] != "s3cret" {
		t.Fatalf("密文未合并：%v", vars)
	}
	if _, ok := vars["off"]; ok {
		t.Fatalf("未启用的变量不应载入：%v", vars)
	}
	if _, err := LoadEnv(dir, "../etc"); err == nil {
		t.Fatal("非法环境名应被拒绝")
	}
	if _, err := LoadEnv(dir, "missing"); err == nil {
		t.Fatal("不存在的环境应报错")
	}
}
