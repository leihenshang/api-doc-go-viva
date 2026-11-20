package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToDarkAndSavesTheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if got := Load(); got.Theme != ThemeLight {
		t.Fatalf("没有配置文件时应回默认日间模式：%+v", got)
	}
	if err := Save(Settings{Theme: ThemeDark}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load(); got.Theme != ThemeDark {
		t.Fatalf("保存后应读到深色：%+v", got)
	}
	info, err := os.Stat(filepath.Join(dir, "api-doc-viva", "settings.json"))
	if err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("配置权限 = %v，期望 0600", info.Mode().Perm())
	}
}

func TestCorruptSettingsFallBackToDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "api-doc-viva")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Theme != ThemeLight {
		t.Fatalf("坏配置应回默认值：%+v", got)
	}
}

func TestSavesNetworkAndSplitPreferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := Save(Settings{Theme: ThemeLight, InsecureSSL: true, SplitSideBySide: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load()
	if !got.InsecureSSL || !got.SplitSideBySide {
		t.Fatalf("新偏好没落盘：%+v", got)
	}
}
