package view

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// TestHoverGeometryAvailable 固定「自绘变量提示」依赖的两个框架行为：
//
//  1. 指针在输入框上移动时，app.Config.OnPointer 仍能收到坐标；
//  2. 输入框里的文字由应用画在 canvas 上，Texts() 里能读到它的原点与字号。
//
// 这两条任一失守，「悬停到某个 {{变量}}」就得换实现（见 doc/任务清单.md 的上游待办）。
// 输入框横向滚动后原点不跟着走，这一条不在本用例里断言：自绘方案对滚动过的输入框
// 一律退回整条框的提示。
func TestHoverGeometryAvailable(t *testing.T) {
	address := "{{host}}/api/users"
	page := ui.NewPage(ui.PageConfig{View: func(c ui.C) {
		ui.Column(c).Fill().Pad(20).Gap(10).Children(func() {
			ui.TextField(c, &address, "地址", "{{host}}/api/users").FillWidth()
		})
	}})
	moves := 0
	var pointerX, pointerY float32
	config := ui.Configure(page, app.Config{
		Width: 480, Height: 240,
		OnPointer: func(event gfx.PointerEvent) gfx.EventResult {
			if event.Kind == gfx.PointerMove {
				moves++
				pointerX, pointerY = event.X, event.Y
			}
			return gfx.EventResult{}
		},
	})
	driver, err := app.NewDriver(config, nil)
	if err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}

	snap := driver.Snapshot()
	var field gfx.Rect
	found := false
	for _, hit := range snap.Hits {
		if hit.Label == "地址" && !hit.IsLabel {
			field, found = hit.Bounds, true
			break
		}
	}
	if !found {
		t.Fatalf("没找到地址栏的命中区：%+v", snap.Hits)
	}
	x, y := field.X+field.Width/2, field.Y+field.Height/2
	if err := driver.Move(x, y); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moves == 0 {
		t.Fatal("输入框上的指针移动没有到达 OnPointer，自绘提示拿不到坐标")
	}
	if diff := pointerX - x; diff > 0.5 || diff < -0.5 {
		t.Fatalf("OnPointer 的横坐标 %0.f 与指针位置 %0.f 不一致", pointerX, x)
	}
	if diff := pointerY - y; diff > 0.5 || diff < -0.5 {
		t.Fatalf("OnPointer 的纵坐标 %0.f 与指针位置 %0.f 不一致", pointerY, y)
	}

	// 文字原点：值本身要作为 canvas 文本出现，且落在输入框里。
	painted := false
	for _, text := range driver.Snapshot().Texts {
		if text.Value != address {
			continue
		}
		if text.Bounds.X < field.X || text.Bounds.X > field.X+field.Width {
			t.Fatalf("文字原点 %.0f 不在输入框 %.0f..%.0f 内", text.Bounds.X, field.X, field.X+field.Width)
		}
		if text.Style.Size <= 0 {
			t.Fatalf("文字没有字号，算不出前缀宽度：%+v", text.Style)
		}
		painted = true
	}
	if !painted {
		t.Fatalf("输入框的值没有被应用画到 canvas 上，读不到文字原点：%+v", driver.Snapshot().Texts)
	}
}
