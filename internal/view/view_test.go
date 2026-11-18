package view

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
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

	// 页签是组件目录的 Tabs：点「响应」才该看到空状态，点「文档」才该看到概述。
	click(t, driver, "响应")
	pump(t, driver)
	if !shows(driver, "尚未发送") {
		t.Fatalf("响应页签应提示尚未发送：%v", painted(driver))
	}
	click(t, driver, "文档")
	pump(t, driver)
	if !shows(driver, "服务探活") {
		t.Fatalf("文档页签应显示概述：%v", painted(driver))
	}
	if !shows(driver, "渲染检查") {
		t.Fatalf("Docs 应按 Markdown 排版（标题未画出）：%v", painted(driver))
	}
	click(t, driver, "请求")
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
	if model.tab != tabRequest {
		t.Fatalf("新请求应停在请求页签，实际 %v", model.tab)
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
	if model.isLight() {
		t.Fatal("默认应是深色")
	}
	click(t, driver, "日间")
	pump(t, driver)
	if !model.isLight() {
		t.Fatal("点「日间」后应切到浅色")
	}
	if settings.Load().Theme != settings.ThemeLight {
		t.Fatalf("主题没有落盘：%+v", settings.Load())
	}
	click(t, driver, "夜间")
	pump(t, driver)
	if model.isLight() {
		t.Fatal("点「夜间」后应切回深色")
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
	if !shows(driver, "输入法") || !shows(driver, "外观") {
		t.Fatalf("设置面板缺少分组：%v", painted(driver))
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

	// 预览页签：标题应当被 markdown 排版（不带 # 号）。
	click(t, driver, "预览")
	pump(t, driver)
	if !shows(driver, "新标题") {
		t.Fatalf("预览没有画出标题：%v", painted(driver))
	}
	if shows(driver, "# 新标题") {
		t.Fatalf("预览把 # 号也画出来了（没走排版）：%v", painted(driver))
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
