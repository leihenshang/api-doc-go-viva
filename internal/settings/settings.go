// Package settings 保存验证版的本地偏好（目前只有主题），放在用户配置目录，
// 与客户端的做法一致：应用配置不写进集合（集合是纯文本真相源，不该混入本机偏好）。
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/mcp"
	"github.com/leihenshang/api-doc-go-viva/internal/mock"
)

// 主题取值。
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Settings 是可持久化的偏好。
type Settings struct {
	Theme string `json:"theme"`
	// TimeoutSec 是发送请求的默认超时（秒）；请求文件里的 settings.timeout 优先。
	TimeoutSec int `json:"timeoutSec"`
	// NoFollowRedirects 为真时不自动跟随重定向（同样可被请求文件的设置覆盖）。
	NoFollowRedirects bool `json:"noFollowRedirects"`
	// InsecureSSL 是全局默认：忽略 HTTPS 证书校验（请求文件的 settings.insecureSsl 显式设置时优先）。
	InsecureSSL bool `json:"insecureSsl,omitempty"`
	// UpdateRepo 是检查更新用的 GitHub 仓库（owner/repo）；空 = 用内置默认。
	UpdateRepo string `json:"updateRepo,omitempty"`
	// CloseToTray 为真时关闭窗口不退出，收到托盘（托盘区不可用时自动退回退出）。
	CloseToTray bool `json:"closeToTray,omitempty"`
	// NoSealSecrets 为真时敏感值明文落盘；默认加密（密钥在配置目录）。
	NoSealSecrets bool `json:"noSealSecrets,omitempty"`
	// NoCookiePersist 为真时 Cookie 只留在内存里（关掉窗口就没了）。
	// 默认落盘：登录一次，下次打开还在。
	NoCookiePersist bool `json:"noCookiePersist,omitempty"`
	// Proxy 是发送用的代理地址（http:// / https:// / socks5://），空表示直连。
	Proxy string `json:"proxy,omitempty"`
	// ClientCert / ClientKey 是双向 TLS 的证书与私钥路径（PEM）；两个都填才生效。
	ClientCert string `json:"clientCert,omitempty"`
	ClientKey  string `json:"clientKey,omitempty"`
	// NoAutoSave 为真时不做自动保存（改动只留在内存与草稿里）。
	NoAutoSave bool `json:"noAutoSave,omitempty"`
	// AutoSaveSec 是自动保存的静默时间（秒），0 用默认值。
	AutoSaveSec int `json:"autoSaveSec,omitempty"`
	// SplitSideBySide 为真时请求 / 响应左右分栏，否则上下分栏。
	SplitSideBySide bool `json:"splitSideBySide,omitempty"`
	// UIScale 是界面缩放百分比（80 / 90 / 100 / 110 / 125）；0 表示 100。
	UIScale int `json:"uiScale,omitempty"`
	// Envs 记住每个集合根上次选中的环境名（键是集合目录的绝对路径）。
	Envs map[string]string `json:"envs,omitempty"`
	// LastCollection 是上次打开的集合目录（启动时没给 -collection 就用它）。
	LastCollection string `json:"lastCollection,omitempty"`
	// LastCollections 是上次同时打开的全部集合根（按打开顺序）；启动时逐个恢复。
	LastCollections []string `json:"lastCollections,omitempty"`
	// SortMode 是树的排序：空 = 磁盘顺序（seq），"name-asc" / "name-desc"。
	SortMode string `json:"sortMode,omitempty"`
	// ActiveCollection 是上次停留的集合根目录，恢复时切回它。
	ActiveCollection string `json:"activeCollection,omitempty"`
	// MCPEnabled 打开内嵌 MCP 服务（把当前打开的集合暴露给 AI 工具）。
	MCPEnabled bool `json:"mcpEnabled,omitempty"`
	// MCPAddr 是 MCP 服务的监听地址，默认只绑回环。
	MCPAddr string `json:"mcpAddr,omitempty"`
	// MCPToken 是访问令牌（首次启用时生成），客户端用 `Authorization: Bearer <token>` 连接。
	MCPToken string `json:"mcpToken,omitempty"`
	// MCPReadOnly 只暴露读工具：不发请求、不写磁盘。
	MCPReadOnly bool `json:"mcpReadOnly,omitempty"`
	// MCPGroups 是要暴露的工具组（collection / docs / env / example / network / write）；空 = 全部。
	MCPGroups []string `json:"mcpGroups,omitempty"`
	// MCPAllow 是可访问工作区的白名单：出现在列表里 = 授权，Writable 决定写工具能否落盘。
	MCPAllow []MCPAllowEntry `json:"mcpAllow,omitempty"`
	// MockEnabled 打开本地桩服务（按当前集合的请求假回复）。
	MockEnabled bool `json:"mockEnabled,omitempty"`
	// MockAddr 是桩服务的监听地址，默认只绑回环。
	MockAddr string `json:"mockAddr,omitempty"`
}

// MCPAllowEntry 是一个被授权给 AI 的集合根（绝对路径 + 是否允许写）。
type MCPAllowEntry struct {
	Dir      string `json:"dir"`
	Writable bool   `json:"writable,omitempty"`
}

// 默认值：深色主题、20 秒超时、跟随重定向。
const (
	DefaultTimeoutSec = 20
	MaxTimeoutSec     = 600
	// DefaultMockAddr 是本地桩服务的默认监听地址（只本机可连）。
	DefaultMockAddr = mock.DefaultAddr
	// DefaultMCPAddr 是内嵌 MCP 服务的默认监听地址（只本机可连）。
	// 与 mcp.DefaultAddr 同源，避免两处各写一份端口。
	DefaultMCPAddr = mcp.DefaultAddr
)

// Default 返回默认偏好：日间模式、20 秒超时、跟随重定向。
func Default() Settings {
	return Settings{
		Theme: ThemeLight, TimeoutSec: DefaultTimeoutSec,
		MCPAddr: DefaultMCPAddr, MockAddr: DefaultMockAddr, Envs: map[string]string{},
	}
}

// AutoSave 报告是否自动保存（默认开）。
func (s Settings) AutoSave() bool { return !s.NoAutoSave }

// UIScalePercent 是界面缩放百分比；没设过或越界时按 100 处理。
func (s Settings) UIScalePercent() int {
	if s.UIScale < 50 || s.UIScale > 200 {
		return 100
	}
	return s.UIScale
}

// AutoSaveInterval 是「最后一次编辑之后静默多久落盘」，夹在 1..30 秒。
func (s Settings) AutoSaveInterval() time.Duration {
	seconds := s.AutoSaveSec
	if seconds <= 0 {
		seconds = 2
	}
	if seconds < 1 {
		seconds = 1
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

// FollowRedirects 报告是否跟随重定向（默认跟随）。
func (s Settings) FollowRedirects() bool { return !s.NoFollowRedirects }

// SealSecrets 报告敏感值是否加密落盘（默认加密）。
func (s Settings) SealSecrets() bool { return !s.NoSealSecrets }

// PersistCookies 报告 Cookie Jar 是否落盘（默认落盘）。
func (s Settings) PersistCookies() bool { return !s.NoCookiePersist }

// Dir 返回配置目录：$XDG_CONFIG_HOME/api-doc-viva，缺省 ~/.config/api-doc-viva。
func Dir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("找不到用户目录: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "api-doc-viva"), nil
}

// Load 读偏好；文件不存在或坏了都退回默认值（偏好不该挡住启动）。
func Load() Settings {
	dir, err := Dir()
	if err != nil {
		return Default()
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Default()
	}
	if err != nil {
		return Default()
	}
	out := Default()
	if err := json.Unmarshal(data, &out); err != nil {
		return Default()
	}
	if out.Theme != ThemeLight && out.Theme != ThemeDark {
		out.Theme = ThemeLight
	}
	if out.TimeoutSec <= 0 {
		out.TimeoutSec = DefaultTimeoutSec
	}
	if out.Envs == nil {
		out.Envs = map[string]string{}
	}
	if strings.TrimSpace(out.MCPAddr) == "" {
		out.MCPAddr = DefaultMCPAddr
	}
	if strings.TrimSpace(out.MockAddr) == "" {
		out.MockAddr = DefaultMockAddr
	}
	return out
}

// Save 原子写入偏好文件（0600，仅本用户可读）。
func Save(s Settings) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "settings-*.json")
	if err != nil {
		return fmt.Errorf("写临时配置: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写配置: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "settings.json"))
}
