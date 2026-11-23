package drafts

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRoundTrip 存了能读回、删了就没了。
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Request, "/col/a", "用户/登录.yml", []byte("info:\n  name: 登录\n")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, ok := Load(dir, Request, "/col/a", "用户/登录.yml")
	if !ok || string(data) != "info:\n  name: 登录\n" {
		t.Fatalf("读回的草稿不对：%q ok=%v", data, ok)
	}
	// 换了集合根就是另一份草稿：不会串到别人的集合上。
	if _, ok := Load(dir, Request, "/col/b", "用户/登录.yml"); ok {
		t.Fatal("不同集合根不该共用草稿")
	}
	// 请求与文档分开：同一个相对路径互不覆盖。
	if _, ok := Load(dir, Doc, "/col/a", "用户/登录.yml"); ok {
		t.Fatal("文档草稿不该读到请求草稿")
	}
	Delete(dir, Request, "/col/a", "用户/登录.yml")
	if _, ok := Load(dir, Request, "/col/a", "用户/登录.yml"); ok {
		t.Fatal("删除后不该还能读到")
	}
}

// TestSaveIsAtomicAndPrivate 落盘的文件权限是 0600，且不留 .tmp。
func TestSaveIsAtomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Doc, "/col", "docs/指南.md", []byte("# 指南")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("应只有一个草稿文件，实际 %d 个", len(entries))
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("草稿权限应是 0600，实际 %v", info.Mode().Perm())
	}
	if filepath.Ext(entries[0].Name()) == ".tmp" {
		t.Fatal("不该留下临时文件")
	}
}

// TestEmptyInputsAreNoOps 空路径 / 空内容都是空操作，不报错也不建文件。
func TestEmptyInputsAreNoOps(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Request, "", "a.yml", []byte("x")); err != nil {
		t.Fatalf("空集合根应静默：%v", err)
	}
	if err := Save(dir, Request, "/col", "", []byte("x")); err != nil {
		t.Fatalf("空路径应静默：%v", err)
	}
	if err := Save(dir, Request, "/col", "a.yml", nil); err != nil {
		t.Fatalf("空内容应静默：%v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("不该写出文件：%v", entries)
	}
	if _, ok := Load("", Request, "/col", "a.yml"); ok {
		t.Fatal("空目录不该读到东西")
	}
}
