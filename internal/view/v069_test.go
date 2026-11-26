package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"errors"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	appmcp "github.com/leihenshang/api-doc-go-viva/internal/mcp"
	"github.com/vivaui/viva/gfx"
	"time"
)

// TestEmptyStateClearsSearch 树没有匹配项时用 v0.6.9 的 ui/empty，并在原地给出「清空搜索」。
func TestEmptyStateClearsSearch(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	if !shows(driver, "健康检查") {
		t.Fatalf("初始应列出请求：%v", painted(driver))
	}
	clickText(t, driver, "搜索请求…")
	pump(t, driver)
	if err := driver.Type("zzz"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if !shows(driver, "没有匹配的请求") || !shows(driver, "换个关键字") {
		t.Fatalf("应显示空状态：%v", painted(driver))
	}
	if !hasControl(driver, "清空搜索") {
		t.Fatalf("空状态应带「清空搜索」按钮：%v", painted(driver))
	}

	click(t, driver, "清空搜索")
	pump(t, driver)
	if model.filter != "" {
		t.Fatalf("清空搜索后 filter 应为空：%q", model.filter)
	}
	if !shows(driver, "健康检查") {
		t.Fatalf("清空后请求应回来：%v", painted(driver))
	}
}

// TestNewRequestValidatesName 新建请求的名称用 v0.6.9 的 validate 校验：
// 错误画在字段下方（FormField），空名不落盘，输入后错误消失。
func TestNewRequestValidatesName(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "新建请求")
	pump(t, driver)
	click(t, driver, "创建")
	pump(t, driver)
	if !shows(driver, "请求名不能为空") {
		t.Fatalf("空名应在字段下方报错：%v", painted(driver))
	}
	if !model.ui.newReqOpen {
		t.Fatal("校验没过时弹窗不该关")
	}
	if _, err := os.Stat(filepath.Join(col.Dir, ".yml")); err == nil {
		t.Fatal("空名不该写出文件")
	}

	clickText(t, driver, "例如：获取用户列表")
	pump(t, driver)
	if err := driver.Type("校验通过"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	pump(t, driver)
	if shows(driver, "请求名不能为空") {
		t.Fatalf("输入后错误应消失：%v", painted(driver))
	}

	click(t, driver, "创建")
	pump(t, driver)
	pump(t, driver)
	if _, err := os.Stat(filepath.Join(col.Dir, "校验通过.yml")); err != nil {
		t.Fatalf("校验通过后应落盘：%v", err)
	}
}

// TestSettingsLocalDescriptions 设置「本地数据」的三条信息用 ui/descriptions 呈现。
func TestSettingsLocalDescriptions(t *testing.T) {
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "设置")
	pump(t, driver)
	click(t, driver, "设置分区 本地数据")
	pump(t, driver)
	for _, want := range []string{"集合目录", "偏好文件", "当前环境"} {
		if !shows(driver, want) {
			t.Fatalf("本地数据缺 %q：%v", want, painted(driver))
		}
	}
}

// TestFailTextShownAndCopyable 传输失败的原因用 typography 显示：标签在、原因在、能复制。
func TestFailTextShownAndCopyable(t *testing.T) {
	// 127.0.0.1:1 必然连不上，制造传输错误（不是 4xx/5xx）。
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	click(t, driver, "GET 健康检查")
	pump(t, driver)
	click(t, driver, "发送")
	waitFor(t, driver, "请求失败")

	if model.failText == "" {
		t.Fatal("应记录失败原因")
	}
	if !shows(driver, "请求失败") {
		t.Fatalf("失败标题应显示：%v", painted(driver))
	}
	// 原因整段在 Texts 里（typography 逐字绘制，用 Texts 而不是 Labels 判断）。
	found := false
	for _, text := range driver.Snapshot().Texts {
		if strings.Contains(text.Value, "connection refused") || strings.Contains(text.Value, "connect") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("失败原因应画出来：%v", painted(driver))
	}
}

// TestWindowStartsWithMaximize 启动配置读设置的「启动时最大化」，并在设置面板里能关掉。
func TestWindowStartsWithMaximize(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	if !model.windowConfig().Maximized {
		t.Fatal("默认应最大化打开窗口")
	}
	if !model.ui.startMaximized {
		t.Fatal("设置面板的开关默认应为开")
	}

	// 关掉开关：落盘字段反过来，窗口配置跟着变。
	model.ui.startMaximized = false
	model.applySettingsEdits()
	if !model.settings.StartWindowed {
		t.Fatal("关掉后 StartWindowed 应为真")
	}
	if model.windowConfig().Maximized {
		t.Fatal("关掉后窗口不该再最大化")
	}
}

// TestMCPSettingsLayout 按参考设计重排后的 MCP 面板：五个分组齐、权限开关同行、
// 令牌可编辑且带「生成 / 复制」，JSON 默认收起。
func TestMCPSettingsLayout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	model.OpenMCPSettings()
	pump(t, driver)
	for _, want := range []string{
		"服务配置", "工具权限", "安全与访问", "客户端接入",
		"启用服务", "监听地址", "权限选项", "只读模式", "写操作确认", "浏览器",
		"暴露工具", "访问令牌", "可访问的集合", "客户端配置（复制内容）", "浏览器来源",
	} {
		if !shows(driver, want) {
			t.Fatalf("MCP 面板缺 %q：%v", want, painted(driver))
		}
	}
	// 「可写」的说明收进集合栏标题旁的 ⓘ（hit 的标签就是说明文字）。
	if !hasControl(driver, "「可写」留给将来的写类工具；当前工具集是只读 + 发送请求。") {
		t.Fatalf("集合栏标题旁应有「可写」说明的 tips：%v", painted(driver))
	}

	// 令牌是输入框（可编辑）+ 生成 / 复制（需求 4）。
	if !hasControl(driver, "生成") || !hasControl(driver, "复制") {
		t.Fatalf("令牌行应有生成与复制按钮：%v", painted(driver))
	}
	// 客户端配置常显（不再折叠）：滚到下面就该看到 mcpServers 那段。
	if model.mcpConfigJSON() == "" {
		t.Fatal("客户端配置 JSON 不应为空")
	}
	if !shows(driver, "mcpServers") {
		if err := driver.Scroll(gfx.ScrollEvent{X: 590, Y: 380, DeltaY: 600}); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		pump(t, driver)
	}
	if !shows(driver, "mcpServers") {
		t.Fatalf("展开后应能看到配置 JSON：%v", painted(driver))
	}

	// 点「生成」换新令牌；点「复制」给出结果提示（无头里可能报剪贴板不可用）。
	before := model.ui.mcpToken
	click(t, driver, "生成")
	pump(t, driver)
	if model.ui.mcpToken == "" || model.ui.mcpToken == before {
		t.Fatalf("生成应换一个新令牌：before=%q after=%q", before, model.ui.mcpToken)
	}
	click(t, driver, "复制")
	pump(t, driver)
	if model.copyNote == "" {
		t.Fatal("复制应给出结果提示")
	}

	// 「最近调用」在内容末尾：窗口不高时要滚一下才看得到（滚动区就是为这个留的）。
	if !shows(driver, "最近调用") {
		if err := driver.Scroll(gfx.ScrollEvent{X: 590, Y: 380, DeltaY: 600}); err != nil {
			t.Fatalf("Scroll: %v", err)
		}
		pump(t, driver)
	}
	if !shows(driver, "最近调用") {
		t.Fatalf("滚动后应看到「最近调用」：%v", painted(driver))
	}
	if !shows(driver, "还没有调用记录。") {
		t.Fatalf("空状态应显示：%v", painted(driver))
	}
}

// TestMCPContentKeepsClearOfScrollbar 滚动区要给自己那条覆盖式滚动条留位置：
// 内容右端不许越过「弹窗右边 - 框内边距 - scrollbarGutter」，否则文字与右对齐
// 的按钮（复制配置 / 可写 / 复制）会被压在条子底下。
func TestMCPContentKeepsClearOfScrollbar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.OpenMCPSettings()
	pump(t, driver)

	// 弹窗 700 宽、在 1180 宽的窗口里居中；Modal 内边距按 24 算（与视觉一致）。
	const dialogRight, modalInset = float32(940), float32(24)
	limit := dialogRight - modalInset - scrollbarGutter
	snap := driver.Snapshot()
	for _, hit := range snap.Hits {
		// 只看滚动内容区（跳过 scrim、工具栏与底部操作栏），跳过整宽容器。
		if hit.Label == "" || hit.Bounds.X < 240 || hit.Bounds.Width > 700 ||
			hit.Bounds.Y < 100 || hit.Bounds.Y > 640 {
			continue
		}
		if right := hit.Bounds.X + hit.Bounds.Width; right > limit {
			t.Fatalf("%q 越过滚动条留白：right=%.1f 应 ≤ %.1f", hit.Label, right, limit)
		}
	}
}

// TestMCPConsentDialogShows 写操作确认框要真的弹出来：
// 之前 m.ui.mcpConsentOpen 从没被置 true，Modal 画不出来，写操作只能等 60 秒超时。
func TestMCPConsentDialogShows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	if shows(driver, "MCP 写操作确认") {
		t.Fatalf("没请求时不该有确认框：%v", painted(driver))
	}

	// 模拟服务端 goroutine 提问（界面线程每帧 take 到就画）。
	reply := make(chan bool, 1)
	if !model.mcpConsent.ask(mcpConsentRequest{tool: "create_request", args: "{name:x}", reply: reply}) {
		t.Fatal("队列空时 ask 应该成功")
	}
	model.invalidator.Invalidate()
	pump(t, driver)
	if !shows(driver, "MCP 写操作确认") {
		t.Fatalf("有待确认时应弹出确认框：%v", painted(driver))
	}
	if !shows(driver, "create_request") {
		t.Fatalf("确认框里应写明要执行的工具：%v", painted(driver))
	}

	// 点「允许」：队列清空、请求方收到 true、弹窗收起。
	click(t, driver, "允许")
	pump(t, driver)
	select {
	case allow := <-reply:
		if !allow {
			t.Fatal("点允许后应回 true")
		}
	default:
		t.Fatal("点允许后请求方应收到答复")
	}
	if model.mcpConsent.take() != nil {
		t.Fatal("答过之后队列应为空")
	}
	pump(t, driver)
	if shows(driver, "MCP 写操作确认") {
		t.Fatalf("答过之后确认框应收起：%v", painted(driver))
	}
}

// TestMCPPanelRefreshesAuditOnOpen 打开面板就该看到最新的调用记录，
// 不需要先点「刷新」（AI 的调用都发生在面板关着的时候）。
func TestMCPPanelRefreshesAuditOnOpen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)

	// 预置两条审计（模拟 AI 在面板关着时调过工具）。
	audit := appmcp.NewAudit(mcpAuditPath())
	audit.Record("list_requests", false, map[string]any{"workspace": "ws_1"}, time.Now().Add(-2*time.Minute), nil)
	audit.Record("create_request", true, map[string]any{"name": "x"}, time.Now().Add(-time.Minute), errors.New("用户拒绝"))

	model.OpenMCPSettings()
	pump(t, driver)
	if len(model.ui.mcpAuditRows) != 2 {
		t.Fatalf("打开面板应载入 2 条审计，实际 %d", len(model.ui.mcpAuditRows))
	}
	// 记录在滚动区底部，滚下去看得见（顺序：最新在前）。
	if err := driver.Scroll(gfx.ScrollEvent{X: 590, Y: 380, DeltaY: 2000}); err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	pump(t, driver)
	if !shows(driver, "create_request") {
		t.Fatalf("面板应显示最新那条记录：%v", painted(driver))
	}
}
