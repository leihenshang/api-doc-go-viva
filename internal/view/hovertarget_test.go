package view

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// TestVariableHotzoneKeepsFieldEditing 固定「变量热区」的机制：
//
// 输入框自己挂一个 Tooltip（整条框的兜底提示），变量所在的那一小段上再叠一条
// 「标签热区」——canvas.HitLabel 声明、canvas.HintSince 补提示。这样：
//
//   - 提示由框架自己画，锚点就是那一小段矩形，不需要自绘；
//   - 按下按标签语义交给输入框（a press on it is a press on the control），
//     光标定位、打字都不受影响。
//
// 两条任一失守，「悬停到某个 {{变量}}」就得回到整条框的提示（见 doc/任务清单.md）。
func TestVariableHotzoneKeepsFieldEditing(t *testing.T) {
	address := "{{host}}/api/users"
	var fieldID gfx.ID
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).Fill().Pad(20).Gap(10).Children(func() {
			id := ui.ID(c)
			fieldID = id
			ui.TextFieldID(c, id, &address, "地址", "{{host}}/api/users").FillWidth().
				Tooltip("整条：host = http://127.0.0.1:8080")
			// 变量那一小段：压在地址栏内部，指针停上去时由它接管提示。
			ui.Slot(c).Absolute().Left(30).Top(26).Size(120, 20).Draw(func(d *ui.DrawContext) {
				mark := d.Canvas.Mark()
				d.Canvas.HitLabel(fieldID, d.Rect, "", false)
				d.Canvas.HintSince(mark, fieldID, "host = http://127.0.0.1:8080")
			})
		})
	}})
	driver, err := app.NewDriver(ui.Configure(page, app.Config{Width: 480, Height: 240}), nil)
	if err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}

	// 热区：同 ID 的第二条命中区，带自己的提示文字。
	var hot gfx.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == fieldID && hit.Hint == "host = http://127.0.0.1:8080" {
			hot = hit.Bounds
		}
	}
	if hot.Width <= 0 {
		t.Fatalf("变量热区没有声明出来：%+v", driver.Snapshot().Hits)
	}

	// 1) 悬停：提示由框架画，内容取热区那一条。
	if err := driver.Move(hot.X+hot.Width/2, hot.Y+hot.Height/2); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := driver.Advance(600 * time.Millisecond); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if !shows(driver, "host = http://127.0.0.1:8080") {
		t.Fatalf("变量热区悬停没有弹出提示：%v", painted(driver))
	}

	// 2) 按下：标签语义把按下交给输入框，聚焦与打字都要正常。
	if err := driver.Click(hot.X+hot.Width/2, hot.Y+hot.Height/2); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	snap := driver.Snapshot()
	if snap.Focused != fieldID || snap.Editor == nil {
		t.Fatalf("按下没落到输入框：聚焦=%v 期望=%v 原生编辑=%v", snap.Focused, fieldID, snap.Editor != nil)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}
	if address != "{{host}}/api/usersX" {
		t.Fatalf("打字没进输入框：%q", address)
	}
}
