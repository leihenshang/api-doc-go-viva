package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeCollection 造一个最小集合：清单 + 一条请求 + 一篇文档。
func writeCollection(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("建目录 %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("写文件 %s: %v", full, err)
		}
	}
	write("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 验证集合\n")
	write("用户/登录.yml", `info:
  name: 登录
  type: http
  seq: 1
summary: 账号密码换令牌
http:
  method: POST
  url: http://example.invalid/login
  headers:
    - name: Content-Type
      value: application/json
      enabled: true
  params:
    - name: page
      value: "1"
      enabled: true
  body:
    type: json
    raw: '{"user":"a"}'
docs: |-
  ## 说明
  登录接口。
`)
	write("docs/指南.md", "---\nname: 指南\n---\n# 指南\n正文。\n")
	return dir
}

// openSession 建一个内存传输的 MCP 会话（与服务端真实握手）。
func openSession(t *testing.T, dirs func() []string) *sdk.ClientSession {
	t.Helper()
	server, err := NewServer(Config{Service: &Service{Roots: dirs}})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t1, t2 := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// toolText 调用一个工具并取出第一段文本。
func toolText(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("调用 %s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s 没有返回内容", name)
	}
	text, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("%s 返回的不是文本：%T", name, res.Content[0])
	}
	return text.Text
}

// TestRegistersTheFiveTools 工具集与 Wails 版的只读 + 发送子集一致。
func TestRegistersTheFiveTools(t *testing.T) {
	cs := openSession(t, func() []string { return nil })
	res, err := cs.ListTools(context.Background(), &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"list_workspaces", "list_requests", "get_request", "get_doc", "send_request"} {
		if !got[want] {
			t.Fatalf("缺少工具 %s（实际 %v）", want, got)
		}
	}
}

// TestListAndReadTools 走一遍读取链路：工作区 → 列表 → 请求定义 → 文档。
func TestListAndReadTools(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })

	list := toolText(t, cs, "list_workspaces", nil)
	if !strings.Contains(list, "ws_1") || !strings.Contains(list, "验证集合") {
		t.Fatalf("list_workspaces = %q", list)
	}
	if strings.Contains(list, dir) {
		t.Fatalf("工作区列表不该暴露真实路径：%q", list)
	}

	entries := toolText(t, cs, "list_requests", map[string]any{})
	if !strings.Contains(entries, "用户/登录.yml") || !strings.Contains(entries, "docs/指南.md") {
		t.Fatalf("list_requests = %q", entries)
	}

	request := toolText(t, cs, "get_request", map[string]any{"path": "用户/登录.yml"})
	for _, want := range []string{"POST", "http://example.invalid/login", "Content-Type", "page", `{"user":"a"}`} {
		if !strings.Contains(request, want) {
			t.Fatalf("get_request 缺少 %q：\n%s", want, request)
		}
	}

	doc := toolText(t, cs, "get_doc", map[string]any{"path": "docs/指南.md"})
	if !strings.Contains(doc, "# 指南") {
		t.Fatalf("get_doc = %q", doc)
	}
}

// TestPathEscapeRejected 工具入参只能是集合内的相对路径。
func TestPathEscapeRejected(t *testing.T) {
	dir := writeCollection(t)
	cs := openSession(t, func() []string { return []string{dir} })
	for _, bad := range []string{"../secret.yml", "/etc/passwd"} {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{
			Name: "get_request", Arguments: map[string]any{"path": bad},
		})
		// 工具错误由 SDK 包成 IsError 结果（也可能直接返回 error），两种都算拒绝。
		if err == nil && res != nil && !res.IsError {
			t.Fatalf("路径 %q 应被拒绝", bad)
		}
	}
}

// TestNoWorkspaceIsAnAnswer 没打开集合时给出可读提示而不是空指针。
func TestNoWorkspaceIsAnAnswer(t *testing.T) {
	cs := openSession(t, func() []string { return nil })
	text := toolText(t, cs, "list_workspaces", nil)
	if !strings.Contains(text, "没有可访问的集合") {
		t.Fatalf("list_workspaces = %q", text)
	}
}

// TestReadOnlyHidesWriteTools 只读模式：写工具与发送工具都不注册。
func TestReadOnlyHidesWriteTools(t *testing.T) {
	svc := &Service{Roots: func() []string { return nil }}
	server, err := NewServer(Config{Service: svc, ReadOnly: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	names := toolNames(t, server)
	for _, hidden := range []string{"save_request", "create_request", "set_env_var", "send_request"} {
		if names[hidden] {
			t.Fatalf("只读模式下不该有 %s：%v", hidden, names)
		}
	}
	if !names["list_requests"] || !names["list_envs"] {
		t.Fatalf("读工具应仍在：%v", names)
	}
}

// TestGroupsFilterTools 只勾「环境变量」时，只剩环境相关工具。
func TestGroupsFilterTools(t *testing.T) {
	svc := &Service{Roots: func() []string { return nil }}
	server, err := NewServer(Config{Service: svc, Groups: []string{GroupEnv}})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	names := toolNames(t, server)
	if !names["list_envs"] {
		t.Fatalf("环境组应包含 list_envs：%v", names)
	}
	if names["list_requests"] || names["get_doc"] {
		t.Fatalf("别的组不该注册：%v", names)
	}
	// 写工具属于「写」组，没勾就不该出现。
	if names["set_env_var"] {
		t.Fatalf("没勾写组不该有 set_env_var：%v", names)
	}
}

// TestAuditMasksSecrets 审计落盘时把 token / password 之类的值打码。
func TestAuditMasksSecrets(t *testing.T) {
	dir := t.TempDir()
	audit := NewAudit(filepath.Join(dir, "audit.jsonl"))
	audit.Record("set_env_var", true, SetEnvArgs{
		Env: "dev", Name: "token", Value: "super-secret",
	}, time.Now(), errors.New("boom"))

	entries := audit.Load()
	if len(entries) != 1 || entries[0].Tool != "set_env_var" || entries[0].OK {
		t.Fatalf("审计记录不对：%+v", entries)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("读审计文件: %v", err)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("敏感值没打码：%s", raw)
	}
	if !strings.Contains(string(raw), "••••") {
		t.Fatalf("应留下打码标记：%s", raw)
	}
	if err := audit.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if len(audit.Load()) != 0 {
		t.Fatal("清空后不该还有记录")
	}
}

// toolNames 通过一次真实的 MCP 会话列出注册了哪些工具。
func toolNames(t *testing.T, server *sdk.Server) map[string]bool {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.Run(ctx, serverTransport) }()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "probe", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("连接: %v", err)
	}
	defer func() { _ = session.Close() }()
	list, err := session.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	out := map[string]bool{}
	for _, tool := range list.Tools {
		out[tool.Name] = true
	}
	return out
}
