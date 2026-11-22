package view

import (
	"fmt"
	"log"

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

// trayIcon 画出托盘图标：圆角方块 + 三条横线（像一列请求），避免额外资源文件。
//
// 先按 4 倍尺寸画再降采样：状态区把它缩到 22px 左右，直接画小图会很脏。
func trayIcon(size int) app.StatusIcon {
	const scale = 4
	big := size * scale
	rgba := make([]byte, big*big*4)
	radius := float64(big) * 0.24
	for y := 0; y < big; y++ {
		for x := 0; x < big; x++ {
			i := (y*big + x) * 4
			if !insideRoundRect(float64(x)+0.5, float64(y)+0.5, float64(big), radius) {
				continue
			}
			r, g, b := uint8(59), uint8(130), uint8(246) // 与界面强调色一致
			// 三条白色横线：覆盖 y 在 26% / 50% / 74% 处的窄带。
			for _, center := range []float64{0.28, 0.5, 0.72} {
				cy := center * float64(big)
				if float64(y)+0.5 > cy-float64(big)*0.05 && float64(y)+0.5 < cy+float64(big)*0.05 &&
					float64(x)+0.5 > float64(big)*0.28 && float64(x)+0.5 < float64(big)*0.72 {
					r, g, b = 255, 255, 255
				}
			}
			rgba[i], rgba[i+1], rgba[i+2], rgba[i+3] = r, g, b, 255
		}
	}
	return app.StatusIcon{Size: size, RGBA: downsample(rgba, big, size)}
}

// insideRoundRect 判断点是否落在圆角矩形内（四角各挖一个圆）。
func insideRoundRect(x, y, side, radius float64) bool {
	if x < radius && y < radius {
		return (radius-x)*(radius-x)+(radius-y)*(radius-y) <= radius*radius
	}
	if x > side-radius && y < radius {
		return (x-(side-radius))*(x-(side-radius))+(radius-y)*(radius-y) <= radius*radius
	}
	if x < radius && y > side-radius {
		return (radius-x)*(radius-x)+(y-(side-radius))*(y-(side-radius)) <= radius*radius
	}
	if x > side-radius && y > side-radius {
		return (x-(side-radius))*(x-(side-radius))+(y-(side-radius))*(y-(side-radius)) <= radius*radius
	}
	return true
}

// downsample 把 big 见方的 RGBA 盒式降采样成 size 见方（取平均，边缘不出锯齿）。
func downsample(rgba []byte, big, size int) []byte {
	factor := big / size
	out := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a, n int
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					i := ((y*factor+dy)*big + (x*factor + dx)) * 4
					r += int(rgba[i])
					g += int(rgba[i+1])
					b += int(rgba[i+2])
					a += int(rgba[i+3])
					n++
				}
			}
			o := (y*size + x) * 4
			if n == 0 {
				continue
			}
			out[o], out[o+1], out[o+2], out[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)
		}
	}
	return out
}

// appIcon 是窗口图标（任务栏 / Alt+Tab）：同一张画，大尺寸。
func appIcon(size int) app.WindowIcon {
	icon := trayIcon(size)
	return app.WindowIcon{Size: icon.Size, RGBA: icon.RGBA}
}
