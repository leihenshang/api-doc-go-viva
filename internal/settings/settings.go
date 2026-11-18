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
	// Envs 记住每个集合根上次选中的环境名（键是集合目录的绝对路径）。
	Envs map[string]string `json:"envs,omitempty"`
}

// 默认值：深色主题、20 秒超时、跟随重定向。
const (
	DefaultTimeoutSec = 20
	MaxTimeoutSec     = 600
)

// Default 返回默认偏好：与客户端一致的深色。
func Default() Settings {
	return Settings{Theme: ThemeDark, TimeoutSec: DefaultTimeoutSec, Envs: map[string]string{}}
}

// FollowRedirects 报告是否跟随重定向（默认跟随）。
func (s Settings) FollowRedirects() bool { return !s.NoFollowRedirects }

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
		out.Theme = ThemeDark
	}
	if out.TimeoutSec <= 0 {
		out.TimeoutSec = DefaultTimeoutSec
	}
	if out.Envs == nil {
		out.Envs = map[string]string{}
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
