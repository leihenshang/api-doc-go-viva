package view

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// writeVarCollection 写一条地址与请求头都带 {{变量}} 的请求。
func writeVarCollection(t *testing.T, requestURL, headerValue string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 变量集合\n")
	request := fmt.Sprintf(`info:
  name: 变量请求
  type: http
  seq: 1
http:
  method: GET
  url: %q
  headers:
    - name: X-Token
      value: %q
      enabled: true
  body:
    type: none
`, requestURL, headerValue)
	writeFile(t, filepath.Join(dir, "变量请求.yml"), request)
	return dir
}

// hover 把指针移到某个命中区中心，等过提示延迟（悬停提示不是立刻出现的）。
func hover(t *testing.T, driver *app.Driver, label string) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label != label || hit.IsLabel {
			continue
		}
		box := hit.Bounds
		if err := driver.Move(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
			t.Fatalf("移到 %q: %v", label, err)
		}
		if err := driver.Advance(600 * time.Millisecond); err != nil {
			t.Fatalf("Advance: %v", err)
		}
		return
	}
	t.Fatalf("画面上没有可悬停的 %q：%v", label, painted(driver))
}

// TestVarTipURL 指针停在地址栏的 {{host}} 上，提示里给出它在当前环境的取值。
func TestVarTipURL(t *testing.T) {
	dir := writeVarCollection(t, "{{host}}/healthz", "")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", map[string]string{"host": "http://127.0.0.1:8080"})
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	hoverFieldVar(t, driver, hitBoundsOf(t, driver, "地址"))
	if !shows(driver, "host = http://127.0.0.1:8080") {
		t.Fatalf("地址栏悬停应显示变量取值：%v", painted(driver))
	}
}

// TestVarTipUndefined 变量在当前环境里没有定义时，提示如实说「未定义」。
func TestVarTipUndefined(t *testing.T) {
	dir := writeVarCollection(t, "{{host}}/healthz", "")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	hoverFieldVar(t, driver, hitBoundsOf(t, driver, "地址"))
	if !shows(driver, "host = 未定义") {
		t.Fatalf("没定义的变量应提示未定义：%v", painted(driver))
	}
}

// TestVarTipHeaderRow 请求头的值也能悬停：切到请求头页签后停在 {{token}} 上。
func TestVarTipHeaderRow(t *testing.T) {
	dir := writeVarCollection(t, "http://127.0.0.1:8080/healthz", "{{token}}")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", map[string]string{"token": "s3cret"})
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	click(t, driver, "请求头")
	pump(t, driver)
	hoverFieldVar(t, driver, hitBoundsOf(t, driver, "值"))
	if !shows(driver, "token = s3cret") {
		t.Fatalf("请求头的值悬停应显示变量取值：%v", painted(driver))
	}
}

// TestVarTipStaysOutOfPlainText 没有 {{变量}} 的输入框不该弹提示。
func TestVarTipStaysOutOfPlainText(t *testing.T) {
	dir := writeVarCollection(t, "http://127.0.0.1:8080/healthz", "plain")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", map[string]string{"host": "http://127.0.0.1:8080"})
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	hover(t, driver, "地址")
	if shows(driver, "host = ") {
		t.Fatalf("地址里没有变量，不该有提示：%v", painted(driver))
	}
}

// tipRect 是画面上那条提示文字的矩形（提示由框架画成普通文字）。
func tipRect(driver *app.Driver, want string) (gfx.Rect, bool) {
	for _, label := range driver.Snapshot().Labels {
		if strings.Contains(label.Value, want) {
			return label.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// hoverFieldVar 把指针移到输入框里第一个变量的位置：文字从左边起算，
// 首个 {{变量}} 落在框左端的几十像素内。
func hoverFieldVar(t *testing.T, driver *app.Driver, box gfx.Rect) {
	t.Helper()
	if err := driver.Move(box.X+20, box.Y+box.Height/2); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Advance(600 * time.Millisecond); err != nil {
		t.Fatalf("Advance: %v", err)
	}
}

// TestVarTipFollowsText 提示只跟着变量走：停在 {{host}} 上时贴在左边，
// 停在同一条框的空白处一个提示都不弹。
func TestVarTipFollowsText(t *testing.T) {
	dir := writeVarCollection(t, "{{host}}/api/users", "")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", map[string]string{"host": "http://127.0.0.1:8080"})
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	field := hitBoundsOf(t, driver, "地址")
	middle := field.Y + field.Height/2

	// 1) 停在变量上：提示锚在文字那一段，明显偏左。
	if err := driver.Move(field.X+30, middle); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Advance(600 * time.Millisecond); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	onVar, ok := tipRect(driver, "host = ")
	if !ok {
		t.Fatalf("变量上没有提示：%v", painted(driver))
	}
	if center := onVar.X + onVar.Width/2; center > field.X+field.Width/3 {
		t.Fatalf("提示应贴在变量上（左三分之一），实际中心 %.0f，框 %.0f..%.0f",
			center, field.X, field.X+field.Width)
	}

	// 2) 停在同一条框的空白处：不该有任何提示。
	if err := driver.Move(field.X+field.Width-40, middle); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Advance(600 * time.Millisecond); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if blank, ok := tipRect(driver, "host = "); ok {
		t.Fatalf("空白处不该弹提示，实际画在 (%.0f,%.0f %.0fx%.0f)",
			blank.X, blank.Y, blank.Width, blank.Height)
	}
}

// hitBoundsOf 是某个可点击元素的命中矩形。
func hitBoundsOf(t *testing.T, driver *app.Driver, label string) gfx.Rect {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == label && !hit.IsLabel {
			return hit.Bounds
		}
	}
	t.Fatalf("没有 %q 的命中区：%v", label, painted(driver))
	return gfx.Rect{}
}

// TestVarHotzoneKeepsEditing 变量热区不吃按下：点在上面仍然是地址栏的点击，
// 光标能定位、也能接着打字。
func TestVarHotzoneKeepsEditing(t *testing.T) {
	dir := writeVarCollection(t, "{{host}}/api/users", "")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", map[string]string{"host": "http://127.0.0.1:8080"})
	driver := openWindow(t, model)
	model.openEntry(col.Entries[0])
	model.invalidator.Invalidate()
	pump(t, driver)

	field := hitBoundsOf(t, driver, "地址")
	before := model.req.url
	if err := driver.Click(field.X+30, field.Y+field.Height/2); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	if err := driver.Type("Z"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	if model.req.url == before {
		t.Fatalf("变量上按下后打字没进地址栏：%q", model.req.url)
	}
	if !strings.Contains(model.req.url, "Z") {
		t.Fatalf("地址里没有刚输入的字符：%q", model.req.url)
	}

	// 框里空白处被一条无声热区盖着（用来挡提示），按下同样要落进地址栏。
	if err := driver.Click(field.X+field.Width-40, field.Y+field.Height/2); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	if err := driver.Type("Y"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	if !strings.Contains(model.req.url, "Y") {
		t.Fatalf("空白处按下后打字没进地址栏：%q", model.req.url)
	}
}
