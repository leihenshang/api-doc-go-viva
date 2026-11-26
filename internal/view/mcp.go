package view

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	appmcp "github.com/leihenshang/api-doc-go-viva/internal/mcp"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// mcpHost 是内嵌 MCP 服务的进程内状态：
//   - dirs：允许 AI 访问的集合根（界面线程写、MCP goroutine 读，用原子快照避免锁）
//   - errs：服务异常退出时把原因送回界面线程（界面线程只在 Page 里收，不跨线程写 Model）
type mcpHost struct {
	dirs   atomic.Value // []string
	cancel context.CancelFunc
	errs   chan string
}

// mcpRoots 返回允许访问的集合目录（可能被 MCP goroutine 并发调用，故只读原子快照）。
func (m *Model) mcpRoots() []string {
	if dirs, ok := m.mcpHost.dirs.Load().([]string); ok {
		return dirs
	}
	return nil
}

// refreshMCPRoots 把「当前打开 + 已授权」的集合根抄一份给 MCP 服务。
// 根增删、白名单勾选变化后各调用一次。
func (m *Model) refreshMCPRoots() {
	dirs := make([]string, 0, len(m.roots))
	for _, root := range m.roots {
		if root.col == nil || root.col.Dir == "" {
			continue
		}
		if !m.mcpAllowed(root.col.Dir) {
			continue
		}
		dirs = append(dirs, root.col.Dir)
	}
	m.mcpHost.dirs.Store(dirs)
}

// mcpAllowed 报告某个集合根是否在 AI 可访问白名单里。
func (m *Model) mcpAllowed(dir string) bool {
	clean := filepath.Clean(dir)
	for _, entry := range m.settings.MCPAllow {
		if filepath.Clean(entry.Dir) == clean {
			return true
		}
	}
	return false
}

// startMCP 按当前偏好启动（或重启）内嵌服务；未启用时只清掉运行状态。
func (m *Model) startMCP() {
	m.stopMCP()
	m.refreshMCPRoots()
	if !m.settings.MCPEnabled {
		m.mcp = mcpRuntime{}
		return
	}
	if strings.TrimSpace(m.settings.MCPToken) == "" {
		m.settings.MCPToken = newMCPToken()
		_ = settings.Save(m.settings)
	}
	// 审计落在偏好目录：AI 连续调用时能翻「它到底改了什么」。
	auditPath := ""
	if dir, err := settings.Dir(); err == nil {
		auditPath = filepath.Join(dir, "mcp-audit.jsonl")
	}
	m.mcp.audit = appmcp.NewAudit(auditPath)
	server, err := appmcp.NewServer(appmcp.Config{
		Service:  &appmcp.Service{Roots: m.mcpRoots},
		ReadOnly: m.settings.MCPReadOnly,
		Groups:   m.settings.MCPGroups,
		// 空 = 拒绝一切带 Origin 的请求（浏览器类客户端因此连不上，要显式授权）。
		AllowOrigins: m.settings.MCPAllowOrigins,
		Confirm:      m.confirmMCPWrite,
		Audit:        m.mcp.audit,
	})
	if err != nil {
		m.mcp = mcpRuntime{err: err.Error()}
		return
	}
	// 令牌 / 地址先取出来：goroutine 里不能再读 Model（界面线程会改）。
	addr, token := m.settings.MCPAddr, m.settings.MCPToken
	ctx, cancel := context.WithCancel(context.Background())
	m.mcpHost.cancel = cancel
	go func() {
		opts := appmcp.HTTPOptions{Addr: addr, Token: token}
		if err := appmcp.ServeHTTP(ctx, server, opts); err != nil {
			select {
			case m.mcpHost.errs <- err.Error():
			default:
			}
		}
	}()
	m.mcp = mcpRuntime{running: true, addr: mcpURL(addr)}
}

// stopMCP 关掉内嵌服务（没启动过也安全）。
func (m *Model) stopMCP() {
	if m.mcpHost.cancel != nil {
		m.mcpHost.cancel()
		m.mcpHost.cancel = nil
	}
	m.mcp = mcpRuntime{}
}

// restartMCP 在设置变化后重开服务（开关 / 地址改了都走这里）。
func (m *Model) restartMCP() {
	m.startMCP()
	m.invalidator.Invalidate()
}

// drainMCP 把后台送回的启动失败原因落到状态里（只在界面线程调用）。
func (m *Model) drainMCP() {
	select {
	case err := <-m.mcpHost.errs:
		m.mcp = mcpRuntime{err: err}
		m.invalidator.Invalidate()
	default:
	}
}

// mcpURL 是给用户/客户端看的连接地址。
func mcpURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = settings.DefaultMCPAddr
	}
	return "http://" + addr + "/mcp"
}

// newMCPToken 生成一次性访问令牌（16 字节随机数的十六进制）。
func newMCPToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// mcpAllowRow 是 MCP 弹窗里的一行工作区授权。
type mcpAllowRow struct {
	Dir      string
	Name     string
	Allowed  bool // 勾选 = 授权 AI 访问
	Writable bool // 允许写（写类工具尚未移植，先随配置存下来）
}

// refreshMCPAllowRows 按「已打开的集合根 + 已存白名单」重建授权列表：
// 当前打开的根全部列出（默认未勾选），白名单里有但没打开的也保留一行，方便取消授权。
func (m *Model) refreshMCPAllowRows() {
	rows := make([]mcpAllowRow, 0, len(m.roots)+len(m.settings.MCPAllow))
	seen := make(map[string]bool, len(m.roots)+len(m.settings.MCPAllow))
	for index, root := range m.roots {
		if root.col == nil || root.col.Dir == "" {
			continue
		}
		dir := filepath.Clean(root.col.Dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		row := mcpAllowRow{Dir: dir, Name: m.collectionLabel(index)}
		for _, entry := range m.settings.MCPAllow {
			if filepath.Clean(entry.Dir) == dir {
				row.Allowed, row.Writable = true, entry.Writable
			}
		}
		rows = append(rows, row)
	}
	for _, entry := range m.settings.MCPAllow {
		dir := filepath.Clean(entry.Dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		rows = append(rows, mcpAllowRow{Dir: dir, Name: filepath.Base(dir), Allowed: true, Writable: entry.Writable})
	}
	m.ui.mcpAllow = rows
}

// mcpConfigJSON 是给 MCP 客户端粘贴的配置片段（含监听地址与令牌）。
func (m *Model) mcpConfigJSON() string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "api-doc-go": {
      "type": "http",
      "url": %q,
      "headers": {
        "Authorization": "Bearer %s"
      }
    }
  }
}`, mcpURL(m.ui.mcpAddr), m.ui.mcpToken)
}

// mcpGroupLabels 是工具组的中文名（设置面板里用）。
var mcpGroupLabels = map[string]string{
	appmcp.GroupCollection: "集合与请求",
	appmcp.GroupDocs:       "文档",
	appmcp.GroupEnv:        "环境变量",
	appmcp.GroupExample:    "响应示例",
	appmcp.GroupNetwork:    "发送请求",
	appmcp.GroupWrite:      "写操作",
}

// syncMCPGroups 把落盘的组勾选装进表单（没配过 = 全选）。
func (m *Model) syncMCPGroups() {
	m.ui.mcpGroupOn = make([]bool, len(appmcp.Groups))
	if len(m.settings.MCPGroups) == 0 {
		for i := range m.ui.mcpGroupOn {
			m.ui.mcpGroupOn[i] = true
		}
		return
	}
	for index, name := range appmcp.Groups {
		for _, picked := range m.settings.MCPGroups {
			if picked == name {
				m.ui.mcpGroupOn[index] = true
			}
		}
	}
}

// pickedMCPGroups 把勾选还原成分组名；全选时返回 nil（= 全部）。
func (m *Model) pickedMCPGroups() []string {
	out := make([]string, 0, len(appmcp.Groups))
	for index, name := range appmcp.Groups {
		if index < len(m.ui.mcpGroupOn) && m.ui.mcpGroupOn[index] {
			out = append(out, name)
		}
	}
	if len(out) == len(appmcp.Groups) {
		return nil
	}
	return out
}

// parseMCPOrigins 把「逗号分隔的来源」拆成数组：去空格、丢空项、去重且保序。
// 除空列表（拒绝一切带 Origin 的请求）之外不加额外限制，匹配规则交给服务端。
func parseMCPOrigins(text string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(text, ",") {
		item := strings.TrimSpace(part)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// mcpAuditPath 是审计文件的位置（偏好目录）；取不到目录时为空。
func mcpAuditPath() string {
	if dir, err := settings.Dir(); err == nil {
		return filepath.Join(dir, "mcp-audit.jsonl")
	}
	return ""
}

// loadMCPAudit 读最近 20 条调用：服务在跑就用内存里的，否则读文件。
func (m *Model) loadMCPAudit() []appmcp.AuditEntry {
	audit := m.mcp.audit
	if audit == nil {
		audit = appmcp.NewAudit(mcpAuditPath())
	}
	rows := audit.Load()
	if len(rows) > 20 {
		rows = rows[len(rows)-20:]
	}
	return rows
}

// clearMCPAudit 清空审计并刷新面板。
func (m *Model) clearMCPAudit() {
	audit := m.mcp.audit
	if audit == nil {
		audit = appmcp.NewAudit(mcpAuditPath())
	}
	_ = audit.Clear()
	m.ui.mcpAuditRows = m.loadMCPAudit()
	m.copyNote = "已清空调用审计"
	m.invalidator.Invalidate()
}

// exportMCPAudit 把审计导出到偏好目录下的固定文件，并在面板上写出路径。
func (m *Model) exportMCPAudit() {
	dir, err := settings.Dir()
	if err != nil {
		m.copyNote = "导出失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	target := filepath.Join(dir, "mcp-audit-export.jsonl")
	audit := m.mcp.audit
	if audit == nil {
		audit = appmcp.NewAudit(mcpAuditPath())
	}
	count, err := audit.Export(target)
	if err != nil {
		m.copyNote = "导出失败：" + err.Error()
	} else {
		m.copyNote = fmt.Sprintf("已导出 %d 条到 %s", count, target)
	}
	m.invalidator.Invalidate()
}
