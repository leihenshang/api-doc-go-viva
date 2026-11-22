package view

import (
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/secrets"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
)

// fakeWindow 记录显示 / 隐藏调用，代替原生窗口。
type fakeWindow struct {
	hidden bool
	shows  int
	hides  int
}

func (w *fakeWindow) Show()        { w.hidden, w.shows = false, w.shows+1 }
func (w *fakeWindow) Hide()        { w.hidden, w.hides = true, w.hides+1 }
func (w *fakeWindow) Hidden() bool { return w.hidden }

// fakeItem 是托盘图标的替身：只记最后一次提示文字。
type fakeItem struct {
	tooltip string
	removed bool
}

func (i *fakeItem) SetTooltip(text string) { i.tooltip = text }
func (i *fakeItem) Remove()                { i.removed = true }

// trayModel 造一个已挂上托盘、带假窗口的模型。
func trayModel(t *testing.T) (*Model, *fakeWindow, *fakeItem) {
	t.Helper()
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	window := &fakeWindow{}
	item := &fakeItem{}
	model.window = window
	model.trayItem = item
	model.trayEnabled = true
	return model, window, item
}

// TestTrayMenuShowsState 菜单里的三项与显隐状态跟着窗口走。
func TestTrayMenuShowsState(t *testing.T) {
	model, window, item := trayModel(t)

	menu := model.trayMenu()
	byID := map[string]app.MenuItem{}
	for _, entry := range menu {
		if entry.ID != "" {
			byID[entry.ID] = entry
		}
	}
	if _, ok := byID["show"]; !ok {
		t.Fatalf("缺少「显示主窗口」：%+v", menu)
	}
	if !byID["show"].Disabled {
		t.Fatal("窗口可见时「显示」该置灰")
	}
	if byID["hide"].Disabled {
		t.Fatal("窗口可见时「隐藏」该可用")
	}
	if byID["collection"].Title == "" || !byID["collection"].Disabled {
		t.Fatalf("集合名应作为只读状态行：%+v", byID["collection"])
	}

	// 隐藏之后：状态反过来。
	byID["hide"].OnSelect()
	if !window.hidden || window.hides != 1 {
		t.Fatalf("「隐藏到托盘」应隐藏窗口：%+v", window)
	}
	menu = model.trayMenu()
	for _, entry := range menu {
		if entry.ID == "show" && entry.Disabled {
			t.Fatal("隐藏后「显示」该可用")
		}
	}
	for _, entry := range menu {
		if entry.ID == "show" {
			entry.OnSelect()
		}
	}
	if window.hidden || window.shows != 1 {
		t.Fatalf("「显示主窗口」应叫回窗口：%+v", window)
	}

	// 提示文字跟着集合走。
	model.refreshTray()
	if !strings.Contains(item.tooltip, "API-DOC-GO") || !strings.Contains(item.tooltip, model.col.Name) {
		t.Fatalf("托盘提示不对：%q", item.tooltip)
	}
}

// TestCloseGoesToTray 关到托盘只在「开关打开 + 托盘可用 + 有窗口」时生效。
func TestCloseGoesToTray(t *testing.T) {
	model, window, _ := trayModel(t)

	closeWindow := func() (allowed bool) {
		model.config().OnCloseRequest(func() { allowed = true })
		return allowed
	}

	// 1) 开关打开 + 托盘可用 → 只隐藏，不真关。
	model.ui.closeToTray = true
	if allowed := closeWindow(); allowed {
		t.Fatal("关到托盘时不该放行关闭")
	}
	if window.hides != 1 || !window.hidden {
		t.Fatalf("应隐藏窗口：%+v", window)
	}

	// 2) 开关关掉 → 正常关闭。
	model.ui.closeToTray = false
	if allowed := closeWindow(); !allowed {
		t.Fatal("没开「关到托盘」时应正常关闭")
	}

	// 3) 开关打开但托盘没挂上 → 必须正常关闭，否则窗口再也打不开。
	model.ui.closeToTray = true
	model.trayEnabled = false
	model.trayItem = nil
	if allowed := closeWindow(); !allowed {
		t.Fatal("没有托盘时必须放行关闭")
	}
}

// TestTrayIconIsOpaqueCenter 图标尺寸对、中间不透明、四角透明（圆角）。
func TestTrayIconIsOpaqueCenter(t *testing.T) {
	const size = 32
	icon := trayIcon(size)
	if icon.Size != size || len(icon.RGBA) != size*size*4 {
		t.Fatalf("图标尺寸不对：%d / %d 字节", icon.Size, len(icon.RGBA))
	}
	center := ((size/2)*size + size/2) * 4
	if icon.RGBA[center+3] != 255 {
		t.Fatalf("中心应不透明，实际 alpha=%d", icon.RGBA[center+3])
	}
	corner := 3 // (0,0) 的 alpha
	if icon.RGBA[corner] != 0 {
		t.Fatalf("圆角外的角应透明，实际 alpha=%d", icon.RGBA[corner])
	}
}

// TestSettingsPersistTrayAndSeal 两个新开关要落进偏好，并且立刻改变加密状态。
func TestSettingsPersistTrayAndSeal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { secrets.Configure("", false) })

	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.openSettings()
	model.ui.closeToTray = true
	model.ui.sealSecrets = false
	model.applySettingsEdits()

	saved := settings.Load()
	if !saved.CloseToTray {
		t.Fatal("「关闭窗口收到托盘」没落盘")
	}
	if saved.SealSecrets() {
		t.Fatal("关掉加密后不该还处于加密状态")
	}
	if secrets.Enabled() {
		t.Fatal("运行期加密状态应跟着设置变")
	}
}
