package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRequestWritesClientDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")

	rel, err := CreateRequest(dir, "", "获取用户", "post", "{{host}}/api/users")
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if rel != "获取用户.yml" {
		t.Fatalf("路径 = %q", rel)
	}
	col, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(col.Entries) != 1 {
		t.Fatalf("应有一条请求：%+v", col.Entries)
	}
	entry := col.Entries[0]
	if entry.Method != "POST" || entry.URL != "{{host}}/api/users" {
		t.Fatalf("方法/URL = %q %q", entry.Method, entry.URL)
	}
	block := entry.Payload.HTTP
	if len(block.Params) != 1 || !block.Params[0].Enabled {
		t.Fatalf("应有一行启用的空参数：%+v", block.Params)
	}
	if len(block.Headers) != 1 || block.Headers[0].Name != "Content-Type" || !block.Headers[0].Enabled {
		t.Fatalf("默认请求头不对：%+v", block.Headers)
	}
	if block.Body.Type != "none" {
		t.Fatalf("默认请求体应为 none：%q", block.Body.Type)
	}
	if entry.Payload.Meta.UID == "" {
		t.Fatal("应写入 uid")
	}

	// 重名自动加 -2，绝不覆盖。
	second, err := CreateRequest(dir, "", "获取用户", "GET", "")
	if err != nil || second != "获取用户-2.yml" {
		t.Fatalf("重名 = %q %v", second, err)
	}
	if _, err := CreateRequest(dir, "", "../逃逸", "GET", ""); err == nil {
		t.Fatal("非法名称应被拒绝")
	}
	if _, err := CreateRequest(dir, "不存在的分组", "x", "GET", ""); err == nil {
		t.Fatal("不存在的分组应被拒绝")
	}
}

func TestRenameEntryKeepsFileName(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	if _, err := CreateRequest(dir, "", "旧名", "GET", ""); err != nil {
		t.Fatal(err)
	}
	col, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := col.Entries[0]
	if err := RenameEntry(dir, entry, "新名"); err != nil {
		t.Fatalf("RenameEntry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "旧名.yml")); err != nil {
		t.Fatalf("文件名不应改变: %v", err)
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Entries[0].Name != "新名" {
		t.Fatalf("显示名 = %q", reloaded.Entries[0].Name)
	}
	if reloaded.Entries[0].Payload.Meta.UID != entry.Payload.Meta.UID {
		t.Fatal("uid 不应改变")
	}
}

func TestRenameDocKeepsFrontmatterKeys(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "docs", "a.md"), "---\nuid: u1\nseq: 3\n---\n正文\n")
	col, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameEntry(dir, col.Entries[0], "新标题"); err != nil {
		t.Fatalf("RenameEntry: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "name: 新标题") || !strings.Contains(text, "seq: 3") || !strings.Contains(text, "正文") {
		t.Fatalf("frontmatter/正文被破坏：%q", text)
	}
}

func TestTrashEntryMovesIntoTrashDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "a.md"), "# A\n")
	col, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := TrashEntry(dir, col.Entries[0]); err != nil {
		t.Fatalf("TrashEntry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); !os.IsNotExist(err) {
		t.Fatal("原文件应已移走")
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".trash"))
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".a.md") {
		t.Fatalf("回收站内容 = %v %v", entries, err)
	}
	// 回收站不参与扫描。
	reloaded, err := Load(dir)
	if err != nil || len(reloaded.Entries) != 0 {
		t.Fatalf("回收站不应再出现在树里：%+v %v", reloaded.Entries, err)
	}
}
