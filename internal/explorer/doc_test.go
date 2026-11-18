package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDocs(t *testing.T) {
	col, err := Load(filepath.Join("testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := map[string]*Entry{}
	for _, entry := range col.Entries {
		got[entry.Name] = entry
	}
	// 子目录 README 不是条目；根目录 README、frontmatter 名、文件名回退都在。
	if _, hidden := got["子目录 README"]; hidden {
		t.Fatalf("子目录 README 不应成为条目：%+v", col.Entries)
	}
	want := []string{"指南", "子目录文档", "README", "手写笔记"}
	if len(col.Entries) != len(want) {
		t.Fatalf("载入 %d 条，期望 %d：%+v", len(col.Entries), len(want), col.Entries)
	}
	for i, name := range want {
		if col.Entries[i].Name != name {
			t.Errorf("第 %d 条 = %q，期望 %q", i, col.Entries[i].Name, name)
		}
		if !col.Entries[i].IsDoc() {
			t.Errorf("%s 应是文档条目", name)
		}
	}
	guide := got["指南"]
	if guide.Seq != 1 || !strings.Contains(guide.Content, "带 frontmatter 的文档") {
		t.Fatalf("frontmatter/正文解析不对：%+v", guide)
	}
	if !strings.Contains(guide.Front, "uid: viva-poc-doc-guide") {
		t.Fatalf("原始 frontmatter 应保留：%q", guide.Front)
	}
	if got["手写笔记"].Content != "随手记\n" {
		t.Fatalf("无 frontmatter 时全文即正文：%q", got["手写笔记"].Content)
	}
	if !strings.HasPrefix(got["手写笔记"].UID, "md-") {
		t.Fatalf("无 uid 时应由路径哈希生成：%q", got["手写笔记"].UID)
	}
}

func TestCreateDocAddsSuffixInsteadOfOverwriting(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")

	first, err := CreateDoc(dir, "新建文档")
	if err != nil {
		t.Fatalf("CreateDoc: %v", err)
	}
	second, err := CreateDoc(dir, "新建文档")
	if err != nil {
		t.Fatalf("CreateDoc 第二次: %v", err)
	}
	if first != "docs/新建文档.md" || second != "docs/新建文档-2.md" {
		t.Fatalf("路径 = %q / %q", first, second)
	}
	body, err := os.ReadFile(filepath.Join(dir, "docs", "新建文档.md"))
	if err != nil {
		t.Fatalf("读新文档: %v", err)
	}
	if !strings.HasPrefix(string(body), "# 新建文档") {
		t.Fatalf("新文档正文 = %q", body)
	}
	if _, err := CreateDoc(dir, "../逃逸"); err == nil {
		t.Fatal("非法名字应被拒绝")
	}
}

func TestSaveDocKeepsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docs")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nuid: u1\nname: 标题\n---\n旧正文\n"
	if err := os.WriteFile(filepath.Join(path, "a.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	front := "---\nuid: u1\nname: 标题\n---\n"
	if err := SaveDoc(dir, "docs/a.md", front, "新正文\n"); err != nil {
		t.Fatalf("SaveDoc: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(path, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != front+"新正文\n" {
		t.Fatalf("保存结果 = %q", data)
	}
	info, err := os.Stat(filepath.Join(path, "a.md"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("权限应保持 0644：%v %v", info, err)
	}
	if err := SaveDoc(dir, "../escape.md", "", "x"); err == nil {
		t.Fatal("越界路径应被拒绝")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录 %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
}
