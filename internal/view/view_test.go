package view

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/history"
	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// TestClickingSendShowsTheResponse 走完整链路：读集合 → 点行 → 点发送 → 真实 HTTP → 界面出状态码。
func TestClickingSendShowsTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"via":"viva"}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	if !shows(driver, srv.URL+"/healthz") {
		t.Fatalf("详情没有显示 URL：%v", painted(driver))
	}

	// 响应区常驻在下方：还没发过就是「尚未发送」。
	if !shows(driver, "尚未发送") {
		t.Fatalf("响应区应提示尚未发送：%v", painted(driver))
	}
	// 概述常驻在标题下方（不再随分段切换），标题下也不再重复路径与分组。
	if !shows(driver, "服务探活") {
		t.Fatalf("标题下方应显示接口概述：%v", painted(driver))
	}
	if shows(driver, "健康检查.yml") {
		t.Fatalf("标题下不应再显示路径与分组：%v", painted(driver))
	}
	// 分段页签换成 Wails 的顺序：点「文档」默认就是 Markdown 预览。
	click(t, driver, "文档")
	pump(t, driver)
	if !shows(driver, "渲染检查") {
		t.Fatalf("Docs 默认应按 Markdown 排版（标题未画出）：%v", painted(driver))
	}
	click(t, driver, "参数")
	pump(t, driver)

	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if model.resp.Status != http.StatusOK || !strings.Contains(model.resp.Body, `"via":"viva"`) {
		t.Fatalf("模型里的响应 = %+v", model.resp)
	}
	if !shows(driver, "200") {
		t.Fatalf("界面没有画状态码：%v", painted(driver))
	}
}

// TestReqDocsPreviewThenEdit 请求的「文档」分段默认只读预览，点「编辑」才进编辑态；
// 接口概述排在标题行下方（不再是标题右侧的一句话，也不再重复路径与分组）。
func TestReqDocsPreviewThenEdit(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	// 概述的 y 必须大于标题行（用同一行的「复制 cURL」定位，标题文字与树里的同名行不好区分）。
	sumY := float32(0)
	for _, label := range driver.Snapshot().Labels {
		if label.Value == "服务探活" && label.Bounds.X > model.split.Pos {
			sumY = label.Bounds.Y
		}
	}
	titleY := float32(0)
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "复制 cURL" {
			titleY = hit.Bounds.Y
		}
	}
	if sumY == 0 || titleY == 0 {
		t.Fatalf("找不到概述或标题行：sumY=%.1f titleY=%.1f %v", sumY, titleY, painted(driver))
	}
	if sumY <= titleY {
		t.Fatalf("概述应在标题行下方：标题 y=%.1f 概述 y=%.1f", titleY, sumY)
	}

	click(t, driver, "文档")
	pump(t, driver)
	if !shows(driver, "渲染检查") {
		t.Fatalf("文档分段默认应是预览：%v", painted(driver))
	}
	if hasControl(driver, "详细说明") {
		t.Fatal("默认预览态不该出现正文输入框")
	}

	click(t, driver, "编辑")
	pump(t, driver)
	if !model.docsEditing {
		t.Fatal("点「编辑」应进编辑态")
	}
	if !hasControl(driver, "详细说明") {
		t.Fatalf("编辑态应出现正文输入框：%v", painted(driver))
	}

	click(t, driver, "预览")
	pump(t, driver)
	if model.docsEditing {
		t.Fatal("点「预览」应回到预览态")
	}
	if hasControl(driver, "详细说明") || !shows(driver, "渲染检查") {
		t.Fatalf("回到预览应收起输入框并重新排版：%v", painted(driver))
	}

	// 预览区整块也可点：点预览内容同样进编辑态。
	click(t, driver, "文档预览")
	pump(t, driver)
	if !model.docsEditing {
		t.Fatal("点预览区应进编辑态")
	}
}

// TestStatusBarShowsCountsAndMCP 底栏常驻：左侧只读信息，右侧回收站 / MCP / 设置与集合规模。
func TestStatusBarShowsCountsAndMCP(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	if !shows(driver, "离线优先") {
		t.Fatalf("底栏左侧应显示只读信息：%v", painted(driver))
	}
	if !shows(driver, "2 个请求") || !shows(driver, "个环境") {
		t.Fatalf("底栏计数不对：%v", painted(driver))
	}
	for _, label := range []string{"回收站", "MCP", "设置"} {
		if !hasControl(driver, label) {
			t.Fatalf("底栏缺少入口 %q：%v", label, painted(driver))
		}
	}
	if !hasControl(driver, "MCP 未启用") {
		t.Fatalf("MCP 状态点应标注未启用：%v", painted(driver))
	}

	// 底栏 MCP 入口打开的是**独立**弹窗：有 MCP 面板，没有软件设置的左侧分区菜单。
	click(t, driver, "MCP")
	pump(t, driver)
	if !shows(driver, "启用服务") || !shows(driver, "监听地址") {
		t.Fatalf("MCP 入口应打开 MCP 面板：%v", painted(driver))
	}
	if hasControl(driver, "设置分区 外观") || hasControl(driver, "设置分区 关于") {
		t.Fatal("MCP 弹窗里不该出现软件设置的分区菜单")
	}
	// 而「设置」入口回到软件设置的第一屏（先关掉 MCP 面板）。
	click(t, driver, "完成")
	pump(t, driver)
	click(t, driver, "设置")
	pump(t, driver)
	if !shows(driver, "主题") {
		t.Fatalf("设置入口应落在外观分区：%v", painted(driver))
	}
}

// TestMCPStartsAndStops 设置里的开关能起停内嵌 MCP 服务（地址用 0 端口避免占用）。
func TestMCPStartsAndStops(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	// 别把测试用的开关写进后续用例读到的偏好里。
	t.Cleanup(func() {
		model.settings.MCPEnabled = false
		model.settings.MCPAddr = settings.DefaultMCPAddr
		_ = settings.Save(model.settings)
	})

	model.settings.MCPAddr = "127.0.0.1:0"
	model.ui.mcpAddr = "127.0.0.1:0"
	model.ui.mcpEnabled = true
	model.applyMCPEdits()
	if !model.mcp.running {
		t.Fatalf("打开开关后服务应运行：err=%q", model.mcp.err)
	}
	if model.settings.MCPToken == "" {
		t.Fatal("首次启用应生成访问令牌")
	}

	model.ui.mcpEnabled = false
	model.applyMCPEdits()
	if model.mcp.running || model.mcp.addr != "" {
		t.Fatalf("关掉开关后服务应停止：%+v", model.mcp)
	}
}

// TestResponseCopyByClick 响应里的 JSON 行与响应头条目可以点一下复制
// （画布文字不支持拖选，改用「点行复制」+ 工具行的「复制正文」）。
func TestResponseCopyByClick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"n":1}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	// JSON 正文：从第 2 行行首拖到行尾，松手复制选中文本。
	body := gfx.Rect{}
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "响应正文" {
			body = hit.Bounds
			break
		}
	}
	if body.Width == 0 {
		t.Fatalf("找不到响应正文区域：%v", painted(driver))
	}
	const lineH = 18
	lineY := body.Y + 1.5*lineH // 第 2 行（`  "ok": true,`）的中线
	if err := driver.Press(body.X+22, lineY, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("Press: %v", err)
	}
	if err := driver.Move(body.X+22+160, lineY); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Release(body.X+22+160, lineY, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("Release: %v", err)
	}
	pump(t, driver)
	// 松手只保留选区，不自动复制。
	if got := driver.Clipboard(); strings.Contains(got, `"ok": true`) {
		t.Fatalf("松手不该自动复制，剪贴板 = %q", got)
	}
	if err := driver.Chord(gfx.ModifierControl, "c"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if got := driver.Clipboard(); !strings.Contains(got, `"ok": true`) {
		t.Fatalf("Ctrl+C 应复制选中文本，剪贴板 = %q", got)
	}

	// 响应头：点一行复制「名称: 值」。
	click(t, driver, "响应头")
	pump(t, driver)
	click(t, driver, "响应头 Content-Type")
	pump(t, driver)
	if got := driver.Clipboard(); !strings.HasPrefix(got, "Content-Type: ") {
		t.Fatalf("点响应头应复制「名称: 值」，剪贴板 = %q", got)
	}

	// 原始视图同样可选：拖一行 + Ctrl+C。
	click(t, driver, "正文")
	pump(t, driver)
	click(t, driver, "原始")
	pump(t, driver)
	raw := gfx.Rect{}
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "响应正文" {
			raw = hit.Bounds
			break
		}
	}
	if raw.Width == 0 {
		t.Fatalf("原始视图里找不到正文区域：%v", painted(driver))
	}
	y := raw.Y + 0.5*lineH
	if err := driver.Press(raw.X+4, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("Press: %v", err)
	}
	if err := driver.Move(raw.X+raw.Width*0.6, y); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Release(raw.X+raw.Width*0.6, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatalf("Release: %v", err)
	}
	pump(t, driver)
	if err := driver.Chord(gfx.ModifierControl, "c"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if got := driver.Clipboard(); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("原始正文的选中复制应生效，剪贴板 = %q", got)
	}
}

// TestTitlebarIsMergedIntoTheToolbar 顶栏融合：不再有系统标题栏，工具条自己就是标题栏。
//   - 窗口是自定义标题栏模式，条高与 Wails 版的 --app-titlebar-h 一致；
//   - 整条工具条是拖动区，否则无边框窗口拖不动；
//   - 右端 3×46 是 viva 自绘的最小化/最大化/关闭，我们自己的控件不能摆进去。
func TestTitlebarIsMergedIntoTheToolbar(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	config := model.config()
	if config.WindowMode != app.WindowCustomTitlebar {
		t.Fatalf("窗口应是自定义标题栏模式，实际 %v", config.WindowMode)
	}
	if config.TitlebarHeight != titlebarHeight {
		t.Fatalf("标题栏高度应是 %.0f，实际 %.0f", float32(titlebarHeight), config.TitlebarHeight)
	}
	if config.WindowControls == nil {
		t.Fatal("应给出窗口按钮配色，否则按钮不吃主题")
	}

	driver := openWindow(t, model)
	// 窗口按钮自己也在快照里（viva 为它们声明了无障碍节点），它们当然在留白区里。
	style := model.windowControls()
	windowButtons := map[string]bool{
		style.Minimize: true, style.Maximize: true, style.Restore: true, style.CloseName: true,
	}
	check := func() {
		t.Helper()
		snap := driver.Snapshot()
		width := float32(snap.Width)
		dragging := false
		for _, hit := range snap.Hits {
			if hit.MovesWindow && hit.Bounds.Y == 0 && hit.Bounds.Width == width {
				if hit.Bounds.Height != titlebarHeight {
					t.Fatalf("拖动区应正好是标题栏高 %.0f，实际 %.0f", float32(titlebarHeight), hit.Bounds.Height)
				}
				dragging = true
			}
		}
		if !dragging {
			t.Fatalf("标题栏整行应是拖动区（宽 %.0f）：%v", width, painted(driver))
		}

		_, trailing := app.TitlebarInsets()
		if trailing <= 0 {
			return // 这个平台由系统画按钮，没有需要让开的宽度
		}
		for _, hit := range snap.Hits {
			if hit.MovesWindow || hit.IsLabel || hit.Label == "" || windowButtons[hit.Label] {
				continue
			}
			if hit.Bounds.Y+hit.Bounds.Height > titlebarHeight {
				continue // 只看标题栏这一行里的控件
			}
			if hit.Bounds.X+hit.Bounds.Width > width-trailing {
				t.Fatalf("宽 %.0f 时控件「%s」压到窗口按钮上：右边界 %.0f，应止于 %.0f",
					width, hit.Label, hit.Bounds.X+hit.Bounds.Width, width-trailing)
			}
			// 挤成 0 宽就等于消失：标题栏在最小宽度下也不该压没自己的控件。
			if hit.Bounds.Width < 24 {
				t.Fatalf("宽 %.0f 时控件「%s」被压成 %.0f 像素", width, hit.Label, hit.Bounds.Width)
			}
		}
	}
	check()

	// 最小宽度是顶栏最容易拥挤的尺寸：集合/环境/分栏/主题 + 右侧 3×46 的窗口按钮。
	if err := driver.Resize(config.MinWidth, config.MinHeight); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	check()
}

// TestSelectionTracksPointer 选区跟着指针走：拖动越远选得越多，且拿到的是该行的前缀。
// 中文是双宽字符，坐标换算若按「等宽 × 字数」估算就会越拖越偏。
func TestSelectionTracksPointer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"名字":"张三丰","n":1}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	body := gfx.Rect{}
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == "响应正文" {
			body = hit.Bounds
			break
		}
	}
	if body.Width == 0 {
		t.Fatalf("找不到响应正文区域：%v", painted(driver))
	}

	// 中文那一行是 `  "名字": "张三丰",`（带缩进）。
	line := `  "名字": "张三丰",`
	lineY := body.Y + 1.5*18
	last := 0
	for _, dx := range []float32{30, 70, 110} {
		if err := driver.Press(body.X+20, lineY, gfx.PointerButtonPrimary); err != nil {
			t.Fatalf("Press: %v", err)
		}
		if err := driver.Move(body.X+20+dx, lineY); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if err := driver.Release(body.X+20+dx, lineY, gfx.PointerButtonPrimary); err != nil {
			t.Fatalf("Release: %v", err)
		}
		pump(t, driver)
		if err := driver.Chord(gfx.ModifierControl, "c"); err != nil {
			t.Fatalf("Chord: %v", err)
		}
		pump(t, driver)
		got := driver.Clipboard()
		if got == "" || !strings.HasPrefix(line, got) {
			t.Fatalf("dx=%.0f 选中内容应是该行前缀：%q", dx, got)
		}
		if n := len([]rune(got)); n < last {
			t.Fatalf("拖得越远不应选得更少：dx=%.0f n=%d last=%d", dx, n, last)
		} else {
			last = n
		}
	}
	if last <= 1 {
		t.Fatalf("拖动后应选中多个字符，实际 %d", last)
	}
}

// TestMCPAllowListGatesWorkspaces 工作区白名单：没勾选时 AI 看不到集合，勾选后才可见；
// 并核对「一键复制配置」的片段内容。
func TestMCPAllowListGatesWorkspaces(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	t.Cleanup(func() {
		model.settings.MCPAllow = nil
		model.settings.MCPEnabled = false
		_ = settings.Save(model.settings)
	})

	model.refreshMCPAllowRows()
	if len(model.ui.mcpAllow) != 1 {
		t.Fatalf("应列出当前打开的集合：%+v", model.ui.mcpAllow)
	}
	if model.ui.mcpAllow[0].Allowed {
		t.Fatal("默认不该自动授权")
	}
	if dirs := model.mcpRoots(); len(dirs) != 0 {
		t.Fatalf("未授权时不该暴露集合：%v", dirs)
	}

	model.ui.mcpAllow[0].Allowed = true
	model.applyMCPEdits()
	if dirs := model.mcpRoots(); len(dirs) != 1 || dirs[0] != col.Dir {
		t.Fatalf("授权后应暴露该集合：%v", dirs)
	}
	if len(model.settings.MCPAllow) != 1 || model.settings.MCPAllow[0].Dir != col.Dir {
		t.Fatalf("白名单没落到偏好：%+v", model.settings.MCPAllow)
	}

	cfg := model.mcpConfigJSON()
	for _, want := range []string{`"type": "http"`, `"url": "http://`, `"Authorization": "Bearer `} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("配置片段缺少 %q：\n%s", want, cfg)
		}
	}
}

// TestWindowTitle 应用标题栏标题固定为 API-DOC-GO。
func TestWindowTitle(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if title := New(col, "", nil).config().Title; title != "API-DOC-GO" {
		t.Fatalf("窗口标题 = %q", title)
	}
}

// TestChooseCollectionCancelKeepsQuiet 「打开集合」点取消时什么都不做：
// 不再弹「请输入集合目录」的兜底框；只有真的失败（没有对话框后端）才退回手输路径。
func TestChooseCollectionCancelKeepsQuiet(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	if err := driver.QueueOpenDirectory("", app.ErrCanceled); err != nil {
		t.Fatalf("QueueOpenDirectory: %v", err)
	}
	model.chooseCollection()
	pump(t, driver)
	if model.ui.pathOpen || model.ui.openErr != "" {
		t.Fatalf("取消后不该弹手输路径框：pathOpen=%v err=%q", model.ui.pathOpen, model.ui.openErr)
	}

	if err := driver.QueueOpenDirectory("", errors.New("no dialog backend")); err != nil {
		t.Fatalf("QueueOpenDirectory: %v", err)
	}
	model.chooseCollection()
	pump(t, driver)
	if !model.ui.pathOpen {
		t.Fatal("没有目录对话框可用时应退回手输路径")
	}
}

// TestSwitchingRequestDropsPreviousResponse 确认换请求后不会把上一条的响应留在屏上。
func TestSwitchingRequestDropsPreviousResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if !model.hasResp {
		t.Fatal("发送后应有响应")
	}

	click(t, driver, "DELETE 删除")
	pump(t, driver)
	if model.hasResp || model.sending {
		t.Fatalf("换请求后应清掉上一条的响应：hasResp=%v sending=%v", model.hasResp, model.sending)
	}
	if !shows(driver, "尚未发送") {
		t.Fatalf("响应页签应回到空态：%v", painted(driver))
	}
}

// TestMain 把偏好目录指到临时目录：主题切换的持久化测试不该碰用户真实配置。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "viva-settings-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestCreateRequestFromSidebar 走「新建请求」弹窗：落盘、出现在树里、停在请求页签。
func TestCreateRequestFromSidebar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 空集合\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "新建请求")
	pump(t, driver)
	model.ui.newName = "新接口"
	model.ui.newURL = "{{host}}/api/users"
	model.ui.newMethod = "POST"
	model.invalidator.Invalidate()
	pump(t, driver)
	click(t, driver, "创建")

	waitForControl(t, driver, "POST 新接口")
	if model.tab != tabParams {
		t.Fatalf("新请求应停在参数页签，实际 %v", model.tab)
	}
	data, err := os.ReadFile(filepath.Join(dir, "新接口.yml"))
	if err != nil {
		t.Fatalf("请求没有落盘: %v", err)
	}
	if !strings.Contains(string(data), "method: POST") || !strings.Contains(string(data), "{{host}}/api/users") {
		t.Fatalf("请求内容 = %s", data)
	}
}

// TestThemeTogglePersists 确认「日间 / 夜间」切换真的换主题并写进偏好文件。
func TestThemeTogglePersists(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	if !model.isLight() {
		t.Fatal("默认应是日间模式")
	}
	click(t, driver, "切换到夜间")
	pump(t, driver)
	if model.isLight() {
		t.Fatal("点「切换到夜间」后应切到深色")
	}
	if settings.Load().Theme != settings.ThemeDark {
		t.Fatalf("主题没有落盘：%+v", settings.Load())
	}
	click(t, driver, "切换到日间")
	pump(t, driver)
	if !model.isLight() {
		t.Fatal("点「切换到日间」后应切回浅色")
	}
}

// TestSettingsDialogShowsIMEStatus 设置面板要如实说明输入法现状。
func TestSettingsDialogShowsIMEStatus(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "设置")
	pump(t, driver)
	if !shows(driver, "外观") || !shows(driver, "关于") {
		t.Fatalf("设置面板缺少分区导航：%v", painted(driver))
	}
	// 「关于」分区里才显示输入法现状。
	click(t, driver, "设置分区 关于")
	pump(t, driver)
	if !shows(driver, "输入法") {
		t.Fatalf("关于分区应显示输入法：%v", painted(driver))
	}
	if model.ui.imeDetail == "" {
		t.Fatal("应探测输入法状态")
	}
	click(t, driver, "完成")
	pump(t, driver)
	if model.ui.settingsOpen {
		t.Fatal("点「完成」应关闭设置")
	}
}

// TestCreateDocThenEditAndPreview 走新建 → 编辑 → 保存 → 预览：文件真的落盘、预览真的是排版后的标题。
func TestCreateDocThenEditAndPreview(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 文档集合\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 集合树上的入口：点「新建文档」会新建 docs/新建文档.md 并选中它。
	click(t, driver, "新建文档")
	waitForControl(t, driver, "MD 新建文档")
	if model.tab != tabEdit {
		t.Fatalf("新文档应停在编辑页签，实际 %v", model.tab)
	}
	created := filepath.Join(dir, "docs", "新建文档.md")
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("新文档没有落盘: %v", err)
	}

	// 改正文 → 保存：写回的应该是编辑器里的内容。
	model.editor.text = "# 新标题\n\n这是正文。\n"
	model.invalidator.Invalidate()
	pump(t, driver)
	click(t, driver, "保存")
	pump(t, driver)
	data, err := os.ReadFile(created)
	if err != nil {
		t.Fatalf("读回文档: %v", err)
	}
	if !strings.Contains(string(data), "# 新标题") {
		t.Fatalf("保存内容 = %q", data)
	}

	// 右栏是实时预览：标题按 markdown 排版，与左侧源码同屏。
	pump(t, driver)
	if !shows(driver, "新标题") {
		t.Fatalf("预览没有画出标题：%v", painted(driver))
	}
}

// TestSwitchDocKeepsDraft 换走再回来，未保存的改动还在（草稿不丢）。
func TestSwitchDocKeepsDraft(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "MD 指南")
	pump(t, driver)
	model.editor.text = "改了还没保存"
	model.invalidator.Invalidate()
	click(t, driver, "MD 手写笔记")
	pump(t, driver)
	click(t, driver, "MD 指南")
	pump(t, driver)
	if model.editor.text != "改了还没保存" {
		t.Fatalf("回到文档时草稿丢了：%q", model.editor.text)
	}
	if !model.docDirty() {
		t.Fatal("草稿还在时应当仍是未保存状态")
	}
}

// TestEditRequestAndSave 编辑 URL 后保存，文件真的变了，草稿回到干净状态。
func TestEditRequestAndSave(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 待编辑\n")
	writeFile(t, filepath.Join(dir, "请求.yml"), `info:
  name: 请求
  type: http
  seq: 1
meta:
  uid: edit-1
http:
  method: GET
  url: '{{host}}/old'
  params: []
  headers: []
  body:
    type: none
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 请求")
	pump(t, driver)
	model.req.url = "{{host}}/new"
	model.req.method = "PUT"
	model.req.dirty = true
	model.invalidator.Invalidate()
	pump(t, driver)
	click(t, driver, "更多操作")
	pump(t, driver)
	click(t, driver, "保存请求")
	pump(t, driver)
	pump(t, driver)

	data, err := os.ReadFile(filepath.Join(dir, "请求.yml"))
	if err != nil {
		t.Fatalf("读回请求: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "{{host}}/new") || !strings.Contains(text, "method: PUT") {
		t.Fatalf("保存内容 = %s", text)
	}
	if model.req.dirty {
		t.Fatal("保存后草稿应回到干净状态")
	}
}

// TestEnvSelectionAndEdit 切环境、改变量、保存后发送用的是新值。
func TestEnvSelectionAndEdit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 环境集合\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.yml"), `info:
  name: dev
uid: env-1
vars:
  - name: host
    value: http://old
    enabled: true
    secret: false
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openWindow(t, model)

	model.applyEnv("dev")
	if model.vars["host"] != "http://old" {
		t.Fatalf("环境变量没装上：%v", model.vars)
	}
	if settings.Load().Envs[col.Dir] != "dev" {
		t.Fatalf("选中的环境没记住：%+v", settings.Load().Envs)
	}

	model.ui.envPick = "dev"
	model.openEnvEditor()
	if len(model.ui.envRows) != 1 {
		t.Fatalf("环境行 = %d", len(model.ui.envRows))
	}
	model.ui.envRows[0].value = "http://new"
	model.saveEnv()
	if model.vars["host"] != "http://new" {
		t.Fatalf("保存后发送变量应更新：%v", model.vars)
	}
}

// TestSettingsTimeoutPersists 设置里的默认超时立即生效并落盘。
func TestSettingsTimeoutPersists(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "设置")
	pump(t, driver)
	model.ui.timeoutText = "35"
	model.invalidator.Invalidate()
	pump(t, driver)
	if model.settings.TimeoutSec != 35 {
		t.Fatalf("超时没生效：%d", model.settings.TimeoutSec)
	}
	if settings.Load().TimeoutSec != 35 {
		t.Fatalf("超时没落盘：%+v", settings.Load())
	}
}

// TestFilterNarrowsTheList 确认搜索框真的驱动了列表内容，而不只是装饰。
func TestFilterNarrowsTheList(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	if !hasControl(driver, "GET 列表") || !hasControl(driver, "DELETE 删除") {
		t.Fatalf("过滤前应看到两条请求：%v", painted(driver))
	}

	model.filter = "删除"
	model.invalidator.Invalidate()
	pump(t, driver)
	if hasControl(driver, "GET 列表") {
		t.Fatalf("过滤后不应再有列表：%v", painted(driver))
	}
	if !hasControl(driver, "DELETE 删除") {
		t.Fatalf("过滤后应保留删除：%v", painted(driver))
	}
}

// TestTabsKeepPerRequestState 打开两条请求应该得到两个标签，各自保留自己的草稿。
func TestTabsKeepPerRequestState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	if len(model.tabs) != 1 {
		t.Fatalf("点一条请求应开一个标签：%d", len(model.tabs))
	}
	model.req.url = srv.URL + "/edited"
	model.req.dirty = true

	click(t, driver, "DELETE 删除")
	pump(t, driver)
	if len(model.tabs) != 2 || model.curTab != 1 {
		t.Fatalf("第二条请求应开第二个标签并切过去：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}
	if model.req.url == srv.URL+"/edited" {
		t.Fatal("两个标签共用了一份草稿")
	}

	// 点标签回到第一条：未保存的地址还在。
	click(t, driver, "标签 健康检查")
	pump(t, driver)
	if model.req.url != srv.URL+"/edited" || !model.req.dirty {
		t.Fatalf("切回标签后草稿 = %q dirty=%v", model.req.url, model.req.dirty)
	}

	// 关掉当前标签：回到剩下的那个。
	click(t, driver, "关闭 健康检查")
	pump(t, driver)
	if len(model.tabs) != 1 || model.curTab != 0 {
		t.Fatalf("关闭后应剩一个标签：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}
	if !strings.HasSuffix(model.req.url, "/gone") {
		t.Fatalf("关闭后应回到删除请求：%q", model.req.url)
	}

	// 全部关掉：详情回到空状态，列表选择被清空。
	click(t, driver, "关闭 删除")
	pump(t, driver)
	if len(model.tabs) != 0 || model.curTab != -1 {
		t.Fatalf("全关后应没有标签：tabs=%d cur=%d", len(model.tabs), model.curTab)
	}
	if !model.overview || !shows(driver, "存储格式") {
		t.Fatalf("全关后应回到集合概览：%v", painted(driver))
	}
}

// TestSendResultLandsInItsOwnTab 发送后切走，结果应该回到发起它的标签，而不是贴在当前标签上。
func TestSendResultLandsInItsOwnTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			time.Sleep(80 * time.Millisecond)
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	// 不等结果就切到另一条请求。
	click(t, driver, "DELETE 删除")
	pump(t, driver)
	if model.hasResp {
		t.Fatal("新标签不该继承上一条请求的响应")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pump(t, driver)
		if len(model.tabs) == 2 && model.tabs[0].hasResp {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(model.tabs) != 2 || !model.tabs[0].hasResp || model.tabs[0].resp.Status != http.StatusOK {
		t.Fatalf("结果没有回到发起标签：tabs=%d", len(model.tabs))
	}
	if model.hasResp {
		t.Fatal("当前标签不该拿到别条请求的结果")
	}

	click(t, driver, "标签 健康检查")
	pump(t, driver)
	if !model.hasResp || model.resp.Status != http.StatusOK {
		t.Fatalf("切回后应看到该标签的响应：hasResp=%v status=%d", model.hasResp, model.resp.Status)
	}
}

// TestResponseHeadersTab 响应头是独立分段：点「响应头」能看到真实的响应头。
func TestResponseHeadersTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Trace", "trace-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	click(t, driver, "响应头")
	pump(t, driver)
	if !shows(driver, "Content-Length") {
		t.Fatalf("响应头分段没有画响应头：%v", painted(driver))
	}
	// 面板高度有限，多出来的行在滚动区外；模型里必须是完整的响应头。
	found := map[string]string{}
	for _, header := range model.tables.respHeaders {
		found[header.Name] = header.Value
	}
	if found["X-Trace"] != "trace-1" || found["Content-Type"] != "application/json" {
		t.Fatalf("响应头 = %v", found)
	}
}

// TestMultipartFormPersists 表单行能写回文件，multipart 的类型列也保留。
func TestMultipartFormPersists(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	model.req.bodyType = "multipart"
	model.req.form = []*kvRow{
		{enabled: true, name: "title", value: "报告", typ: "text"},
		{enabled: true, name: "file", value: "note.txt", typ: "file"},
	}
	model.req.dirty = true
	model.saveRequest()
	pump(t, driver)

	data, err := os.ReadFile(filepath.Join(col.Dir, "健康检查.yml"))
	if err != nil {
		t.Fatalf("读回请求：%v", err)
	}
	text := string(data)
	for _, want := range []string{"type: multipart", "name: title", "type: file", "value: note.txt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("保存后的文件缺少 %q：\n%s", want, text)
		}
	}
	if model.req.dirty {
		t.Fatal("保存后草稿应回到干净状态")
	}
}

// TestTreeNestsFoldersAndFilters 确认树真的按目录分层、可折叠，搜索保留祖先、去掉无关枝干。
func TestTreeNestsFoldersAndFilters(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 嵌套集合\n")
	writeFile(t, filepath.Join(dir, "接口文档", "自定义规则", "列表.yml"), `info:
  name: 列表
  type: http
  seq: 1
meta:
  uid: nested-list
http:
  method: GET
  url: http://example.invalid/list
  params: []
  headers: []
  body:
    type: none
`)
	writeFile(t, filepath.Join(dir, "接口文档", "首页.yml"), `info:
  name: 首页
  type: http
  seq: 2
meta:
  uid: nested-home
http:
  method: GET
  url: http://example.invalid/home
  params: []
  headers: []
  body:
    type: none
`)

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	pump(t, driver)

	// 可见行：集合根 / 接口文档 / 自定义规则 / 列表 / 首页。
	want := []struct {
		name  string
		depth int
	}{{"嵌套集合", 0}, {"接口文档", 1}, {"自定义规则", 2}, {"列表", 3}, {"首页", 2}}
	if len(model.rows) != len(want) {
		t.Fatalf("树的行数 = %d：%v", len(model.rows), rowNames(model))
	}
	for i, expected := range want {
		if model.rows[i].name != expected.name || model.rows[i].depth != expected.depth {
			t.Fatalf("第 %d 行 = %q/%d，期望 %q/%d：%v", i,
				model.rows[i].name, model.rows[i].depth, expected.name, expected.depth, rowNames(model))
		}
	}

	// 折叠顶层分组：子目录与条目都应该消失。
	model.toggleRow(1)
	pump(t, driver)
	if len(model.rows) != 2 {
		t.Fatalf("折叠后应只剩两行：%v", rowNames(model))
	}
	model.toggleRow(1)
	pump(t, driver)
	if len(model.rows) != 5 {
		t.Fatalf("展开后应恢复：%v", rowNames(model))
	}

	// 搜索命中的条目，保留它的目录祖先，隐藏同一层的其它条目。
	model.filter = "列表"
	model.shownOnce = false
	model.invalidator.Invalidate()
	pump(t, driver)
	names := rowNames(model)
	if len(names) != 4 || names[3] != "列表" {
		t.Fatalf("过滤后的树 = %v", names)
	}
}

// rowNames 是树当前可见行的名字，失败时能看出结构。
func rowNames(model *Model) []string {
	out := make([]string, 0, len(model.rows))
	for _, row := range model.rows {
		out = append(out, row.name)
	}
	return out
}

// TestSecondRootOpensItsRequest 点另一个集合根下的条目：先切集合，再开标签。
func TestSecondRootOpensItsRequest(t *testing.T) {
	first, err := explorer.Load(makeCollection(t, "甲", "甲请求"))
	if err != nil {
		t.Fatalf("Load 甲: %v", err)
	}
	second, err := explorer.Load(makeCollection(t, "乙", "乙请求"))
	if err != nil {
		t.Fatalf("Load 乙: %v", err)
	}
	model := New(first, "", nil)
	model.AddRoot(second)
	driver := openWindow(t, model)
	pump(t, driver)
	if len(model.roots) != 2 {
		t.Fatalf("应有两个集合根：%d", len(model.roots))
	}
	// 默认只展开当前根，先点开第二个根。
	if hasControl(driver, "GET 乙请求") {
		t.Fatal("未展开的根不该显示子条目")
	}
	click(t, driver, "集合 乙")
	pump(t, driver)
	click(t, driver, "GET 乙请求")
	pump(t, driver)

	if model.active != 1 || model.col.Name != "乙" {
		t.Fatalf("应切到第二个集合：active=%d col=%q", model.active, model.col.Name)
	}
	if len(model.tabs) != 1 || !strings.HasSuffix(model.req.url, "/乙请求") {
		t.Fatalf("应打开第二个集合的条目：tabs=%d url=%q", len(model.tabs), model.req.url)
	}
}

func TestScratchRequestSendAndSaveAs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	pump(t, driver)
	click(t, driver, "新建临时请求")
	pump(t, driver)
	if len(model.tabs) != 1 || !model.scratch {
		t.Fatalf("应开一个临时标签：tabs=%d scratch=%v", len(model.tabs), model.scratch)
	}
	model.req.method = "GET"
	model.req.url = srv.URL + "/healthz"
	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if !model.hasResp {
		t.Fatal("临时请求应能直接发送")
	}
	click(t, driver, "更多操作")
	pump(t, driver)
	click(t, driver, "另存为")
	pump(t, driver)
	if !model.ui.newReqOpen || !model.ui.savingScratch {
		t.Fatal("应打开另存为弹窗")
	}
	model.ui.newName = "临时保存"
	click(t, driver, "创建")
	waitForControl(t, driver, "GET 临时保存")
	if model.scratch || len(model.tabs) != 1 {
		t.Fatalf("另存后应换成文件标签：scratch=%v tabs=%d", model.scratch, len(model.tabs))
	}
	if !strings.HasSuffix(model.req.url, "/healthz") {
		t.Fatalf("另存后地址丢了：%q", model.req.url)
	}
}

// TestEnvSecretRoundTrip 敏感变量：值写进 *.secrets.yml，主文件只留标记，发送时能合并回来。
func TestEnvSecretRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 环境\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.yml"), "info:\n  name: dev\nvars:\n  - name: host\n    value: http://127.0.0.1:8080\n    enabled: true\n    secret: false\n")
	writeFile(t, filepath.Join(dir, "environments", "dev.secrets.yml"), "secrets:\n  token: t0k\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openWindow(t, model)

	model.ui.envPick = "dev"
	model.openEnvEditorFor("dev")
	model.ui.envRows = append(model.ui.envRows,
		&kvRow{enabled: true, name: "apiKey", value: "k1", secret: true},
		&kvRow{enabled: true, name: "pageSize", value: "20"},
	)
	model.saveEnv()

	main, err := os.ReadFile(filepath.Join(dir, "environments", "dev.yml"))
	if err != nil {
		t.Fatalf("读主文件: %v", err)
	}
	if strings.Contains(string(main), "k1") {
		t.Fatalf("敏感值不该写进主文件：\n%s", main)
	}
	secrets, err := os.ReadFile(filepath.Join(dir, "environments", "dev.secrets.yml"))
	if err != nil || !strings.Contains(string(secrets), "apiKey: k1") {
		t.Fatalf("密钥文件 = %q, %v", secrets, err)
	}
	vars, err := explorer.LoadEnv(dir, "dev")
	if err != nil || vars["apiKey"] != "k1" || vars["pageSize"] != "20" || vars["token"] != "t0k" {
		t.Fatalf("合并后的变量 = %v, %v", vars, err)
	}
}

// TestTabStripScrollsToActive 标签多了以后，滚动窗口必须让当前标签可见。
func TestTabStripScrollsToActive(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.tabs = make([]*tabState, 10)
	model.overview = false

	model.curTab = 9
	model.clampTabStart(3)
	if model.tabStart != 7 {
		t.Fatalf("最后一个标签应滚到可见：start=%d", model.tabStart)
	}
	model.curTab = 0
	model.clampTabStart(3)
	if model.tabStart != 0 {
		t.Fatalf("回到第一个标签应滚回开头：start=%d", model.tabStart)
	}
}

// TestParseJSONHighlights 响应体是 JSON 时按词法切开：键、字符串、数字、字面量各有类别。
func TestParseJSONHighlights(t *testing.T) {
	lines, folds := parseJSON(`{"name":"neo","age":3,"ok":true}`)
	if len(lines) == 0 {
		t.Fatal("合法 JSON 应能排版")
	}
	seen := map[jsonKind]bool{}
	for _, line := range lines {
		for _, span := range line {
			seen[span.kind] = true
		}
	}
	for _, kind := range []jsonKind{jsonKey, jsonString, jsonNumber, jsonLiteral, jsonPunct} {
		if !seen[kind] {
			t.Fatalf("缺少类别 %v：%v", kind, lines)
		}
	}
	if end, ok := folds[0]; !ok || end == 0 || end >= len(lines) {
		t.Fatalf("对象应有折叠范围：%v", folds)
	}
	if lines, _ := parseJSON("not json"); lines != nil {
		t.Fatal("非 JSON 应回退成纯文本")
	}
}

// TestBodyFieldsPersist 请求体字段表可编辑并写回 body.fields。
func TestBodyFieldsPersist(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	model.req.bodyType = "json"
	model.req.fields = []*fieldRow{{name: "id", typ: "string", required: true, example: "1", desc: "主键"}}
	model.req.dirty = true
	model.saveRequest()
	pump(t, driver)

	data, err := os.ReadFile(filepath.Join(col.Dir, "健康检查.yml"))
	if err != nil {
		t.Fatalf("读回请求：%v", err)
	}
	text := string(data)
	for _, want := range []string{"fields:", "name: id", "type: string", "required: true", "example: \"1\"", "description: 主键"} {
		if !strings.Contains(text, want) {
			t.Fatalf("字段表没有写回 %q：\n%s", want, text)
		}
	}
	if len(model.req.fields) != 1 || model.req.fields[0].name != "id" || !model.req.fields[0].required {
		t.Fatalf("保存后草稿 = %+v", model.req.fields)
	}
}

// TestCommandPaletteFiltersAndRuns 命令面板：按输入过滤，点一行执行命令。
func TestCommandPaletteFiltersAndRuns(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openPalette()
	pump(t, driver)
	if !hasControl(driver, "命令 设置") {
		var labels []string
		for _, hit := range driver.Snapshot().Hits {
			labels = append(labels, hit.Label)
		}
		t.Fatalf("命令面板应列出命令：open=%v hits=%v labels=%v", model.ui.paletteOpen, labels, painted(driver))
	}

	model.ui.paletteQuery = "临时"
	model.invalidator.Invalidate()
	pump(t, driver)
	if !hasControl(driver, "命令 新建临时请求") || hasControl(driver, "命令 设置") {
		t.Fatalf("过滤后的命令 = %v", painted(driver))
	}

	click(t, driver, "命令 新建临时请求")
	pump(t, driver)
	pump(t, driver)
	if model.ui.paletteOpen || len(model.tabs) != 1 || !model.scratch {
		t.Fatalf("执行后应关面板并开临时标签：open=%v tabs=%d scratch=%v",
			model.ui.paletteOpen, len(model.tabs), model.scratch)
	}
}

// TestTrashListAndRestore 删除到回收站后能列出并恢复回集合根。
func TestTrashListAndRestore(t *testing.T) {
	dir := makeCollection(t, "回收站集合", "旧请求")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := explorer.TrashEntry(dir, col.Entries[0]); err != nil {
		t.Fatalf("TrashEntry: %v", err)
	}
	col, err = explorer.Load(dir)
	if err != nil || len(col.Entries) != 0 {
		t.Fatalf("删除后应没有条目：%v %v", col, err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openTrash()
	pump(t, driver)
	if len(model.ui.trashRows) != 1 || !shows(driver, "回收站") {
		t.Fatalf("回收站应有 1 个文件：%+v", model.ui.trashRows)
	}
	model.restoreTrash(model.ui.trashRows[0].File)
	pump(t, driver)
	if len(model.ui.trashRows) != 0 {
		t.Fatalf("恢复后列表应清空：%+v", model.ui.trashRows)
	}
	if _, err := os.Stat(filepath.Join(dir, "旧请求.yml")); err != nil {
		t.Fatalf("文件没有恢复：%v", err)
	}
	if len(model.tabs) != 1 {
		t.Fatalf("恢复后应打开该条目：tabs=%d", len(model.tabs))
	}
}

// TestCurlImportCreatesRequest 粘贴 curl → 落盘成请求并打开。
func TestCurlImportCreatesRequest(t *testing.T) {
	dir := makeCollection(t, "导入集合", "占位")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openCurlImport("")
	pump(t, driver)
	if !hasControl(driver, "导入") {
		t.Fatalf("导入弹窗应有「导入」按钮：%v", painted(driver))
	}
	model.ui.curlText = `curl 'https://api.example.com/v1/users' -X POST -H 'Content-Type: application/json' -d '{"name":"neo"}'`
	model.importCurl()
	pump(t, driver)
	pump(t, driver)

	if !strings.Contains(model.req.url, "api.example.com/v1/users") || model.req.method != "POST" {
		t.Fatalf("导入后的请求 = %s %s", model.req.method, model.req.url)
	}
	data, err := os.ReadFile(filepath.Join(dir, "users.yml"))
	if err != nil {
		t.Fatalf("请求文件没有落盘：%v", err)
	}
	text := string(data)
	for _, want := range []string{"method: POST", "Content-Type", "application/json", "neo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("文件缺少 %q：\n%s", want, text)
		}
	}
	if len(model.tabs) != 1 {
		t.Fatalf("导入后应打开新请求：tabs=%d", len(model.tabs))
	}
}

// TestMoveEntryIntoFolder 拖到分组后文件真的换了目录，标签跟着新路径。
func TestMoveEntryIntoFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 移动集合\n")
	writeFile(t, filepath.Join(dir, "用户", "folder.yml"), "info:\n  name: 用户\n  type: folder\n  seq: 1\n")
	writeFile(t, filepath.Join(dir, "移动我.yml"), `info:
  name: 移动我
  type: http
  seq: 1
meta:
  uid: move-me
http:
  method: GET
  url: http://example.invalid/move
  params: []
  headers: []
  body:
    type: none
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 移动我")
	pump(t, driver)

	model.moveEntry("移动我.yml", "用户")
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(dir, "用户", "移动我.yml")); err != nil {
		t.Fatalf("文件没有移到分组：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "移动我.yml")); !os.IsNotExist(err) {
		t.Fatalf("旧路径仍存在：%v", err)
	}
	if len(model.tabs) != 1 || model.req.url != "http://example.invalid/move" {
		t.Fatalf("标签应跟着新路径：tabs=%d url=%q", len(model.tabs), model.req.url)
	}
}

// TestToggleJSONFold 折叠状态按行记住，再点一次恢复。
func TestToggleJSONFold(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "sample"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	lines, folds := parseJSON(`{"a":{"b":1}}`)
	model.tables.json, model.tables.jsonFolds = lines, folds
	model.tables.jsonFolded = map[int]bool{}
	if len(folds) == 0 {
		t.Fatal("嵌套对象应有折叠范围")
	}
	model.toggleJSONFold(0)
	if !model.tables.jsonFolded[0] {
		t.Fatal("第一次应折叠")
	}
	model.toggleJSONFold(0)
	if model.tables.jsonFolded[0] {
		t.Fatal("第二次应展开")
	}
}

func openWindow(t *testing.T, model *Model) *app.Driver {
	t.Helper()
	driver, err := app.NewDriver(model.config(), nil)
	if err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	t.Cleanup(driver.Close)
	return driver
}

// click 按标签点击一个可交互元素（Viva 里标签就是测试钩子，等价 data-testid）。
func click(t *testing.T, driver *app.Driver, label string) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == label && !hit.IsLabel {
			b := hit.Bounds
			if err := driver.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
				t.Fatalf("点击 %q: %v", label, err)
			}
			return
		}
	}
	t.Fatalf("没有名为 %q 的可点击元素：%v", label, painted(driver))
}

// clickText 按**可见文字**点击：像 ui.Select 这种控件，触发器的无障碍名不是
// 它显示的字，只能顺着标签的包围盒点进去。
func clickText(t *testing.T, driver *app.Driver, text string) {
	t.Helper()
	for _, label := range driver.Snapshot().Labels {
		if label.Value != text {
			continue
		}
		b := label.Bounds
		if err := driver.Click(b.X+b.Width/2, b.Y+b.Height/2); err != nil {
			t.Fatalf("点击文字 %q: %v", text, err)
		}
		return
	}
	t.Fatalf("画面上没有 %q：%v", text, painted(driver))
}

func pump(t *testing.T, driver *app.Driver) {
	t.Helper()
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
}

// waitFor 轮询重绘直到想要的字出现：发送在后台 goroutine 里完成。
func waitFor(t *testing.T, driver *app.Driver, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := driver.Pump(); err != nil {
			t.Fatalf("Pump: %v", err)
		}
		if shows(driver, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等不到 %q：%v", want, painted(driver))
}

func shows(driver *app.Driver, want string) bool {
	for _, label := range driver.Snapshot().Labels {
		if strings.Contains(label.Value, want) {
			return true
		}
	}
	return false
}

// hasControl 按标签找一个可交互元素：列表行把「方法 + 名称」作为整体标签。
func hasControl(driver *app.Driver, label string) bool {
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == label && !hit.IsLabel {
			return true
		}
	}
	return false
}

// waitForControl 轮询重绘直到出现某个可交互元素（新建文档要经过 After + 选中两帧）。
func waitForControl(t *testing.T, driver *app.Driver, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := driver.Pump(); err != nil {
			t.Fatalf("Pump: %v", err)
		}
		if hasControl(driver, label) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等不到控件 %q：%v", label, painted(driver))
}

// painted 汇总界面上的文字，失败时能看出到底画了什么。
func painted(driver *app.Driver) []string {
	labels := driver.Snapshot().Labels
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		out = append(out, label.Value)
	}
	return out
}

// writeCollection 造一个最小集合：清单 + 一条指向测试服务器的请求。
func writeCollection(t *testing.T, base string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 验证集合\n")
	request := fmt.Sprintf(`info:
  name: 健康检查
  type: http
  seq: 1
meta:
  uid: viva-poc-e2e
summary: 服务探活
http:
  method: GET
  url: %q
  headers: []
  body:
    type: none
docs: |-
  ## 渲染检查

  这是 **Markdown** 段落。
`, base+"/healthz")
	writeFile(t, filepath.Join(dir, "健康检查.yml"), request)
	second := fmt.Sprintf(`info:
  name: 删除
  type: http
  seq: 2
meta:
  uid: viva-poc-e2e-delete
http:
  method: DELETE
  url: %q
  headers: []
  body:
    type: none
`, base+"/gone")
	writeFile(t, filepath.Join(dir, "删除.yml"), second)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录 %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
}

// TestArrangeEntriesKeepsSiblingsOrdered 同级拖放会把请求排到目标前 / 后，并按新顺序重写 seq。
func TestArrangeEntriesKeepsSiblingsOrdered(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.syncTree()

	health, gone := -1, -1
	for i, row := range model.rows {
		switch row.key {
		case entryKey(col.Dir, "健康检查.yml"):
			health = i
		case entryKey(col.Dir, "删除.yml"):
			gone = i
		}
	}
	if health < 0 || gone < 0 {
		t.Fatalf("树里找不到两条请求：%+v", model.rows)
	}
	before := ui.TreeMove{From: gone, To: health, Where: ui.TreeDropBefore}
	if !model.canDrop(before.From, before.To, before.Where) {
		t.Fatal("同目录条目前后落点应可放置")
	}
	model.dropRow(before)
	pump(t, driver)

	col, err = explorer.Load(col.Dir)
	if err != nil {
		t.Fatalf("重扫: %v", err)
	}
	if len(col.Entries) != 2 || col.Entries[0].File != "删除.yml" || col.Entries[0].Seq != 1 ||
		col.Entries[1].File != "健康检查.yml" || col.Entries[1].Seq != 2 {
		t.Fatalf("顺序没写回：%+v", col.Entries)
	}
	model.syncTree()
	if model.rows[1].key != entryKey(col.Dir, "删除.yml") || model.rows[2].key != entryKey(col.Dir, "健康检查.yml") {
		t.Fatalf("树没有按新 seq 排：%+v", model.rows)
	}

	if len(model.tabs) != 0 {
		t.Fatalf("排序不该打开标签：%d", len(model.tabs))
	}

	// 再拖回末尾：应回到原顺序。
	after := ui.TreeMove{From: 1, To: 2, Where: ui.TreeDropAfter}
	model.dropRow(after)
	pump(t, driver)
	col, err = explorer.Load(col.Dir)
	if err != nil {
		t.Fatalf("重扫: %v", err)
	}
	if col.Entries[0].File != "健康检查.yml" || col.Entries[1].File != "删除.yml" {
		t.Fatalf("拖回末尾失败：%+v", col.Entries)
	}

	// 跨目录 / 落到集合根上的前后落点都不接受。
	model.syncTree()
	if model.canDrop(1, 0, ui.TreeDropBefore) {
		t.Fatal("集合根不是同级排序的目标")
	}
}

// TestFolderDragReordersAndReparents 分组可拖到同级前后，也可拖进另一个分组；打开中的标签跟着目录改名。
func TestFolderDragReordersAndReparents(t *testing.T) {
	dir := writeCollection(t, "http://example.invalid")
	writeFile(t, filepath.Join(dir, "甲", "folder.yml"), "info:\n  name: 甲\n  seq: 1\n")
	writeFile(t, filepath.Join(dir, "甲", "甲请求.yml"), "info:\n  name: 甲请求\n  type: http\n  seq: 1\nmeta:\n  uid: a-1\nhttp:\n  method: GET\n  url: 'http://example.invalid/a'\n")
	writeFile(t, filepath.Join(dir, "乙", "folder.yml"), "info:\n  name: 乙\n  seq: 2\n")
	writeFile(t, filepath.Join(dir, "乙", "乙请求.yml"), "info:\n  name: 乙请求\n  type: http\n  seq: 1\nmeta:\n  uid: b-1\nhttp:\n  method: GET\n  url: 'http://example.invalid/b'\n")

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.syncTree()

	find := func(key string) int {
		for i, row := range model.rows {
			if row.key == key {
				return i
			}
		}
		t.Fatalf("树里没有 %s：%+v", key, model.rows)
		return -1
	}
	first, second := find(dirKey(col.Dir, "甲")), find(dirKey(col.Dir, "乙"))
	if !model.canDrop(second, first, ui.TreeDropBefore) {
		t.Fatal("同父分组之间应可前后排序")
	}
	model.dropRow(ui.TreeMove{From: second, To: first, Where: ui.TreeDropBefore})
	pump(t, driver)
	col, err = explorer.Load(dir)
	if err != nil {
		t.Fatalf("重扫: %v", err)
	}
	if col.Folders[0].Dir != "乙" || col.Folders[0].Seq != 1 || col.Folders[1].Dir != "甲" || col.Folders[1].Seq != 2 {
		t.Fatalf("分组顺序没写回：%+v", col.Folders)
	}

	// 打开 甲请求，再把整个 甲 分组拖进 乙：标签路径要跟着改。
	click(t, driver, "GET 甲请求")
	pump(t, driver)
	if len(model.tabs) != 1 || model.tabs[0].file != "甲/甲请求.yml" {
		t.Fatalf("标签没打开：%+v", model.tabs)
	}
	model.syncTree()
	from, into := find(dirKey(col.Dir, "甲")), find(dirKey(col.Dir, "乙"))
	if !model.canDrop(from, into, ui.TreeDropInside) {
		t.Fatal("分组应可拖进另一个分组")
	}
	model.dropRow(ui.TreeMove{From: from, To: into, Where: ui.TreeDropInside})
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(dir, "乙", "甲", "甲请求.yml")); err != nil {
		t.Fatalf("目录没移动: %v", err)
	}
	if len(model.tabs) != 1 || model.tabs[0].file != "乙/甲/甲请求.yml" || model.tabs[0].entry == nil {
		t.Fatalf("标签没跟着目录改名：%+v", model.tabs)
	}
	// 草稿也跟着换路径：保存必须写进新目录，不能在旧位置又写一份。
	model.req.url = "http://example.invalid/moved"
	model.req.dirty = true
	model.saveRequest()
	pump(t, driver)
	data, err := os.ReadFile(filepath.Join(dir, "乙", "甲", "甲请求.yml"))
	if err != nil || !strings.Contains(string(data), "/moved") {
		t.Fatalf("保存没落到新路径：%v\n%s", err, data)
	}
	// 移进自己 / 子孙不接受。
	model.syncTree()
	if model.canDrop(find(dirKey(col.Dir, "乙")), find(dirKey(col.Dir, "乙/甲")), ui.TreeDropInside) {
		t.Fatal("祖先不能拖进自己的子孙")
	}
}

// TestNewFolderRenameAndAutoExpand 分组可在树里新建 / 重命名；把条目拖进折叠的分组后目标会展开。
func TestNewFolderRenameAndAutoExpand(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openNewFolderIn("")
	model.ui.folderName = "分组甲"
	model.createFolder()
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(col.Dir, "分组甲", "folder.yml")); err != nil {
		t.Fatalf("分组没建出来: %v", err)
	}

	model.openRenameFolder(&explorer.Folder{Dir: "分组甲", Name: "分组甲"})
	model.ui.renameName = "分组乙"
	model.renameEntry()
	pump(t, driver)
	data, _ := os.ReadFile(filepath.Join(col.Dir, "分组甲", "folder.yml"))
	if !strings.Contains(string(data), "name: 分组乙") {
		t.Fatalf("分组没重命名：\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(col.Dir, "分组甲")); err != nil {
		t.Fatalf("重命名不该动目录名: %v", err)
	}

	// 给分组放一条内容，让它成为可展开的枝干；折叠后再把根目录的请求拖进去。
	writeFile(t, filepath.Join(col.Dir, "分组甲", "内部.yml"),
		"info:\n  name: 内部\n  type: http\n  seq: 1\nmeta:\n  uid: inside\nhttp:\n  method: GET\n  url: ''\n")
	if !model.reloadCollection() {
		t.Fatal("重扫失败")
	}
	pump(t, driver)
	model.expanded[dirKey(col.Dir, "分组甲")] = false
	model.treeDirty = true
	model.syncTree()
	folder, entry := -1, -1
	for i, row := range model.rows {
		switch row.key {
		case dirKey(col.Dir, "分组甲"):
			folder = i
		case entryKey(col.Dir, "健康检查.yml"):
			entry = i
		}
	}
	if folder < 0 || entry < 0 {
		t.Fatalf("树里找不到目标行：%+v", model.rows)
	}
	if model.rows[folder].expanded {
		t.Fatal("分组应该是折叠的")
	}
	if !model.canDrop(entry, folder, ui.TreeDropInside) {
		t.Fatal("请求应能拖进分组")
	}
	// 悬停到点：分组在下一帧被展开（等价于拖拽停在它上面 600ms）。
	model.dragHoverSince = time.Now().Add(-time.Second)
	model.canDrop(entry, folder, ui.TreeDropInside)
	model.flushHoverExpand()
	if !model.expanded[dirKey(col.Dir, "分组甲")] {
		t.Fatal("悬停后分组应自动展开")
	}
	model.expanded[dirKey(col.Dir, "分组甲")] = false
	model.treeDirty = true
	model.syncTree()
	model.dropRow(ui.TreeMove{From: entry, To: folder, Where: ui.TreeDropInside})
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(col.Dir, "分组甲", "健康检查.yml")); err != nil {
		t.Fatalf("请求没移动: %v", err)
	}
	if !model.expanded[dirKey(col.Dir, "分组甲")] {
		t.Fatal("落进折叠分组后应自动展开")
	}
	if len(model.tabs) != 0 {
		t.Fatalf("拖进分组不该顺手打开标签：%d", len(model.tabs))
	}
}

// TestCrossCollectionDragAndFolderOps 跨集合拖放、克隆与删除分组都落到磁盘，标签跟着收敛。
func TestCrossCollectionDragAndFolderOps(t *testing.T) {
	srcDir := writeCollection(t, "http://example.invalid")
	writeFile(t, filepath.Join(srcDir, "整组", "folder.yml"), "info:\n  name: 整组\n  type: folder\n  seq: 1\n")
	writeFile(t, filepath.Join(srcDir, "整组", "组内.yml"), "info:\n  name: 组内\n  type: http\n  seq: 1\nmeta:\n  uid: inner\nhttp:\n  method: GET\n  url: 'http://example.invalid/inner'\n")
	writeFile(t, filepath.Join(srcDir, "用户", "folder.yml"), "info:\n  name: 用户\n  type: folder\n  seq: 2\n")
	writeFile(t, filepath.Join(srcDir, "用户", "用户请求.yml"), "info:\n  name: 用户请求\n  type: http\n  seq: 1\nmeta:\n  uid: user-req\nhttp:\n  method: GET\n  url: 'http://example.invalid/user'\n")
	dstDir := writeCollection(t, "http://example.invalid")
	dstCol, err := explorer.Load(dstDir)
	if err != nil {
		t.Fatalf("Load dst: %v", err)
	}
	srcCol, err := explorer.Load(srcDir)
	if err != nil {
		t.Fatalf("Load src: %v", err)
	}
	model := New(srcCol, "", nil)
	model.AddRoot(dstCol)
	driver := openWindow(t, model)
	model.syncTree()

	// 打开源集合里整组下的请求，跨集合移走后标签应消失。
	click(t, driver, "GET 组内")
	pump(t, driver)
	if len(model.tabs) != 1 {
		t.Fatalf("标签没打开：%+v", model.tabs)
	}
	from := -1
	for i, row := range model.rows {
		if row.folder != nil && row.folder.Dir == "整组" {
			from = i
		}
	}
	if from < 0 {
		t.Fatalf("树里没有整组：%+v", model.rows)
	}
	// 目标集合根行是第二个根（active 仍是第一个）。
	to := -1
	for i, row := range model.rows {
		if row.kind == nodeRoot && row.root == 1 {
			to = i
		}
	}
	if to < 0 {
		t.Fatalf("树里没有第二个集合根：%+v", model.rows)
	}
	if !model.canDrop(from, to, ui.TreeDropInside) {
		t.Fatal("分组应能拖到另一个集合根上")
	}
	model.dropRow(ui.TreeMove{From: from, To: to, Where: ui.TreeDropInside})
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(dstDir, "整组", "组内.yml")); err != nil {
		t.Fatalf("分组没跨集合移动: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "整组")); !os.IsNotExist(err) {
		t.Fatal("源分组还在")
	}
	if len(model.tabs) != 0 {
		t.Fatalf("移走的条目标签应关闭：%+v", model.tabs)
	}

	// 克隆源集合的「用户」分组。
	model.syncTree()
	model.active = 0
	var users *explorer.Folder
	for _, folder := range model.roots[0].col.Folders {
		if folder.Name == "用户" {
			users = folder
		}
	}
	model.cloneFolder(users)
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(srcDir, "用户 副本1", "folder.yml")); err != nil {
		t.Fatalf("克隆分组没生成: %v", err)
	}

	// 删除克隆出来的分组：目录进回收站，标签若在其中会被关掉。
	model.askDeleteFolder(&explorer.Folder{Dir: "用户 副本1", Name: "用户 副本1"})
	model.deleteFolder()
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(srcDir, "用户 副本1")); !os.IsNotExist(err) {
		t.Fatal("分组没删掉")
	}
	items, err := explorer.ListTrash(srcDir)
	if err != nil || len(items) == 0 {
		t.Fatalf("回收站里应有被删的分组：%+v %v", items, err)
	}
}

// TestTwoRootsWithSamePathsShareNoTreeKeys 两个集合有同名条目时，树键必须按根区分；
// 否则 Viva 会在一次绘制里报「two siblings have the key ...」并 panic。
func TestTwoRootsWithSamePathsShareNoTreeKeys(t *testing.T) {
	first := writeCollection(t, "http://example.invalid")
	second := writeCollection(t, "http://example.invalid")
	colA, err := explorer.Load(first)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	colB, err := explorer.Load(second)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	model := New(colA, "", nil)
	model.AddRoot(colB)
	// 两个根都展开，让同名条目同时进入可见行。
	model.expanded["root:"+colA.Dir] = true
	model.expanded["root:"+colB.Dir] = true
	openWindow(t, model)
	model.syncTree()

	seen := map[string]bool{}
	for _, row := range model.rows {
		if seen[row.key] {
			t.Fatalf("树键重复：%s", row.key)
		}
		seen[row.key] = true
	}
	if len(model.rows) != 6 {
		t.Fatalf("两个根的条目应都在树里：%d 行", len(model.rows))
	}
	// 一个根的分组折叠状态不会串到另一个根。
	if dirKey(colA.Dir, "") == dirKey(colB.Dir, "") {
		t.Fatal("不同集合的同名目录键应不同")
	}
}

// TestCollectionPickerSwitchesRoots 工具条的集合下拉能列出已打开的根，并切到选中的那个。
func TestCollectionPickerSwitchesRoots(t *testing.T) {
	first := writeCollection(t, "http://example.invalid")
	second := writeCollection(t, "http://example.invalid")
	colA, err := explorer.Load(first)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	colB, err := explorer.Load(second)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	model := New(colA, "", nil)
	model.AddRoot(colB)
	driver := openWindow(t, model)
	model.syncTree()

	// 下拉按钮显示当前集合名；点开后重名集合带目录名尾巴。
	current := "验证集合 · " + filepath.Base(colA.Dir)
	click(t, driver, current)
	pump(t, driver)
	target := "验证集合 · " + filepath.Base(colB.Dir)
	click(t, driver, target)
	pump(t, driver)
	if model.active != 1 || model.col.Dir != colB.Dir {
		t.Fatalf("下拉没切到第二个集合：active=%d col=%s", model.active, model.col.Dir)
	}
	// 按钮跟着显示新集合（重名所以带尾巴）。
	click(t, driver, target)
	pump(t, driver)
	if model.active != 1 {
		t.Fatal("点当前集合不应改变活动根")
	}
}

// TestStartsWithoutCollection 双击启动（没有 -collection）时窗口照常打开，
// 首屏给「打开集合」；打开后占位根被真实集合替换，不留下「未打开集合」。
func TestStartsWithoutCollection(t *testing.T) {
	model := New(nil, "", nil)
	driver := openWindow(t, model)

	if !shows(driver, "尚未打开集合") {
		t.Fatalf("空状态首屏没画出来：%v", painted(driver))
	}
	if !hasControl(driver, "打开集合") {
		t.Fatal("空状态里没有「打开集合」入口")
	}

	dir := writeCollection(t, "http://example.invalid")
	model.openCollection(dir)
	pump(t, driver)
	if model.col.Dir != dir {
		t.Fatalf("打开集合失败：col=%q", model.col.Dir)
	}
	if len(model.roots) != 1 || model.roots[0].col.Dir != dir {
		t.Fatalf("占位根没被替换：%+v", model.roots)
	}
	if shows(driver, "尚未打开集合") {
		t.Fatal("打开集合后还停在空状态")
	}
	if !shows(driver, "健康检查") {
		t.Fatalf("集合内容没画出来：%v", painted(driver))
	}
}

// TestRequestResponseSplitLayout 设置里的「左右」分栏应把响应放到请求的右侧。
func TestRequestResponseSplitLayout(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	find := func(want string) (x, y float32, ok bool) {
		for _, label := range driver.Snapshot().Labels {
			if label.Value == want {
				return label.Bounds.X, label.Bounds.Y, true
			}
		}
		return 0, 0, false
	}
	reqX, reqY, okReq := find("参数")
	respX, respY, okResp := find("响应")
	if !okReq || !okResp {
		t.Fatalf("找不到分栏标题：req=%v resp=%v", okReq, okResp)
	}
	if !(respY > reqY) {
		t.Fatalf("默认应为上下分栏：参数 y=%.1f 响应 y=%.1f", reqY, respY)
	}

	// 分段按钮：点「左右分栏」那一段才换方向；点当前已选中的「上下分栏」不算切换。
	click(t, driver, "左右分栏")
	pump(t, driver)
	if !model.settings.SplitSideBySide {
		t.Fatal("点「左右分栏」应切到左右分栏")
	}
	click(t, driver, "上下分栏")
	pump(t, driver)
	if model.settings.SplitSideBySide {
		t.Fatal("点「上下分栏」应切回上下分栏")
	}
	click(t, driver, "左右分栏")
	pump(t, driver)
	if !model.settings.SplitSideBySide {
		t.Fatal("再点「左右分栏」应切回左右分栏")
	}
	reqX, reqY, _ = find("参数")
	respX, respY, _ = find("响应")
	if !(respX > reqX+100) {
		t.Fatalf("左右分栏时响应应在请求右侧：参数 x=%.1f 响应 x=%.1f", reqX, respX)
	}
	if respY > 400 {
		t.Fatalf("左右分栏时响应不应掉到下方：响应 y=%.1f", respY)
	}
	// 分段页签（参数 / 请求体 / 请求头 / 认证 / 文档）只占请求列：
	// 它曾画在分栏之上、横跨整幅宽度，看起来像独立于两栏的一条横幅。
	lastTabRight := float32(0)
	for _, hit := range driver.Snapshot().Hits {
		if hit.Role == gfx.RoleTab && hit.Label == "文档" {
			lastTabRight = hit.Bounds.X + hit.Bounds.Width
		}
	}
	if lastTabRight == 0 {
		t.Fatalf("找不到分段页签「文档」：%v", painted(driver))
	}
	if lastTabRight > respX {
		t.Fatalf("分段页签应止于响应列左侧：页签右边界 %.1f，响应 x=%.1f", lastTabRight, respX)
	}
}

// TestCurlImportIntoFolderAndName 粘贴 curl 时可指定落点分组与请求名；
// 导入后展开目标分组并打开新请求。
func TestCurlImportIntoFolderAndName(t *testing.T) {
	dir := writeCollection(t, "http://example.invalid")
	writeFile(t, filepath.Join(dir, "分组", "folder.yml"), "info:\n  name: 分组\n  seq: 1\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openCurlImport("分组")
	if model.ui.curlFolder != "分组" {
		t.Fatalf("预设分组 = %q", model.ui.curlFolder)
	}
	model.ui.curlName = "自定义导入"
	model.ui.curlText = `curl 'http://example.invalid/v1/things' -H 'X-A: 1'`
	model.importCurl()
	pump(t, driver)
	pump(t, driver)

	data, err := os.ReadFile(filepath.Join(dir, "分组", "自定义导入.yml"))
	if err != nil {
		t.Fatalf("请求没落到分组：%v", err)
	}
	if !strings.Contains(string(data), "X-A") || !strings.Contains(string(data), "/v1/things") {
		t.Fatalf("导入内容不对：\n%s", data)
	}
	if !model.expanded[dirKey(col.Dir, "分组")] {
		t.Fatal("导入到折叠分组后应自动展开")
	}
	if len(model.tabs) != 1 || model.tabs[0].file != "分组/自定义导入.yml" {
		t.Fatalf("导入后应打开新请求：%+v", model.tabs)
	}
}

// TestRequestMenuUtilities 地址栏「更多操作」包含导入/导出/代码生成/格式化 URL。
func TestRequestMenuUtilities(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	model.req.url = "  http://example.invalid/a?x=1&&y=2  "
	model.req.dirty = true
	click(t, driver, "更多操作")
	pump(t, driver)
	for _, item := range []string{"导入 CURL", "导出 CURL", "代码生成", "格式化 URL"} {
		if !hasControl(driver, item) {
			t.Fatalf("菜单缺少 %q：%v", item, painted(driver))
		}
	}
	click(t, driver, "格式化 URL")
	pump(t, driver)
	if model.req.url != "http://example.invalid/a?x=1&y=2" {
		t.Fatalf("格式化 URL = %q", model.req.url)
	}

	click(t, driver, "更多操作")
	pump(t, driver)
	click(t, driver, "代码生成")
	pump(t, driver)
	if !model.codeOpen || !strings.Contains(model.codeText, "curl") {
		t.Fatalf("代码生成没打开或片段不对：open=%v code=%q", model.codeOpen, model.codeText)
	}
}

// TestResponseRawView 响应可切回原始报文：关闭格式化后原样显示响应体。
func TestResponseRawView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"n":1}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if len(model.tables.json) == 0 {
		t.Fatal("默认应解析成可折叠 JSON")
	}
	click(t, driver, "原始")
	pump(t, driver)
	if !model.rawResp {
		t.Fatal("点「原始」应切到原始报文")
	}
	if !shows(driver, `{"ok":true,"n":1}`) {
		t.Fatalf("原始报文没显示原样正文：%v", painted(driver))
	}
}

// TestRequestHistoryRecordsAndReopens 每发送一次记一条历史：
// 记录落到本机、弹窗列得出来、点一行能回到那条请求、清空后为空。
func TestRequestHistoryRecordsAndReopens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	send := func(want int) {
		t.Helper()
		// 上一次还在发的时候按钮是「发送中…」，先等它回来。
		for deadline := time.Now().Add(3 * time.Second); model.sending && time.Now().Before(deadline); {
			pump(t, driver)
			time.Sleep(10 * time.Millisecond)
		}
		click(t, driver, "发送")
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			pump(t, driver)
			if len(history.Load()) >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("等不到第 %d 条历史：%+v", want, history.Load())
	}
	send(1)
	send(2)

	entries := history.Load()
	if len(entries) != 2 {
		t.Fatalf("应记两条：%+v", entries)
	}
	if entries[0].Status != 200 || !strings.Contains(entries[0].URL, "/healthz") {
		t.Fatalf("记录内容不对：%+v", entries[0])
	}
	if entries[0].File == "" || entries[0].Dir != col.Dir {
		t.Fatalf("记录应带请求路径与集合根：%+v", entries[0])
	}
	if hits != 2 {
		t.Fatalf("应该真发了两次，实际 %d", hits)
	}

	// 关掉标签，再从历史点回来。
	model.closeAllTabs()
	pump(t, driver)
	if entry := model.activeEntry(); entry != nil && entry.File == entries[0].File {
		t.Fatal("关掉标签后不该还停在请求上")
	}
	click(t, driver, "历史")
	pump(t, driver)
	if !shows(driver, "/healthz") {
		t.Fatalf("历史弹窗没列出记录：%v", painted(driver))
	}
	click(t, driver, "历史 "+entries[0].URL)
	pump(t, driver)
	if model.ui.historyOpen {
		t.Fatal("点历史应关掉弹窗")
	}
	if model.activeEntry() == nil || model.activeEntry().File != entries[0].File {
		t.Fatal("点历史应打开那条请求")
	}

	// 再打开，清空。
	click(t, driver, "历史")
	pump(t, driver)
	click(t, driver, "清空历史")
	pump(t, driver)
	if len(history.Load()) != 0 {
		t.Fatalf("清空后还有记录：%+v", history.Load())
	}
}

// TestExportContentCoversTheCollection 导出的两种格式都覆盖当前集合：
// Markdown 有标题与请求名，HTML 是完整文档且目录锚点对得上。
func TestExportContentCoversTheCollection(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)

	md, ext, filter := model.exportContent("md")
	if ext != "md" || filter.Pattern != "*.md" {
		t.Fatalf("Markdown 的扩展名/过滤器不对：%s %+v", ext, filter)
	}
	for _, want := range []string{"# 验证集合", "GET 健康检查", "DELETE 删除"} {
		if !strings.Contains(md, want) {
			t.Fatalf("Markdown 缺 %q：\n%s", want, md)
		}
	}

	htmlOut, ext, filter := model.exportContent("html")
	if ext != "html" || filter.Pattern != "*.html" {
		t.Fatalf("HTML 的扩展名/过滤器不对：%s %+v", ext, filter)
	}
	if !strings.HasPrefix(htmlOut, "<!DOCTYPE html>") || !strings.HasSuffix(htmlOut, "</html>\n") {
		t.Fatal("HTML 不完整")
	}
	if !strings.Contains(htmlOut, "健康检查") || !strings.Contains(htmlOut, `class="toc"`) {
		t.Fatal("HTML 缺正文或目录")
	}
	if strings.Count(htmlOut, "<script>") != 1 {
		t.Fatal("只应有一段内嵌脚本")
	}
}

// TestEntryMenuVisibility 行尾「···」的可见性：行悬停、按钮自己悬停、菜单开着都显示。
// 少了「按钮自己悬停」这一条就会闪：指针压上去的那一帧判断成不可见，下一帧又回来。
func TestEntryMenuVisibility(t *testing.T) {
	cases := []struct {
		name        string
		row, button bool
		menu        bool
		want        bool
	}{
		{"指针在行上", true, false, false, true},
		{"指针在按钮上", false, true, false, true},
		{"指针在别处", false, false, false, false},
		{"指针离开但菜单开着", false, false, true, true},
	}
	for _, item := range cases {
		if got := entryMenuVisible(item.row, item.button, item.menu); got != item.want {
			t.Fatalf("%s：应显示 = %v，实际 %v", item.name, item.want, got)
		}
	}
}

// TestEntryMenuButtonStaysUnderPointer 行尾「···」指过去要一直亮着。
// 曾经的写法是「悬停才声明按钮」：指针一落到按钮上，Hovered() 变成按钮、
// 下一帧按钮消失，指针又回到行上，于是按钮反复出现/消失，看着一闪一闪。
func TestEntryMenuButtonStaysUnderPointer(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	row := findHit(driver, "GET 健康检查")
	if row.Width == 0 {
		t.Fatalf("找不到请求行：%v", painted(driver))
	}
	// 先落在行上（离开行的最右侧，别一上来就压到按钮）。
	if err := driver.Move(row.X+row.Width*0.5, row.Y+row.Height*0.5); err != nil {
		t.Fatalf("Move: %v", err)
	}
	pump(t, driver)
	more := findHit(driver, "更多 健康检查")
	if more.Width == 0 {
		t.Fatalf("悬停行后应出现「···」：%v", painted(driver))
	}
	// 移到按钮正中：按钮必须还在，并且能点开菜单。
	if err := driver.Move(more.X+more.Width/2, more.Y+more.Height/2); err != nil {
		t.Fatalf("Move: %v", err)
	}
	pump(t, driver)
	still := findHit(driver, "更多 健康检查")
	if still.Width == 0 {
		t.Fatalf("指针停在按钮上时按钮不该消失：%v", painted(driver))
	}
	if err := driver.Click(still.X+still.Width/2, still.Y+still.Height/2); err != nil {
		t.Fatalf("Click: %v", err)
	}
	pump(t, driver)
	if !shows(driver, "删除到回收站") {
		t.Fatalf("点「···」应打开条目菜单：%v", painted(driver))
	}
}

// clickRightOf 点「横坐标在 x 右侧」的那个同名命中——树与弹窗里可能有同名标签。
func clickRightOf(t *testing.T, driver *app.Driver, label string, x float32) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label != label || hit.IsLabel || hit.Bounds.X < x {
			continue
		}
		if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatalf("点击 %q: %v", label, err)
		}
		return
	}
	t.Fatalf("右侧没有名为 %q 的可点元素：%v", label, painted(driver))
}

// findHit 按标签找可命中区域（找不到返回零矩形）。
func findHit(driver *app.Driver, label string) gfx.Rect {
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == label && !hit.IsLabel {
			return hit.Bounds
		}
	}
	return gfx.Rect{}
}

// TestRunCollectionFromDialog 运行器：勾两条（一条成功一条 500）跑完，
// 结果逐条回来，失败计数对得上。
func TestRunCollectionFromDialog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "gone") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openRunner()
	pump(t, driver)
	if len(model.run.Rows) != 2 {
		t.Fatalf("应列出两条 http 请求：%+v", model.run.Rows)
	}
	click(t, driver, "运行")
	// 结果由 Page 每帧搬运，等它跑完。
	deadline := time.Now().Add(5 * time.Second)
	for model.run.Running && time.Now().Before(deadline) {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}
	if model.run.Running {
		t.Fatal("等不到运行结束")
	}
	if len(model.run.Results) != 2 {
		t.Fatalf("应跑完两条：%+v", model.run.Results)
	}
	failed := 0
	for _, result := range model.run.Results {
		if result.Failed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("应有一条失败：%+v", model.run.Results)
	}
	if !strings.Contains(model.run.Note, "失败 1 条") {
		t.Fatalf("小结不对：%q", model.run.Note)
	}
	if !shows(driver, "500") {
		t.Fatalf("结果里应看到 500：%v", painted(driver))
	}
}

// TestRunStopsOnFirstFailure 勾了「遇到失败即停」就只跑第一条。
func TestRunStopsOnFirstFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openRunner()
	model.run.StopOnFail = true
	pump(t, driver)
	click(t, driver, "运行")
	deadline := time.Now().Add(5 * time.Second)
	for model.run.Running && time.Now().Before(deadline) {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}
	if len(model.run.Results) != 1 {
		t.Fatalf("应当第一条就停：%+v", model.run.Results)
	}
}

// TestReorderItems 列表拖拽的搬运语义：把若干行整体挪到 to 之前（to 按拖动前的下标算）。
func TestReorderItems(t *testing.T) {
	cases := []struct {
		name string
		list []string
		rows []int
		to   int
		want []string
	}{
		{"往后挪一行", []string{"a", "b", "c"}, []int{0}, 2, []string{"b", "a", "c"}},
		{"挪到末尾", []string{"a", "b", "c"}, []int{0}, 3, []string{"b", "c", "a"}},
		{"往前挪一行", []string{"a", "b", "c"}, []int{2}, 0, []string{"c", "a", "b"}},
		{"多行一起挪", []string{"a", "b", "c", "d"}, []int{0, 1}, 4, []string{"c", "d", "a", "b"}},
		{"挪到原位不动", []string{"a", "b", "c"}, []int{1}, 1, []string{"a", "b", "c"}},
	}
	for _, item := range cases {
		list := append([]string(nil), item.list...)
		reorderItems(list, item.rows, item.to)
		if strings.Join(list, ",") != strings.Join(item.want, ",") {
			t.Fatalf("%s：得到 %v，应为 %v", item.name, list, item.want)
		}
	}
	if reorderItems([]string{}, []int{0}, 0) {
		t.Fatal("空列表不该报告改动")
	}
	if reorderItems([]string{"a"}, []int{9}, 0) {
		t.Fatal("越界下标不该报告改动")
	}
}

// TestEnvRowsCanBeReordered 环境变量表接上了 List 的重排回调：交换后保存，文件里的顺序跟着换。
func TestEnvRowsCanBeReordered(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	writeFile(t, filepath.Join(dir, "environments", "dev.yml"), `info:
  name: dev
vars:
  - name: host
    value: http://127.0.0.1:8080
  - name: token
    value: abc
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "dev", nil)
	driver := openWindow(t, model)
	model.openEnvEditorFor("dev")
	pump(t, driver)
	pump(t, driver) // 弹窗在下一帧才换成环境表（模态切换有一帧过渡）
	if len(model.ui.envRows) != 2 || model.ui.envRows[0].name != "host" {
		t.Fatalf("变量表没读出来：%+v", model.ui.envRows)
	}
	if model.ui.envList.Reorder == nil {
		t.Fatal("变量表应挂上拖拽重排回调")
	}
	// 注意：环境弹窗在无头驱动里不进快照（含 ui.List 的模态不发 hit），
	// 所以这里只测模型侧——重排回调与落盘顺序；行尾 ↑↓ 与拖拽走同一份数据。

	// 模拟拖拽结束时的回调（手势本身是 viva 的 List 实现的）。
	model.ui.envList.Reorder([]int{0}, 2)
	pump(t, driver)
	if model.ui.envRows[0].name != "token" {
		t.Fatalf("重排后顺序不对：%+v", model.ui.envRows)
	}

	model.saveEnv()
	pump(t, driver)
	raw, err := os.ReadFile(filepath.Join(dir, "environments", "dev.yml"))
	if err != nil {
		t.Fatalf("读回环境文件: %v", err)
	}
	if strings.Index(string(raw), "token") > strings.Index(string(raw), "host") {
		t.Fatalf("保存后顺序没跟着换：\n%s", raw)
	}
}

// TestMockRuleEditsAndServes 桩规则：在弹窗里改状态码与响应体，保存到请求文件的 mock: 键，
// 桩服务按新规则应答。
func TestMockRuleEditsAndServes(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.openMockRule("健康检查.yml")
	pump(t, driver)
	model.ui.mockRuleStatus = "418"
	model.ui.mockRuleDelay = "1"
	model.ui.mockRuleBody = `{"tea":true}`
	pump(t, driver)
	click(t, driver, "保存")
	pump(t, driver)

	raw, err := os.ReadFile(filepath.Join(dir, "健康检查.yml"))
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if !strings.Contains(string(raw), "mock:") || !strings.Contains(string(raw), "418") {
		t.Fatalf("规则没落盘：\n%s", raw)
	}

	model.settings.MockEnabled = true
	model.settings.MockAddr = "127.0.0.1:0"
	model.restartMock()
	defer model.stopMock()
	if !model.mock.running {
		t.Fatalf("桩没起来：%+v", model.mock)
	}
	resp, err := http.Get("http://" + model.mock.addr + "/healthz")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 418 {
		t.Fatalf("状态码没按规则返回：%d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应: %v", err)
	}
	if !strings.Contains(string(body), "tea") {
		t.Fatalf("响应体没按规则覆盖：%s", body)
	}
}

// TestTrashBatchRestoreAndPurge 回收站批量操作：勾选后恢复会回集合根，
// 彻底删除要二次确认，删掉的文件真的从磁盘上没了。
func TestTrashBatchRestoreAndPurge(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 把两条请求都丢进回收站。
	model.trashEntry("健康检查.yml")
	pump(t, driver)
	model.trashEntry("删除.yml")
	pump(t, driver)
	model.openTrash()
	pump(t, driver)
	if len(model.ui.trashRows) != 2 {
		t.Fatalf("回收站应有两条：%+v", model.ui.trashRows)
	}
	if !shows(driver, "回收站") {
		t.Fatalf("弹窗没打开：%v", painted(driver))
	}

	// 批量恢复：两条都回来。
	click(t, driver, "全选")
	pump(t, driver)
	click(t, driver, "恢复选中")
	pump(t, driver)
	if len(model.ui.trashRows) != 0 {
		t.Fatalf("恢复后回收站应为空：%+v", model.ui.trashRows)
	}
	for _, name := range []string{"健康检查.yml", "删除.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("恢复后的 %s 不在：%v", name, err)
		}
	}

	// 再丢一次，这次彻底删除。
	model.trashEntry("健康检查.yml")
	pump(t, driver)
	model.openTrash()
	pump(t, driver)
	click(t, driver, "全选")
	pump(t, driver)
	click(t, driver, "彻底删除")
	pump(t, driver)
	if !strings.Contains(model.ui.trashMsg, "无法恢复") {
		t.Fatalf("第一次点应只是上膛：%q", model.ui.trashMsg)
	}
	click(t, driver, "确认彻底删除")
	pump(t, driver)
	if len(model.ui.trashRows) != 0 {
		t.Fatalf("彻底删除后回收站应为空：%+v", model.ui.trashRows)
	}
	if _, err := os.Stat(filepath.Join(dir, ".trash", "健康检查.yml")); err == nil {
		t.Fatal("文件还留在 .trash 里")
	}
	if _, err := os.Stat(filepath.Join(dir, "健康检查.yml")); err == nil {
		t.Fatal("彻底删除的文件不该回到集合根")
	}
}

// TestExportDialogSelectsRange 导出范围：勾分组会带上子项，取消勾选的条目不出现在产物里。
func TestExportDialogSelectsRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 范围集合\n")
	writeFile(t, filepath.Join(dir, "健康检查.yml"), `info:
  name: 健康检查
  type: http
  seq: 1
meta:
  uid: range-health
http:
  method: GET
  url: http://127.0.0.1:1/health
  headers: []
  body:
    type: none
`)
	writeFile(t, filepath.Join(dir, "用户", "folder.yml"), "info:\n  name: 用户\n  type: folder\n  seq: 1\n")
	writeFile(t, filepath.Join(dir, "用户", "登录.yml"), `info:
  name: 登录
  type: http
  seq: 1
meta:
  uid: range-login
http:
  method: POST
  url: http://127.0.0.1:1/login
  headers: []
  body:
    type: none
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openExport("md")
	pump(t, driver)

	if len(model.ui.exportRows) != 3 { // 分组 + 两条请求
		t.Fatalf("应列出三条（1 分组 + 2 请求）：%+v", model.ui.exportRows)
	}
	if requests, _ := model.exportCounts(); requests != 2 {
		t.Fatalf("默认应全选两条：%d", requests)
	}

	// 点掉分组「用户」：它下面的「登录」跟着取消。
	groupIndex := -1
	for index, row := range model.ui.exportRows {
		if row.Group {
			groupIndex = index
		}
	}
	if groupIndex < 0 {
		t.Fatalf("没找到分组行：%+v", model.ui.exportRows)
	}
	// 树里也有「用户」，只点弹窗里那一个（弹窗在右侧）。
	clickRightOf(t, driver, "用户", 150)
	pump(t, driver)
	if requests, _ := model.exportCounts(); requests != 1 {
		t.Fatalf("取消分组后应只剩根目录那条：%d（分组行 Selected=%v）",
			requests, model.ui.exportRows[groupIndex].Selected)
	}
	content, ext, _ := model.exportContentFor("md", model.exportSelection())
	if ext != "md" || !strings.Contains(content, "GET 健康检查") {
		t.Fatalf("产物应包含仍在勾选里的请求：\n%s", content)
	}
	if strings.Contains(content, "POST 登录") {
		t.Fatalf("取消勾选的请求不该出现：\n%s", content)
	}

	// 再勾回来：分组联动应当恢复两条。
	clickRightOf(t, driver, "用户", 150)
	pump(t, driver)
	if requests, _ := model.exportCounts(); requests != 2 {
		t.Fatalf("重新勾分组应带回子项：%d", requests)
	}
	html, _, _ := model.exportContentFor("html", model.exportSelection())
	if !strings.Contains(html, "POST 登录") {
		t.Fatal("HTML 也要跟着范围走")
	}
}

// TestExportDialogRejectsEmptySelection 一条都不勾时给提示，不写文件。
func TestExportDialogRejectsEmptySelection(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openExport("html")
	model.selectAllExports(false)
	pump(t, driver)
	model.saveExport()
	if model.ui.exportNote != "至少勾一条再导出" {
		t.Fatalf("应提示至少勾一条，实际 %q", model.ui.exportNote)
	}
	if !model.ui.exportOpen {
		t.Fatal("拒绝导出时弹窗不该关掉")
	}
}

// TestCookieJarPanel Cookie Jar：发过一次带 Set-Cookie 的请求后，
// 面板能列出 cookie、后面的请求自动带上、清空后不再带。
func TestCookieJarPanel(t *testing.T) {
	send.ClearCookies()
	t.Cleanup(send.ClearCookies)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s-9", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
			_, _ = fmt.Fprint(w, `{"ok":true}`)
		default:
			if cookie, err := r.Cookie("sid"); err == nil {
				_, _ = fmt.Fprintf(w, `{"sid":%q}`, cookie.Value)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 会话集合\n")
	for name, spec := range map[string][2]string{
		"登录": {"POST", "/login"},
		"我的": {"GET", "/me"},
	} {
		writeFile(t, filepath.Join(dir, name+".yml"), fmt.Sprintf(`info:
  name: %s
  type: http
  seq: 1
meta:
  uid: cookie-%s
http:
  method: %s
  url: %s
  headers: []
  body:
    type: none
`, name, name, spec[0], srv.URL+spec[1]))
	}
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 先打登录接口，种下 cookie。
	click(t, driver, "POST 登录")
	pump(t, driver)
	click(t, driver, "发送")
	for deadline := time.Now().Add(3 * time.Second); model.sending && time.Now().Before(deadline); {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}

	click(t, driver, "Cookie")
	pump(t, driver)
	if !shows(driver, "sid") || !shows(driver, "s-9") {
		t.Fatalf("面板没列出 cookie：%v", painted(driver))
	}
	if len(model.ui.cookieRows) != 2 {
		t.Fatalf("缓存里应有两条：%+v", model.ui.cookieRows)
	}

	// 单条删除：只掉 theme，sid 还在。
	click(t, driver, "删除 theme")
	pump(t, driver)
	if len(model.ui.cookieRows) != 1 || model.ui.cookieRows[0].Name != "sid" {
		t.Fatalf("删单条后应只剩 sid：%+v", model.ui.cookieRows)
	}
	if left := send.Cookies(); len(left) != 1 || left[0].Name != "sid" {
		t.Fatalf("jar 里也该只剩 sid：%+v", left)
	}

	// 清空后：面板空了，请求也不再带 cookie（服务端回 401）。
	click(t, driver, "清空")
	pump(t, driver)
	if len(model.ui.cookieRows) != 0 || len(send.Cookies()) != 0 {
		t.Fatalf("清空后不该还有 cookie：%+v", model.ui.cookieRows)
	}
	click(t, driver, "关闭")
	pump(t, driver)
	click(t, driver, "GET 我的")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "401")
}

// TestRunnerRulesAndVariables 规则编辑 → 落盘 → 运行：第一条提取 token，第二条带上它。
func TestRunnerRulesAndVariables(t *testing.T) {
	const token = "tok-7"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"token":%q}}`, token)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 规则集合\n")
	writeFile(t, filepath.Join(dir, "登录.yml"), fmt.Sprintf(`info:
  name: 登录
  type: http
  seq: 1
meta:
  uid: rule-login
http:
  method: POST
  url: %q
  headers: []
  body:
    type: none
`, srv.URL+"/login"))
	writeFile(t, filepath.Join(dir, "我的信息.yml"), fmt.Sprintf(`info:
  name: 我的信息
  type: http
  seq: 2
meta:
  uid: rule-me
http:
  method: GET
  url: %q
  headers:
    - name: Authorization
      value: "Bearer {{token}}"
      enabled: true
  body:
    type: none
`, srv.URL+"/me"))

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 给「登录」加一条提取规则（弹窗默认就是 json + $.data.token，只差变量名）。
	model.openRunRules("登录.yml")
	pump(t, driver)
	click(t, driver, "添加提取")
	pump(t, driver)
	if len(model.ui.ruleExtracts) != 1 {
		t.Fatalf("应当有一行提取：%+v", model.ui.ruleExtracts)
	}
	model.ui.ruleExtracts[0].Name = "token"
	pump(t, driver)
	click(t, driver, "保存")
	pump(t, driver)
	click(t, driver, "关闭") // 规则弹窗收起，下面才点得到运行器
	pump(t, driver)

	raw, err := os.ReadFile(filepath.Join(dir, "登录.yml"))
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if !strings.Contains(string(raw), "run:") || !strings.Contains(string(raw), "token") {
		t.Fatalf("规则没落盘（应写进 run: 键）：\n%s", raw)
	}

	// 跑整个集合：第二条能拿到第一条提取的 token。
	model.openRunner()
	pump(t, driver)
	click(t, driver, "运行")
	deadline := time.Now().Add(5 * time.Second)
	for model.run.Running && time.Now().Before(deadline) {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}
	if len(model.run.Results) != 2 {
		t.Fatalf("应跑完两条：%+v", model.run.Results)
	}
	if model.run.Results[0].Extracted["token"] != token {
		t.Fatalf("第一条没提取到 token：%+v", model.run.Results[0])
	}
	if model.run.Results[1].Status != 200 {
		t.Fatalf("第二条应带上 token 成功，实际 %d：%+v", model.run.Results[1].Status, model.run.Results[1])
	}
	if !strings.Contains(model.run.Note, "全部通过") {
		t.Fatalf("小结应显示全部通过：%q", model.run.Note)
	}
}

// TestSavingRequestKeepsRunRules 在请求页保存这条请求时，run: 规则不能被冲掉。
func TestSavingRequestKeepsRunRules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 规则集合\n")
	writeFile(t, filepath.Join(dir, "登录.yml"), `info:
  name: 登录
  type: http
  seq: 1
meta:
  uid: rule-keep
http:
  method: POST
  url: http://127.0.0.1:1/login
  headers: []
  body:
    type: none
run:
  extract:
    - name: token
      from: json
      path: $.data.token
  assert:
    - kind: status
      op: equals
      want: "200"
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "POST 登录")
	pump(t, driver)

	// 改点别的东西再保存——保存是从草稿重建文件的，最容易把 run: 弄丢。
	model.req.url = "http://127.0.0.1:1/login?v=2"
	model.saveRequest()
	pump(t, driver)

	raw, err := os.ReadFile(filepath.Join(dir, "登录.yml"))
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if !strings.Contains(string(raw), "run:") || !strings.Contains(string(raw), "$.data.token") {
		t.Fatalf("保存请求把运行规则冲掉了：\n%s", raw)
	}
	if !strings.Contains(string(raw), "v=2") {
		t.Fatalf("地址没保存成功：\n%s", raw)
	}
}

// TestRunnerShowsAssertionFailure 断言失败要在结果里给出「期望 vs 实际」，并计入失败条数。
func TestRunnerShowsAssertionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1}`))
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 给「健康检查」塞一条会失败的断言。
	for _, entry := range col.Entries {
		if entry.Name == "健康检查" && entry.Payload != nil {
			runner.Apply(entry.Payload, runner.Rules{
				Assert: []runner.Assert{{Kind: "json", Path: "$.code", Op: "equals", Want: "0"}},
			})
		}
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openRunner()
	pump(t, driver)
	click(t, driver, "运行")
	deadline := time.Now().Add(5 * time.Second)
	for model.run.Running && time.Now().Before(deadline) {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}
	var failed *runner.Result
	for index := range model.run.Results {
		if model.run.Results[index].Name == "健康检查" {
			failed = &model.run.Results[index]
		}
	}
	if failed == nil || !failed.Failed || len(failed.Failures) != 1 {
		t.Fatalf("应有断言失败：%+v", model.run.Results)
	}
	if !strings.Contains(failed.Failures[0], "期望 \"0\"，实际 \"1\"") {
		t.Fatalf("失败文案不对：%v", failed.Failures)
	}
	if !shows(driver, "✗") {
		t.Fatalf("结果里应看到失败标记：%v", painted(driver))
	}
}

// TestMockServerFollowsSettings 开了桩服务就按集合里的请求应答，关了就停。
func TestMockServerFollowsSettings(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.settings.MockEnabled = true
	model.settings.MockAddr = "127.0.0.1:0" // 让系统分配端口，避免和本机别的服务撞
	model.restartMock()
	defer model.stopMock()
	if !model.mock.running || model.mock.addr == "" {
		t.Fatalf("桩服务没起来：%+v", model.mock)
	}
	if len(model.mock.routes) == 0 {
		t.Fatal("应把集合里的请求注册成路由")
	}

	resp, err := http.Get("http://" + model.mock.addr + "/__mock/routes")
	if err != nil {
		t.Fatalf("GET 路由表: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("路由表状态码 = %d", resp.StatusCode)
	}
	// 集合里的请求路径是 /healthz，桩应当认得。
	var routed bool
	for _, route := range model.mock.routes {
		if route.Path == "/healthz" {
			routed = true
		}
	}
	if !routed {
		t.Fatalf("没注册 /healthz：%+v", model.mock.routes)
	}
	answer, err := http.Get("http://" + model.mock.addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = answer.Body.Close() }()
	if answer.StatusCode != 200 || answer.Header.Get("X-Mock") != "api-doc-go" {
		t.Fatalf("桩没应答：%d %v", answer.StatusCode, answer.Header)
	}

	model.settings.MockEnabled = false
	model.restartMock()
	if model.mock.running {
		t.Fatal("关掉后桩服务应停止")
	}
}

// TestImportPostmanDialog 导入弹窗走完整条路：贴 JSON → 点导入 → 树里多出分组与请求。
func TestImportPostmanDialog(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openImport(0)
	pump(t, driver)

	model.ui.importText = `{"info":{"name":"外部"},"item":[
	  {"name":"用户","item":[{"name":"外部登录","request":{"method":"POST","url":"https://a/login","header":[]}}]}
	]}`
	click(t, driver, "导入")
	pump(t, driver)
	pump(t, driver)

	if model.ui.importErr != "" {
		t.Fatalf("导入失败：%s", model.ui.importErr)
	}
	if !strings.Contains(model.ui.importNote, "1 条请求") {
		t.Fatalf("结果提示不对：%q", model.ui.importNote)
	}
	// 重扫之后新条目应当出现在集合里，并且能按名字找到。
	var imported *explorer.Entry
	for _, entry := range model.col.Entries {
		if entry.Name == "外部登录" {
			imported = entry
		}
	}
	if imported == nil {
		var names []string
		for _, entry := range model.col.Entries {
			names = append(names, entry.Name)
		}
		t.Fatalf("导入的请求没进集合：%v", names)
	}
	if imported.Payload == nil || imported.Payload.HTTP.Method != "POST" {
		t.Fatalf("导入内容不对：%+v", imported.Payload)
	}
	if !strings.Contains(imported.File, "用户/") {
		t.Fatalf("应落在同名分组里：%s", imported.File)
	}
}

// TestParameterRowsMoveAndSave 参数表的行能上下移，保存后文件里的顺序也跟着换。
func TestParameterRowsMoveAndSave(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 排序集合\n")
	writeFile(t, filepath.Join(dir, "查询.yml"), `info:
  name: 查询
  type: http
  seq: 1
meta:
  uid: viva-row-move
http:
  method: GET
  url: http://127.0.0.1:1/list
  headers: []
  params:
    - name: a
      value: "1"
      enabled: true
    - name: b
      value: "2"
      enabled: true
  body:
    type: none
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 查询")
	pump(t, driver)

	if len(model.req.params) != 2 || model.req.params[0].name != "a" {
		t.Fatalf("参数没读进来：%+v", model.req.params)
	}
	// 第一行的「下移」。
	click(t, driver, "下移")
	pump(t, driver)
	if model.req.params[0].name != "b" || model.req.params[1].name != "a" {
		t.Fatalf("下移后顺序 = %q, %q", model.req.params[0].name, model.req.params[1].name)
	}
	// 第一行现在是 b，上移按钮该是灰的：点它不该把顺序再弄乱。
	model.saveRequest()
	pump(t, driver)
	raw, err := os.ReadFile(filepath.Join(dir, "查询.yml"))
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if strings.Index(string(raw), "name: b") > strings.Index(string(raw), "name: a") {
		t.Fatalf("保存后参数顺序没换：\n%s", raw)
	}
}

// TestCommandPaletteGroupsAndShortcuts 命令面板按「请求 / 集合 / 环境 / 视图」分组，
// 右侧提示只写真实存在的快捷键；那几个快捷键本身也真的能用。
func TestCommandPaletteGroupsAndShortcuts(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 页面级快捷键要有焦点在页面里才生效（headless 驱动没有「窗口」，target 为空时不算）。
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	if err := driver.Chord(gfx.ModifierControl, "k"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	for _, want := range []string{"命令 新建请求", "命令 设置"} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.Label == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("命令面板缺少「%s」：%v", want, painted(driver))
		}
	}
	if !shows(driver, primaryName()+" N") || !shows(driver, primaryName()+" ,") {
		t.Fatalf("右侧没写出快捷键：%v", painted(driver))
	}
	if !shows(driver, "视图") || !shows(driver, "集合") {
		t.Fatalf("缺少分组标题：%v", painted(driver))
	}

	// 关掉面板，然后用提示里写的那几个快捷键（每次先点回树，让焦点回到页面）。
	model.ui.paletteOpen = false
	pump(t, driver)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	if err := driver.Chord(gfx.ModifierControl, "n"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if !model.ui.newReqOpen {
		t.Fatal("Ctrl+N 应打开新建请求")
	}
	model.ui.newReqOpen = false
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	if err := driver.Chord(gfx.ModifierControl, ","); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if !model.ui.settingsOpen {
		t.Fatalf("Ctrl+, 应打开设置：%v", painted(driver))
	}
	model.ui.settingsOpen = false
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	tabs := len(model.tabs)
	if err := driver.Chord(gfx.ModifierControl, "w"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	if len(model.tabs) != tabs-1 {
		t.Fatalf("Ctrl+W 应关掉当前标签：%d → %d", tabs, len(model.tabs))
	}
}

// TestSettingsLabelsShareOneColumn 设置表单的标签定宽右对齐：宽度一致，右缘对齐。
func TestSettingsLabelsShareOneColumn(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "设置")
	pump(t, driver)
	click(t, driver, "设置分区 网络与安全")
	pump(t, driver)

	right := float32(-1)
	for _, want := range []string{"默认超时（秒）", "跟随重定向", "忽略证书校验"} {
		bounds := gfx.Rect{}
		for _, label := range driver.Snapshot().Labels {
			// 同名文本可能还有开关自带的标签：只认标签列里的那一个（在第一列）。
			if label.Value == want && label.Bounds.X < 600 && label.Bounds.Width <= float32(formLabelWidth) {
				bounds = label.Bounds
				break
			}
		}
		if bounds.Width == 0 {
			t.Fatalf("找不到标签「%s」：%v", want, painted(driver))
		}
		if right < 0 {
			right = bounds.X + bounds.Width
			continue
		}
		if got := bounds.X + bounds.Width; got != right {
			t.Fatalf("标签右缘没对齐：「%s」%.0f，前一行 %.0f", want, got, right)
		}
	}
}

// labelBounds 找一个画面文本的矩形（只按值匹配，取第一个）。
func labelBounds(driver *app.Driver, want string) (gfx.Rect, bool) {
	for _, label := range driver.Snapshot().Labels {
		if label.Value == want {
			return label.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// TestResponseTruncationNotice 超过 1 MB 的响应：正文里不再拼提示（会破坏 JSON），
// 改由状态行下方单独一行说明「只显示前多少、共多大」。
func TestResponseTruncationNotice(t *testing.T) {
	payload := strings.Repeat("x", 1200*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	if !model.resp.Truncated {
		t.Fatal("超过上限应标记为截断")
	}
	if !shows(driver, "响应超过上限") {
		t.Fatalf("缺少截断提示：%v", painted(driver))
	}
	for _, text := range painted(driver) {
		if strings.Contains(text, "已截断") {
			t.Fatalf("正文里不该再拼提示文字：%q", text)
		}
	}
	if model.resp.TotalSize != int64(len(payload)) {
		t.Fatalf("总长度 = %d，期望 %d", model.resp.TotalSize, len(payload))
	}
}

// TestResponseExamplesSaveSwitchDelete 响应示例：存一份当前响应 → 再发一次拿到新响应 →
// 用响应区右上角的下拉切回示例 → 切回实时 → 删掉示例。示例落在 examples/<请求>/ 下。
func TestResponseExamplesSaveSwitchDelete(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"n":%d}`, n)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)

	send := func(want string) {
		t.Helper()
		click(t, driver, "发送")
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			pump(t, driver)
			if model.hasResp && model.resp.Body == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("等不到响应 %s，当前 %q", want, model.resp.Body)
	}
	send(`{"n":1}`)

	// 保存也在响应区：右上角「保存响应」下拉。
	click(t, driver, "保存响应")
	pump(t, driver)
	click(t, driver, "保存响应示例")
	pump(t, driver)
	list := explorer.Examples(col.Dir, "健康检查.yml")
	if len(list) != 1 || list[0].Body != `{"n":1}` || list[0].Status != 200 {
		t.Fatalf("示例没落盘或内容不对：%+v", list)
	}
	if !strings.Contains(model.copyNote, "已存为示例") {
		t.Fatalf("应提示已保存，实际 %q", model.copyNote)
	}

	// 再发一次：实时响应换成 n=2，示例仍是 n=1。
	send(`{"n":2}`)

	// 右上角下拉：点开选示例。
	clickText(t, driver, "当前响应")
	pump(t, driver)
	shown := model.exampleLabel(list[0].File) // 下拉里显示的是截断后的名字
	if !shows(driver, shown) {
		t.Fatalf("下拉里没有示例项：%v", painted(driver))
	}
	clickText(t, driver, shown)
	pump(t, driver)
	if model.exSel != list[0].File {
		t.Fatalf("应选中示例 %q，实际 %q", list[0].File, model.exSel)
	}
	if model.resp.Body != `{"n":1}` {
		t.Fatalf("示例正文没铺上去：%q", model.resp.Body)
	}

	// 触发器现在显示示例名；再点它选回实时。
	clickText(t, driver, shown)
	pump(t, driver)
	clickText(t, driver, "当前响应")
	pump(t, driver)
	if model.exSel != "" || model.resp.Body != `{"n":2}` {
		t.Fatalf("切回实时失败：sel=%q body=%q", model.exSel, model.resp.Body)
	}

	// 删掉示例：先切回示例视图，再用「保存响应」下拉删。
	clickText(t, driver, "当前响应")
	pump(t, driver)
	clickText(t, driver, shown)
	pump(t, driver)
	click(t, driver, "保存响应")
	pump(t, driver)
	click(t, driver, "删除这份示例")
	pump(t, driver)
	if left := explorer.Examples(col.Dir, "健康检查.yml"); len(left) != 0 {
		t.Fatalf("删除后还有示例：%+v", left)
	}
	if model.exSel != "" || model.resp.Body != `{"n":2}` {
		t.Fatalf("删除后应回到实时响应：sel=%q body=%q", model.exSel, model.resp.Body)
	}
}

// TestFileResponseShowsCardAndSaves 文件类响应（PDF / 二进制 / 带 filename）不当文本渲染，
// 而是一张带文件名与保存按钮的卡片；保存出来的字节与响应体一致。
func TestFileResponseShowsCardAndSaves(t *testing.T) {
	payload := "%PDF-1.4\n%\xE2\xE3\xCF\xD3\n1 0 obj\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="报告.pdf"`)
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	for deadline := time.Now().Add(3 * time.Second); model.sending && time.Now().Before(deadline); {
		pump(t, driver)
		time.Sleep(10 * time.Millisecond)
	}
	if !model.resp.IsFile() {
		t.Fatalf("应判定为文件类响应：%+v", model.resp.ContentType)
	}
	if !shows(driver, "报告.pdf") {
		t.Fatalf("卡片里应有文件名：%v", painted(driver))
	}
	if !shows(driver, "保存为文件") {
		t.Fatalf("卡片里应有保存按钮：%v", painted(driver))
	}

	target := filepath.Join(t.TempDir(), "saved.pdf")
	if err := model.writeResponseBody(target); err != nil {
		t.Fatalf("writeResponseBody: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("保存的字节不对：%q", got)
	}
}

// TestResponseStatusSurvivesNarrowPane 左右分栏把响应拖窄时，状态码不能被挤掉。
// 曾经 Tabs 抢满整行，把 200 / 耗时 / 复制正文一起推出可视区；
// 现在状态在最左且不收缩，分段控件收成一个图标按钮，点它仍能切到原始视图。
func TestResponseStatusSurvivesNarrowPane(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"n":1}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")

	model.setSplitLayout(true)
	pump(t, driver)
	model.hsplit.Pos = 380
	pump(t, driver)

	if !shows(driver, "200") {
		t.Fatalf("窄栏下状态码不见了：%v", painted(driver))
	}
	click(t, driver, "原始")
	pump(t, driver)
	if !model.rawResp {
		t.Fatal("窄栏下点「原始」应切到原始报文")
	}
	if !shows(driver, `{"ok":true,"n":1}`) {
		t.Fatalf("窄栏下原始报文没显示：%v", painted(driver))
	}
}

// TestOpenRootsAreRemembered 多集合打开状态与活动根要写进偏好，关掉一个后同步更新。
func TestOpenRootsAreRemembered(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first := writeCollection(t, "http://example.invalid")
	second := writeCollection(t, "http://example.invalid")
	colA, err := explorer.Load(first)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	colB, err := explorer.Load(second)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	model := New(colA, "", nil)
	model.AddRoot(colB)
	model.ActivateCollection(colB.Dir)

	saved := settings.Load()
	if len(saved.LastCollections) != 2 || saved.LastCollections[1] != colB.Dir {
		t.Fatalf("打开的集合没记住：%+v", saved.LastCollections)
	}
	if saved.ActiveCollection != colB.Dir {
		t.Fatalf("活动根没记住：%q", saved.ActiveCollection)
	}

	model.closeRoot(1)
	saved = settings.Load()
	if len(saved.LastCollections) != 1 || saved.LastCollections[0] != colA.Dir {
		t.Fatalf("关闭后偏好没更新：%+v", saved.LastCollections)
	}
}

// TestCollectionDefaultsDialog 集合公共参数：弹窗里加一条默认头 → 存进清单 →
// 之后的请求自动带上；同名头以请求为准（这条在 send 包里测，这里测界面与落盘）。
func TestCollectionDefaultsDialog(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Team")
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	col, err := explorer.Load(writeCollection(t, srv.URL))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 概览页的「公共参数」入口。
	click(t, driver, "公共参数")
	pump(t, driver)
	if !model.ui.defaultsOpen {
		t.Fatalf("应打开公共参数弹窗：%v", painted(driver))
	}

	// 第一行填上 X-Team / core（占位符不是可点元素，按可见文字点进输入框）。
	clickText(t, driver, "X-Team")
	pump(t, driver)
	if err := driver.Type("X-Team"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	clickText(t, driver, "{{team}}")
	pump(t, driver)
	if err := driver.Type("core"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	click(t, driver, "保存")
	pump(t, driver)

	if model.ui.defaultsOpen {
		t.Fatal("保存后应关闭弹窗")
	}
	if len(model.defaults.Headers) != 1 || model.defaults.Headers[0].Name != "X-Team" {
		t.Fatalf("内存里的默认头不对：%+v", model.defaults.Headers)
	}
	raw, err := os.ReadFile(filepath.Join(col.Dir, "opencollection.yml"))
	if err != nil {
		t.Fatalf("读清单: %v", err)
	}
	if !strings.Contains(string(raw), "X-Team") {
		t.Fatalf("清单里没写入默认头：\n%s", raw)
	}

	// 真发一次：服务端应收到这条默认头。
	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "200")
	if gotHeader != "core" {
		t.Fatalf("默认头没带上，服务端收到 %q", gotHeader)
	}
}
