package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestSearchAndListDocs 检索命中名称 / 地址，文档清单带路径。
func TestSearchAndListDocs(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	hits := toolText(t, cs, "search_requests", map[string]any{"query": "login"})
	if !strings.Contains(hits, "用户/登录.yml") || !strings.Contains(hits, "POST") {
		t.Fatalf("按地址没搜到：%q", hits)
	}
	hits = toolText(t, cs, "search_requests", map[string]any{"query": "登录"})
	if !strings.Contains(hits, "用户/登录.yml") {
		t.Fatalf("按名称没搜到：%q", hits)
	}
	if miss := toolText(t, cs, "search_requests", map[string]any{"query": "不存在的东西"}); !strings.Contains(miss, "没有匹配") {
		t.Fatalf("没命中应给人话：%q", miss)
	}

	docs := toolText(t, cs, "list_docs", map[string]any{})
	if !strings.Contains(docs, "docs/指南.md") || !strings.Contains(docs, "指南") {
		t.Fatalf("文档清单不对：%q", docs)
	}
}

// TestGroupLifecycle 新建分组 → 改名 → 删除（进回收站）。
func TestGroupLifecycle(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	created := toolText(t, cs, "create_group", map[string]any{"name": "订单"})
	if !strings.Contains(created, "订单") {
		t.Fatalf("create_group = %q", created)
	}
	if _, err := os.Stat(filepath.Join(dir, "订单")); err != nil {
		t.Fatalf("目录没建出来：%v", err)
	}

	// 分组名存在 folder.yml 里：改名只改显示名，目录名不动（与界面一致）。
	if text := toolText(t, cs, "rename_group", map[string]any{"folder": "订单", "name": "交易"}); !strings.Contains(text, "交易") {
		t.Fatalf("rename_group = %q", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "订单", "folder.yml")); err != nil {
		t.Fatalf("目录不该跟着改名：%v", err)
	}
	renamed := findFolder(dir, "订单")
	if renamed == nil || renamed.Name != "交易" {
		t.Fatalf("folder.yml 里的名字没改：%+v", renamed)
	}

	if text := toolText(t, cs, "delete_group", map[string]any{"folder": "交易"}); !strings.Contains(text, "回收站") {
		t.Fatalf("delete_group = %q", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "交易")); err == nil {
		t.Fatal("删掉的分组还在")
	}
	if items, err := explorer.ListTrash(dir); err != nil || len(items) == 0 {
		t.Fatalf("回收站里应有东西：%v %+v", err, items)
	}
}

// TestDocLifecycle 新建（带正文）→ 追加 → 读 → 删。
func TestDocLifecycle(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	created := toolText(t, cs, "create_doc", map[string]any{"name": "接入指南", "content": "# 接入指南\n\n先申请 token。"})
	rel := strings.TrimSpace(strings.TrimPrefix(created, "已新建文档 "))
	if !strings.HasSuffix(rel, ".md") {
		t.Fatalf("create_doc 没给出路径：%q", created)
	}

	if text := toolText(t, cs, "update_doc", map[string]any{
		"path": rel, "content": "再配一个环境。", "append": true,
	}); !strings.Contains(text, "追加") {
		t.Fatalf("update_doc = %q", text)
	}
	body := toolText(t, cs, "get_doc", map[string]any{"path": rel})
	if !strings.Contains(body, "先申请 token") || !strings.Contains(body, "再配一个环境") {
		t.Fatalf("追加后正文不对：%q", body)
	}

	if text := toolText(t, cs, "delete_doc", map[string]any{"path": rel}); !strings.Contains(text, "回收站") {
		t.Fatalf("delete_doc = %q", text)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
		t.Fatal("删掉的文档还在")
	}
}

// TestExampleSaveDelete 存示例（自动带方法 / 地址）→ 列出 → 删。
func TestExampleSaveDelete(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	saved := toolText(t, cs, "save_example", map[string]any{
		"path": "用户/登录.yml", "name": "登录成功", "status": 200,
		"body": `{"token":"t-1"}`, "duration_ms": 12,
		"headers": map[string]any{"Content-Type": "application/json"},
	})
	if !strings.Contains(saved, "已保存示例") {
		t.Fatalf("save_example = %q", saved)
	}
	examples := explorer.Examples(dir, "用户/登录.yml")
	if len(examples) != 1 || examples[0].Name != "登录成功" || examples[0].Status != 200 {
		t.Fatalf("示例没落盘：%+v", examples)
	}
	if examples[0].URL != "http://example.invalid/login" || examples[0].Method != "POST" {
		t.Fatalf("示例应带上请求的方法与地址：%+v", examples[0])
	}

	list := toolText(t, cs, "list_examples", map[string]any{"path": "用户/登录.yml"})
	if !strings.Contains(list, "登录成功") {
		t.Fatalf("list_examples = %q", list)
	}

	if text := toolText(t, cs, "delete_example", map[string]any{
		"path": "用户/登录.yml", "name": "登录成功",
	}); !strings.Contains(text, "已删除示例") {
		t.Fatalf("delete_example = %q", text)
	}
	if left := explorer.Examples(dir, "用户/登录.yml"); len(left) != 0 {
		t.Fatalf("删除后还有示例：%+v", left)
	}
}

// TestEnvLifecycle 新建（带初始变量）→ 改名 → 列 → 删。
func TestEnvLifecycle(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	if text := toolText(t, cs, "create_env", map[string]any{
		"name": "staging", "vars": map[string]any{"host": "https://s.example"},
	}); !strings.Contains(text, "staging") {
		t.Fatalf("create_env = %q", text)
	}
	if text := toolText(t, cs, "rename_env", map[string]any{"old": "staging", "new": "pre"}); !strings.Contains(text, "pre") {
		t.Fatalf("rename_env = %q", text)
	}
	envs := toolText(t, cs, "list_envs", map[string]any{})
	if !strings.Contains(envs, "pre") || !strings.Contains(envs, "https://s.example") {
		t.Fatalf("改名后环境不对：%q", envs)
	}
	if text := toolText(t, cs, "delete_env", map[string]any{"name": "pre"}); !strings.Contains(text, "已删除环境") {
		t.Fatalf("delete_env = %q", text)
	}
	if names, err := explorer.ListEnvs(dir); err == nil && len(names) != 0 {
		t.Fatalf("删除后还剩环境：%v", names)
	}
}

// TestDeleteRequestsBatch 批量删除：成功的报路径，找不到的单独列出来。
func TestDeleteRequestsBatch(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	text := toolText(t, cs, "delete_requests", map[string]any{
		"paths": []any{"用户/登录.yml", "不存在.yml"},
	})
	if !strings.Contains(text, "已移入回收站 1 条") {
		t.Fatalf("批量结果不对：%q", text)
	}
	if !strings.Contains(text, "不存在.yml") {
		t.Fatalf("应列出没处理的条目：%q", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "用户", "登录.yml")); err == nil {
		t.Fatal("请求还在原处")
	}
}
