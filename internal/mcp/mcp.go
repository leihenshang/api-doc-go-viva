// Package mcp 是验证版的「内嵌 MCP 服务」：把当前打开的集合以只读 + 发送的方式
// 暴露给 AI 工具（MCP 客户端）。移植自 Wails 客户端 internal/mcp 的核心工具集：
//
//	Wails: list_workspaces / list_requests / get_request / get_doc / send_request …
//	viva : 同样这五个（写类工具尚未移植，见 README 的待办）
//
// 与 Wails 版的关键差异：验证版没有跨线程的 App 门面，服务**直接读磁盘**
// （集合是纯文本真相源），因此不会和界面线程共享内存状态，也就没有数据竞争。
package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// 工具执行上限：一个不响应的地址不该把 MCP 会话挂死。
const toolTimeout = 3 * time.Minute

// 输出体积上限：AI 工具链里文本太长的收益很低，反而把上下文挤爆。
const (
	maxBodyBytes = 16 << 10
	maxDocBytes  = 32 << 10
)

// Service 是工具的业务实现：所有访问都以「允许的集合根」为白名单。
type Service struct {
	// Roots 返回允许访问的集合目录（绝对值，按界面里的打开顺序）。
	// 由调用方注入而不是持有引用：roots 会随界面增删，且必须跨线程安全。
	Roots func() []string
}

// Workspace 是对外展示的一个集合根：只有别名与名称，绝不带真实路径
// （AI 拿到绝对路径就能绕过工具直接读写文件）。
type Workspace struct {
	Alias    string
	Name     string
	Dir      string // 内部使用，不输出
	Requests int
	Docs     int
	Envs     []string
}

// workspaces 扫描当前允许的集合根；读不动的根直接跳过（不让一个坏集合拖垮整次调用）。
func (s *Service) workspaces() []Workspace {
	if s.Roots == nil {
		return nil
	}
	out := make([]Workspace, 0, 4)
	for _, dir := range s.Roots() {
		col, err := explorer.Load(dir)
		if err != nil {
			continue
		}
		ws := Workspace{Alias: fmt.Sprintf("ws_%d", len(out)+1), Name: collectionName(col), Dir: col.Dir}
		for _, entry := range col.Entries {
			if entry.IsDoc() {
				ws.Docs++
				continue
			}
			ws.Requests++
		}
		ws.Envs, _ = explorer.ListEnvs(col.Dir)
		out = append(out, ws)
	}
	return out
}

// collectionName 是集合显示名：清单里的 name，缺省用目录名。
func collectionName(col *explorer.Collection) string {
	if col.Name != "" {
		return col.Name
	}
	return filepath.Base(col.Dir)
}

// pick 按别名挑一个工作区；alias 为空时取第一个（单集合场景最常用）。
func (s *Service) pick(alias string) (Workspace, error) {
	list := s.workspaces()
	if len(list) == 0 {
		return Workspace{}, fmt.Errorf("没有可访问的集合：请在客户端打开一个集合目录")
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return list[0], nil
	}
	for _, ws := range list {
		if strings.EqualFold(ws.Alias, alias) || ws.Name == alias {
			return ws, nil
		}
	}
	names := make([]string, 0, len(list))
	for _, ws := range list {
		names = append(names, ws.Alias+"("+ws.Name+")")
	}
	return Workspace{}, fmt.Errorf("找不到工作区 %q，可用：%s", alias, strings.Join(names, ", "))
}

// resolve 把「相对集合根的路径」钉死在工作区内：拒绝绝对路径与 .. 逃逸。
func resolve(ws Workspace, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("缺少 path 参数（相对集合根的路径，如 用户/登录.yml）")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path 必须是集合内的相对路径：%s", rel)
	}
	return filepath.Join(ws.Dir, clean), nil
}

// ---- 工具实现 ----

// ListWorkspaces 列出当前打开、且允许 AI 访问的集合（不暴露真实路径）。
func (s *Service) ListWorkspaces(context.Context) (string, error) {
	list := s.workspaces()
	if len(list) == 0 {
		return "没有可访问的集合：请在客户端打开一个集合目录。", nil
	}
	var b strings.Builder
	b.WriteString("可用工作区（用别名或名称引用）：\n")
	for _, ws := range list {
		fmt.Fprintf(&b, "- %s %s：%d 个请求，%d 篇文档，环境 %s\n",
			ws.Alias, ws.Name, ws.Requests, ws.Docs, envList(ws.Envs))
	}
	return b.String(), nil
}

// envList 把环境名列表写成可读文案。
func envList(names []string) string {
	if len(names) == 0 {
		return "（无）"
	}
	return strings.Join(names, " / ")
}

// ListArgs 是 list_requests 的入参。
type ListArgs struct {
	Workspace string `json:"workspace,omitempty"` // 工作区别名（缺省第一个）
	Query     string `json:"query,omitempty"`     // 名称 / 路径 / URL / 概述 的关键字过滤
}

// PathArgs 是 get_request / get_doc 的入参。
type PathArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path"` // 相对集合根的路径，如 用户/登录.yml
}

// SendArgs 是 send_request 的入参。
type SendArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path"`
	Env       string `json:"env,omitempty"` // 环境名；缺省用集合里第一个
}

// ListRequests 列出集合里的请求与文档：方法 + 名称 + 分组 + 相对路径。
func (s *Service) ListRequests(_ context.Context, in ListArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	col, err := explorer.Load(ws.Dir)
	if err != nil {
		return "", fmt.Errorf("读取集合 %s 失败: %w", ws.Name, err)
	}
	needle := strings.ToLower(strings.TrimSpace(in.Query))
	entries := make([]*explorer.Entry, 0, len(col.Entries))
	for _, entry := range col.Entries {
		if needle == "" || entryMatches(entry, needle) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return fmt.Sprintf("集合 %s 里没有匹配 %q 的条目。", ws.Name, in.Query), nil
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].File < entries[j].File })
	var b strings.Builder
	fmt.Fprintf(&b, "集合 %s（%d 条）：\n", ws.Name, len(entries))
	for _, entry := range entries {
		if entry.IsDoc() {
			fmt.Fprintf(&b, "- MD  %s（文档）%s\n", entry.File, groupSuffix(entry.Group))
			continue
		}
		fmt.Fprintf(&b, "- %-4s %s  %s%s\n", entry.Method, entry.Name, entry.File, groupSuffix(entry.Group))
	}
	return b.String(), nil
}

// groupSuffix 是分组展示尾巴。
func groupSuffix(group string) string {
	if strings.TrimSpace(group) == "" {
		return ""
	}
	return "  [" + group + "]"
}

// entryMatches 与界面搜索同一套匹配规则（名称 / 分组 / 地址 / 概述）。
func entryMatches(entry *explorer.Entry, needle string) bool {
	haystack := strings.ToLower(entry.Name + " " + entry.Group + " " + entry.URL + " " + entry.Summary)
	return strings.Contains(haystack, needle)
}

// GetRequest 读一条请求的完整定义（方法、地址、参数、请求头、请求体、认证、文档）。
func (s *Service) GetRequest(_ context.Context, in PathArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	full, err := resolve(ws, in.Path)
	if err != nil {
		return "", err
	}
	entry, err := readEntry(ws.Dir, full)
	if err != nil {
		return "", err
	}
	if entry.IsDoc() {
		return "", fmt.Errorf("%s 是文档，请用 get_doc 读取", entry.File)
	}
	return formatRequest(entry), nil
}

// readEntry 按文件路径读一条条目：集合扫描只暴露已载入的条目，这里再补一次单文件读取。
func readEntry(dir, full string) (*explorer.Entry, error) {
	col, err := explorer.Load(dir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(col.Dir, full)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)
	for _, entry := range col.Entries {
		if entry.File == rel {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("集合里没有 %s（先用 list_requests 查路径）", rel)
}

// formatRequest 把请求定义渲染成紧凑文本（AI 读起来比嵌套 JSON 省 token）。
func formatRequest(entry *explorer.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", entry.Method, entry.URL)
	if entry.Summary != "" {
		fmt.Fprintf(&b, "概述：%s\n", entry.Summary)
	}
	fmt.Fprintf(&b, "分组：%s · 路径：%s\n", orDash(entry.Group), entry.File)
	block := entry.Payload.HTTP
	if block.Method == "" && block.URL == "" {
		writeGRPC(&b, entry)
		return b.String()
	}
	writeRows(&b, "查询参数", block.Params)
	writeRows(&b, "请求头", block.Headers)
	if block.Auth != nil && block.Auth.Type != "" && block.Auth.Type != "none" {
		fmt.Fprintf(&b, "认证：%s\n", block.Auth.Type)
	}
	switch block.Body.Type {
	case "", "none":
	case "form", "multipart":
		fmt.Fprintf(&b, "请求体（%s）：\n", block.Body.Type)
		for _, row := range block.Body.Form {
			fmt.Fprintf(&b, "  %s = %s\n", row.Name, row.Value)
		}
	default:
		body := block.Body.Raw
		if body == "" {
			body = block.Body.Data
		}
		body = clip(body, maxBodyBytes)
		fmt.Fprintf(&b, "请求体（%s）：\n%s\n", block.Body.Type, body)
	}
	if len(block.Body.Fields) > 0 {
		b.WriteString("请求体字段：\n")
		for _, field := range block.Body.Fields {
			fmt.Fprintf(&b, "  %s: %s%s%s\n", field.Name, field.Type, requiredMark(field.Required), exampleMark(field.Example))
		}
	}
	if docs := strings.TrimSpace(entry.Payload.Docs); docs != "" {
		fmt.Fprintf(&b, "文档：\n%s\n", clip(docs, maxDocBytes))
	}
	return b.String()
}

// writeGRPC 是 gRPC 请求的只读摘要（验证版不发起调用）。
func writeGRPC(b *strings.Builder, entry *explorer.Entry) {
	grpc := entry.Payload.GRPC
	if grpc == nil {
		return
	}
	fmt.Fprintf(b, "gRPC：%s / %s.%s\n", grpc.Target, grpc.Service, grpc.Method)
	if grpc.Proto != "" {
		fmt.Fprintf(b, "proto：%s\n", grpc.Proto)
	}
}

// writeRows 写一张键值表（查询参数 / 请求头）：只列有名字的行。
func writeRows(b *strings.Builder, title string, rows []collection.KV) {
	named := make([]collection.KV, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Name) != "" {
			named = append(named, row)
		}
	}
	if len(named) == 0 {
		return
	}
	fmt.Fprintf(b, "%s：\n", title)
	for _, row := range named {
		fmt.Fprintf(b, "  %s: %s\n", row.Name, row.Value)
	}
}

// requiredMark / exampleMark 是字段表的尾巴。
func requiredMark(required bool) string {
	if required {
		return "（必填）"
	}
	return ""
}

func exampleMark(example string) string {
	if strings.TrimSpace(example) == "" {
		return ""
	}
	return " 例：" + example
}

// GetDoc 读一篇 Markdown 文档的正文。
func (s *Service) GetDoc(_ context.Context, in PathArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	full, err := resolve(ws, in.Path)
	if err != nil {
		return "", err
	}
	entry, err := readEntry(ws.Dir, full)
	if err != nil {
		return "", err
	}
	if !entry.IsDoc() {
		return "", fmt.Errorf("%s 不是文档，请用 get_request 读取", entry.File)
	}
	body := strings.TrimSpace(entry.Content)
	if body == "" {
		return "（空文档）", nil
	}
	return clip(body, maxDocBytes), nil
}

// SendRequest 真正发送一条请求，返回状态、耗时、响应头与正文（按上限截断）。
func (s *Service) SendRequest(ctx context.Context, in SendArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	full, err := resolve(ws, in.Path)
	if err != nil {
		return "", err
	}
	entry, err := readEntry(ws.Dir, full)
	if err != nil {
		return "", err
	}
	if entry.IsDoc() || entry.Payload == nil {
		return "", fmt.Errorf("%s 不是可发送的请求", entry.File)
	}
	vars, envName := loadEnvVars(ws, in.Env)
	follow := true
	plan := send.Plan{
		File:            entry.Payload,
		Vars:            vars,
		Timeout:         toolTimeout,
		MaxBody:         maxBodyBytes,
		Dir:             ws.Dir,
		FollowRedirects: &follow,
	}
	runCtx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	out := send.Do(runCtx, plan)
	if out.Err != "" {
		return "", fmt.Errorf("发送失败：%s", out.Err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n环境：%s\n状态：%d %s · %s · %s\n",
		entry.Method, entry.URL, orDash(envName), out.Status, out.Reason,
		out.Duration.Round(time.Millisecond), humanBytes(out.Size))
	if len(out.Missing) > 0 {
		fmt.Fprintf(&b, "未解析变量：%s\n", strings.Join(out.Missing, ", "))
	}
	b.WriteString("响应头：\n")
	for _, header := range out.Headers {
		fmt.Fprintf(&b, "  %s: %s\n", header.Name, header.Value)
	}
	body := strings.TrimSpace(out.Body)
	if body == "" {
		b.WriteString("响应体：（空）\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "响应体：\n%s\n", clip(body, maxBodyBytes))
	return b.String(), nil
}

// loadEnvVars 读环境变量；没指定就看集合里第一个环境，一个都没有则空表。
func loadEnvVars(ws Workspace, want string) (map[string]string, string) {
	names, err := explorer.ListEnvs(ws.Dir)
	if err != nil || len(names) == 0 {
		return nil, ""
	}
	name := strings.TrimSpace(want)
	if name == "" {
		name = names[0]
	}
	vars, err := explorer.LoadEnv(ws.Dir, name)
	if err != nil {
		return nil, name
	}
	return vars, name
}

// orDash 把空串写成占位符。
func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

// clip 截断长文本，尾巴说明被截了多少。
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + fmt.Sprintf("\n…（已截断，共 %d 字节）", len(text))
}

// humanBytes 把字节数写成 B / KB / MB。
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
