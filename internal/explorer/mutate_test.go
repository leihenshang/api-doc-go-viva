package explorer

import (
	"fmt"
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

func TestArrangeEntriesRenumbersSeq(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	for name, seq := range map[string]int{"一": 1, "二": 2, "三": 3} {
		write(t, filepath.Join(dir, name+".yml"), fmt.Sprintf(`info:
  name: %s
  type: http
  seq: %d
meta:
  uid: uid-%s
http:
  method: GET
  url: ''
docs: ''
x-keep: 保留
`, name, seq, name))
	}
	// 文档只改 frontmatter 的 seq，name/uid/扩展行照旧。
	write(t, filepath.Join(dir, "指南.md"), `---
uid: md-1
name: 指南
seq: 1
icon: book
---
正文
`)

	err := ArrangeEntries(dir, []string{"指南.md", "三.yml", "一.yml", "二.yml"})
	if err != nil {
		t.Fatalf("ArrangeEntries: %v", err)
	}
	ordered := []string{"指南.md", "三.yml", "一.yml", "二.yml"}
	for index, rel := range ordered {
		data, readErr := os.ReadFile(filepath.Join(dir, rel))
		if readErr != nil {
			t.Fatalf("读 %s: %v", rel, readErr)
		}
		if !strings.Contains(string(data), fmt.Sprintf("seq: %d", index+1)) {
			t.Fatalf("%s 的 seq 没改成 %d：\n%s", rel, index+1, data)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "指南.md"))
	for _, want := range []string{"uid: md-1", "name: 指南", "icon: book", "---\n正文"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("文档 frontmatter 丢了 %q：\n%s", want, data)
		}
	}
	request, _ := os.ReadFile(filepath.Join(dir, "三.yml"))
	text := string(request)
	if !strings.Contains(text, "x-keep: 保留") || !strings.Contains(text, "uid-三") {
		t.Fatalf("请求的未知字段/uid 丢了：\n%s", request)
	}
	// 只换 seq 的值，2 空格缩进保持原样（整体重编码会变成 4 空格）。
	if !strings.Contains(text, "\n  seq: 2\n") {
		t.Fatalf("seq 行以外的内容被改写了：\n%s", request)
	}
}

func TestArrangeFoldersWritesSeq(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "a", "folder.yml"), "info:\n  name: 甲\n  seq: 1\nmeta:\n  uid: u-a\n")
	write(t, filepath.Join(dir, "b", "folder.yml"), "info:\n  name: 乙\n  seq: 2\n")
	write(t, filepath.Join(dir, "c", "keep.yml"), "info:\n  name: 占位\n  type: http\n  seq: 1\n")

	if err := ArrangeFolders(dir, []string{"c", "a", "b"}); err != nil {
		t.Fatalf("ArrangeFolders: %v", err)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a", "folder.yml"))
	text := string(a)
	if !strings.Contains(text, "\n  seq: 2\n") || !strings.Contains(text, "uid: u-a") {
		t.Fatalf("已有 folder.yml 被改坏：\n%s", text)
	}
	// 没有 folder.yml 的目录补一个最小文件，name 取目录名。
	c, _ := os.ReadFile(filepath.Join(dir, "c", "folder.yml"))
	if !strings.Contains(string(c), "name: c") || !strings.Contains(string(c), "seq: 1") {
		t.Fatalf("缺省 folder.yml 不对：\n%s", c)
	}

	col, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(col.Folders) != 3 || col.Folders[0].Dir != "c" || col.Folders[1].Dir != "a" {
		t.Fatalf("分组顺序没落盘：%+v", col.Folders)
	}
}

func TestMoveFolderReparentsAndRejectsDescendant(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "a", "b", "folder.yml"), "info:\n  name: 子\n  seq: 1\n")
	write(t, filepath.Join(dir, "a", "b", "请求.yml"), "info:\n  name: 请求\n  type: http\n  seq: 1\nhttp:\n  method: GET\n  url: ''\n")
	write(t, filepath.Join(dir, "c", "folder.yml"), "info:\n  name: 目标\n  seq: 1\n")

	moved, err := MoveFolder(dir, &Folder{Dir: "a/b"}, "c")
	if err != nil {
		t.Fatalf("MoveFolder: %v", err)
	}
	if moved != "c/b" {
		t.Fatalf("新路径 = %q", moved)
	}
	if _, err := os.Stat(filepath.Join(dir, "c", "b", "请求.yml")); err != nil {
		t.Fatalf("子文件没跟着移动: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b")); !os.IsNotExist(err) {
		t.Fatal("旧目录还在")
	}
	// 移进自己的子孙必须拒绝。
	if _, err := MoveFolder(dir, &Folder{Dir: "c"}, "c/b"); err == nil {
		t.Fatal("不应允许把分组移进自己的子孙")
	}
}

func TestCreateAndRenameFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "已有", "folder.yml"), "info:\n  name: 已有\n  type: folder\n  seq: 3\nmeta:\n  uid: keep-me\n")

	created, err := CreateFolder(dir, "", "新分组")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if created != "新分组" {
		t.Fatalf("路径 = %q", created)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "新分组", "folder.yml"))
	text := string(data)
	for _, want := range []string{"name: 新分组", "type: folder", "seq: 4", "uid: "} {
		if !strings.Contains(text, want) {
			t.Fatalf("folder.yml 缺 %q：\n%s", want, text)
		}
	}
	// 重名补 -2。
	second, err := CreateFolder(dir, "", "新分组")
	if err != nil || second != "新分组-2" {
		t.Fatalf("重名 = %q %v", second, err)
	}

	// 重命名只改 info.name，其余行与目录名不动。
	if err := RenameFolder(dir, &Folder{Dir: "已有"}, "改名后"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "已有", "folder.yml"))
	text = string(data)
	if !strings.Contains(text, "name: 改名后") || !strings.Contains(text, "seq: 3") || !strings.Contains(text, "uid: keep-me") {
		t.Fatalf("重命名改坏了 folder.yml：\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "已有")); err != nil {
		t.Fatalf("重命名不该动目录名: %v", err)
	}
}

func TestCrossCollectionMoves(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	for _, dir := range []string{src, dst} {
		write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	}
	write(t, filepath.Join(src, "请求.yml"), "info:\n  name: 请求\n  type: http\n  seq: 1\nmeta:\n  uid: u-1\nhttp:\n  method: GET\n  url: ''\n")
	write(t, filepath.Join(dst, "已有.yml"), "info:\n  name: 已有\n  type: http\n  seq: 1\n")
	write(t, filepath.Join(dst, "分组", "folder.yml"), "info:\n  name: 分组\n  seq: 1\n")

	// 跨集合搬到目标根；目标同名文件补 -2，源文件消失。
	write(t, filepath.Join(src, "已有.yml"), "info:\n  name: 已有\n  type: http\n  seq: 2\n")
	moved, err := MoveEntryTo(src, dst, &Entry{File: "已有.yml"}, "")
	if err != nil {
		t.Fatalf("MoveEntryTo: %v", err)
	}
	if moved != "已有-2.yml" {
		t.Fatalf("重名路径 = %q", moved)
	}
	if _, err := os.Stat(filepath.Join(src, "已有.yml")); !os.IsNotExist(err) {
		t.Fatal("源文件还在")
	}
	if _, err := os.Stat(filepath.Join(dst, "已有-2.yml")); err != nil {
		t.Fatalf("目标文件不在: %v", err)
	}

	// 跨集合搬整个分组：请求跟着走。
	write(t, filepath.Join(src, "整组", "folder.yml"), "info:\n  name: 整组\n  seq: 1\n")
	write(t, filepath.Join(src, "整组", "内部.yml"), "info:\n  name: 内部\n  type: http\n  seq: 1\n")
	movedFolder, err := MoveFolderTo(src, dst, &Folder{Dir: "整组"}, "分组")
	if err != nil {
		t.Fatalf("MoveFolderTo: %v", err)
	}
	if movedFolder != "分组/整组" {
		t.Fatalf("分组新路径 = %q", movedFolder)
	}
	if _, err := os.Stat(filepath.Join(dst, "分组", "整组", "内部.yml")); err != nil {
		t.Fatalf("分组内容没跟过去: %v", err)
	}
	// 目标已有同名分组时按客户端语义拒绝，不悄悄改目录名。
	write(t, filepath.Join(src, "整组", "folder.yml"), "info:\n  name: 整组\n  seq: 1\n")
	if _, err := MoveFolderTo(src, dst, &Folder{Dir: "整组"}, "分组"); err == nil {
		t.Fatal("目标已有同名分组时应报错")
	}
}

func TestTrashFolderRoundsTrip(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "待删", "folder.yml"), "info:\n  name: 待删\n  seq: 1\n")
	write(t, filepath.Join(dir, "待删", "请求.yml"), "info:\n  name: 请求\n  type: http\n  seq: 1\n")

	if err := TrashFolder(dir, &Folder{Dir: "待删"}); err != nil {
		t.Fatalf("TrashFolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "待删")); !os.IsNotExist(err) {
		t.Fatal("分组还在")
	}
	items, err := ListTrash(dir)
	if err != nil || len(items) != 1 {
		t.Fatalf("回收站应有一个分组：%+v %v", items, err)
	}
	if items[0].Name != "待删" {
		t.Fatalf("回收站显示名 = %q", items[0].Name)
	}
	restored, err := RestoreTrash(dir, items[0].File)
	if err != nil {
		t.Fatalf("RestoreTrash: %v", err)
	}
	if restored != "待删" {
		t.Fatalf("恢复路径 = %q", restored)
	}
	if _, err := os.Stat(filepath.Join(dir, "待删", "请求.yml")); err != nil {
		t.Fatalf("恢复后的内容不在: %v", err)
	}
}

func TestCloneFolderRegeneratesUIDs(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	write(t, filepath.Join(dir, "甲", "folder.yml"), "info:\n  name: 甲\n  type: folder\n  seq: 2\nmeta:\n  uid: folder-old\n")
	write(t, filepath.Join(dir, "甲", "请求.yml"), "info:\n  name: 请求\n  type: http\n  seq: 1\nmeta:\n  uid: req-old\nhttp:\n  method: GET\n  url: ''\nx-keep: 保留\n")
	write(t, filepath.Join(dir, "甲", "说明.md"), "---\nuid: md-old\nname: 说明\n---\n正文\n")
	write(t, filepath.Join(dir, "甲", "子", "folder.yml"), "info:\n  name: 子\n  seq: 1\nmeta:\n  uid: sub-old\n")
	write(t, filepath.Join(dir, "甲", "子", "内部.yml"), "info:\n  name: 内部\n  type: http\n  seq: 1\nmeta:\n  uid: inner-old\n")

	clone, err := CloneFolder(dir, &Folder{Dir: "甲", Name: "甲"})
	if err != nil {
		t.Fatalf("CloneFolder: %v", err)
	}
	if clone != "甲 副本1" {
		t.Fatalf("副本路径 = %q", clone)
	}
	top, _ := os.ReadFile(filepath.Join(dir, clone, "folder.yml"))
	if !strings.Contains(string(top), "name: 甲 副本1") || strings.Contains(string(top), "folder-old") {
		t.Fatalf("顶层 folder.yml 不对：\n%s", top)
	}
	req, _ := os.ReadFile(filepath.Join(dir, clone, "请求.yml"))
	text := string(req)
	if strings.Contains(text, "req-old") || !strings.Contains(text, "x-keep: 保留") {
		t.Fatalf("请求 uid 没换或其余字段丢了：\n%s", text)
	}
	doc, _ := os.ReadFile(filepath.Join(dir, clone, "说明.md"))
	if strings.Contains(string(doc), "md-old") || !strings.Contains(string(doc), "正文") {
		t.Fatalf("文档 uid 没换：\n%s", doc)
	}
	sub, _ := os.ReadFile(filepath.Join(dir, clone, "子", "folder.yml"))
	if strings.Contains(string(sub), "sub-old") || !strings.Contains(string(sub), "name: 子") {
		t.Fatalf("子分组 folder.yml 不对：\n%s", sub)
	}
	inner, _ := os.ReadFile(filepath.Join(dir, clone, "子", "内部.yml"))
	if strings.Contains(string(inner), "inner-old") {
		t.Fatalf("子请求 uid 没换：\n%s", inner)
	}
	// 原分组不受影响。
	original, _ := os.ReadFile(filepath.Join(dir, "甲", "请求.yml"))
	if !strings.Contains(string(original), "req-old") {
		t.Fatal("原分组被改了")
	}
}
