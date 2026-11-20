package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// TestSaveAndListExamples 示例落进 examples/<请求基名>/，新的在前，同名自动去重。
func TestSaveAndListExamples(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "用户", "查询.yml"), `info:
  name: 查询
  type: http
  seq: 1
http:
  method: GET
  url: http://127.0.0.1:1/users
`)

	snap := Example{
		Method: "GET", URL: "http://127.0.0.1:1/users",
		Status: 200, DurationMS: 12, Size: 9, ContentType: "application/json",
		Body:    `{"ok":true}`,
		Headers: []collection.KV{{Name: "X-Trace-Id", Value: "t-1", Enabled: true}},
	}
	first, err := SaveExample(dir, "用户/查询.yml", snap)
	if err != nil {
		t.Fatalf("SaveExample: %v", err)
	}
	if first != "examples/用户/查询/01-01 00:00:20 · 200.yml" && !strings.HasPrefix(first, "examples/用户/查询/") {
		t.Fatalf("落点 = %q", first)
	}
	// 第二次：同名场景（这里第二个手动起名，逼出去重逻辑）。
	snap.Name = strings.TrimSuffix(filepath.Base(first), ".yml")
	second, err := SaveExample(dir, "用户/查询.yml", snap)
	if err != nil {
		t.Fatalf("SaveExample 第二次: %v", err)
	}
	if second == first {
		t.Fatalf("同名应去重，实际都是 %q", first)
	}
	if !strings.Contains(second, " (2)") {
		t.Fatalf("去重后缀不对：%q", second)
	}

	list := Examples(dir, "用户/查询.yml")
	if len(list) != 2 {
		t.Fatalf("应有两个示例：%+v", list)
	}
	// 新的在前：seq 大的排前面。
	if list[0].File != second || list[1].File != first {
		t.Fatalf("排序不对：%+v", list)
	}
	if list[0].Body != `{"ok":true}` || list[0].Status != 200 {
		t.Fatalf("内容没存全：%+v", list[0])
	}
	if len(list[0].Headers) != 1 || list[0].Headers[0].Name != "X-Trace-Id" {
		t.Fatalf("响应头没存：%+v", list[0].Headers)
	}

	// examples 目录不算分组：集合里只有「用户」一个分组。
	col, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, folder := range col.Folders {
		if folder.Dir == "examples" || strings.HasPrefix(folder.Dir, "examples/") {
			t.Fatalf("examples 不该出现在树里：%+v", col.Folders)
		}
	}

	if err := DeleteExample(dir, second); err != nil {
		t.Fatalf("DeleteExample: %v", err)
	}
	if left := Examples(dir, "用户/查询.yml"); len(left) != 1 || left[0].File != first {
		t.Fatalf("删除后剩余不对：%+v", left)
	}
}

// TestExamplesIgnoreBrokenFiles 坏文件跳过，不影响其它示例，也不让集合打不开。
func TestExamplesIgnoreBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "查询.yml"), "info:\n  name: 查询\n  type: http\nhttp:\n  method: GET\n  url: http://x\n")
	write(t, filepath.Join(dir, "examples", "查询", "坏的.yml"), "info: [\n")
	if _, err := SaveExample(dir, "查询.yml", Example{Status: 200, Body: "ok"}); err != nil {
		t.Fatalf("SaveExample: %v", err)
	}
	list := Examples(dir, "查询.yml")
	if len(list) != 1 {
		t.Fatalf("坏文件应被跳过，只剩一条：%+v", list)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "examples", "查询")); err != nil || len(entries) != 2 {
		t.Fatalf("磁盘上应有两个文件（一个坏的）：%v %v", entries, err)
	}
}
