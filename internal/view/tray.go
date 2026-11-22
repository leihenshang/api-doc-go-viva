package view

import (
	"fmt"
	"log"

	"github.com/leihenshang/api-doc-go-viva/internal/brand"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
)

// 托盘：关闭窗口时可以把窗口收到系统状态区，而不是退出进程。
//
// viva 的托盘是三平台原生的（macOS NSStatusItem / Windows 通知区 / Linux 的
// StatusNotifierItem + dbusmenu），Linux 上没有宿主时 AddStatusItem 会返回
// app.ErrNoStatusArea —— 这时必须退回「关闭即退出」，不能让窗口关掉后
// 用户再也打不开。

// trayWindow 是托盘菜单需要的那几个窗口动作。
//
// 抽成接口是为了无头用例：托盘菜单的构建与点击不依赖真实的原生窗口。
type trayWindow interface {
	Show()
	Hide()
	Hidden() bool
}

// statusItem 是托盘图标的句柄（app.StatusItem 的窄化，便于测试替身）。
type statusItem interface {
	SetTooltip(string)
	Remove()
}

// installTray 挂上托盘图标；不可用时记下原因并保持「关闭即退出」。
//
// 顺序很重要：先确认托盘真的挂上了，再允许「关到托盘」；反过来会丢窗口。
func (m *Model) installTray(application *app.Application) {
	if application == nil {
		return
	}
	item, err := application.AddStatusItem(app.StatusItemConfig{
		Tooltip: "API-DOC-GO",
		Icon:    trayIcon(32),
		OnClick: func(app.StatusClick) { m.showWindow() },
		Menu:    m.trayMenu,
	})
	if err != nil {
		m.trayNote = "当前桌面没有可用的托盘区（" + err.Error() + "），关闭窗口仍会退出"
		if m.ui.closeToTray {
			// 已经存了「关到托盘」但这次没有托盘：本次会话先按退出处理。
			m.ui.closeToTray = false
			m.trayEnabled = false
		}
		log.Println("托盘不可用：" + err.Error())
		return
	}
	m.trayItem = item
	m.trayEnabled = true
	m.trayNote = ""
	// 挂上时就把集合名写进提示：那一刻主窗口里的集合已经载入好了。
	m.refreshTray()
}

// refreshTray 在集合切换后刷新提示文字（菜单每次展开都会重建，无需重建菜单）。
func (m *Model) refreshTray() {
	if m.trayItem == nil {
		return
	}
	m.trayItem.SetTooltip("API-DOC-GO · " + m.traySummary())
}

// traySummary 是托盘提示里那句话：集合名 + 规模。
func (m *Model) traySummary() string {
	if m.col == nil {
		return "没有打开集合"
	}
	return fmt.Sprintf("%s（%d 条）", m.col.Name, countRequests(m.col))
}

// countRequests 数集合里的请求与文档（分组不算）。
func countRequests(col *explorer.Collection) int {
	if col == nil {
		return 0
	}
	count := 0
	for _, entry := range col.Entries {
		if entry.Kind == explorer.KindHTTP || entry.Kind == explorer.KindDoc || entry.Kind == explorer.KindGRPC {
			count++
		}
	}
	return count
}

// trayMenu 是右键菜单：每次展开时重建，所以状态永远是最新的。
func (m *Model) trayMenu() []app.MenuItem {
	hidden := m.window != nil && m.window.Hidden()
	items := []app.MenuItem{
		{Title: "显示主窗口", ID: "show", Disabled: !hidden, OnSelect: m.showWindow},
		{Title: "隐藏到托盘", ID: "hide", Disabled: hidden, OnSelect: m.hideWindow},
		{Separator: true, ID: "sep-status"},
	}
	if m.col != nil {
		items = append(items, app.MenuItem{
			Title:    "集合：" + m.col.Name,
			ID:       "collection",
			Disabled: true,
		})
	}
	items = append(items,
		app.MenuItem{Title: fmt.Sprintf("请求 %d 条", countRequests(m.col)), ID: "count", Disabled: true},
		app.MenuItem{Separator: true, ID: "sep-quit"},
		app.MenuItem{Title: "退出 API-DOC-GO", ID: "quit", OnSelect: m.quitApp},
	)
	return items
}

// showWindow 把主窗口叫回来（也用于托盘左键点击）。
func (m *Model) showWindow() {
	if m.window == nil {
		return
	}
	m.window.Show()
	m.invalidator.Invalidate()
}

// hideWindow 把窗口收起来；进程留在托盘。
func (m *Model) hideWindow() {
	if m.window == nil {
		return
	}
	m.window.Hide()
}

// quitApp 退出进程（不用等窗口关闭回调）。
func (m *Model) quitApp() {
	if m.app != nil {
		m.app.Quit()
		return
	}
	if m.window != nil {
		m.window.Hide()
	}
}

// shouldHideOnClose 判断这次「关闭窗口」该不该只收起来。
//
// 三个条件缺一不可：用户选了「关到托盘」、托盘真的挂上了、窗口还在。
func (m *Model) shouldHideOnClose() bool {
	return m.ui.closeToTray && m.trayEnabled && m.trayItem != nil && m.window != nil
}

// trayHint 是设置面板里那一行说明：托盘可用性要说清楚，不能让人猜。
func (m *Model) trayHint() string {
	if m.trayEnabled {
		return "关闭窗口后进程留在系统托盘，点图标或用菜单可再打开。"
	}
	if m.trayNote != "" {
		return m.trayNote
	}
	return "当前桌面没有托盘区，这一项不可用。"
}

// trayIcon 取应用图标并转成状态区要的形态。
//
// 画法在 internal/brand 里（打包时生成 .ico 也用同一份），
// 这里只负责包成 viva 的类型。
func trayIcon(size int) app.StatusIcon {
	return app.StatusIcon{Size: size, RGBA: brand.RGBA(size)}
}

// appIcon 是窗口图标（任务栏 / Alt+Tab）：同一张画，大尺寸。
func appIcon(size int) app.WindowIcon {
	return app.WindowIcon{Size: size, RGBA: brand.RGBA(size)}
}
