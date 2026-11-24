package view

import (
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestUIScaleScalesLayoutAndFonts 缩放要同时改布局尺寸与字号，100% 时是恒等。
func TestUIScaleScalesLayoutAndFonts(t *testing.T) {
	t.Cleanup(func() { applyUIScale(100) })

	applyUIScale(100)
	if tabWidth != 132 || tabPitch != 134 || splitterThickness != 8 || selLineH != 18 {
		t.Fatalf("100%% 应保持设计稿尺寸：tab=%v pitch=%v splitter=%v line=%v",
			tabWidth, tabPitch, splitterThickness, selLineH)
	}
	if got := fs(11); got != 11 {
		t.Fatalf("100%% 字号应不变：%v", got)
	}

	applyUIScale(125)
	if tabWidth != 165 || splitterThickness != 10 || titlebarHeight != 55 {
		t.Fatalf("125%% 布局没放大：tab=%v splitter=%v titlebar=%v",
			tabWidth, splitterThickness, titlebarHeight)
	}
	if got := fs(11); got != 13.75 {
		t.Fatalf("125%% 字号应为 13.75：%v", got)
	}
	// 1–2px 的描边保持物理像素，不跟着放大。
	if px(1) != 1 {
		t.Fatalf("1px 描边不该被放大：%v", px(1))
	}
}

// TestSetUIScaleKeepsSplitPositions 换缩放要把三处分隔条按比例搬，别把右栏挤没。
func TestSetUIScaleKeepsSplitPositions(t *testing.T) {
	t.Cleanup(func() { applyUIScale(100) })

	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	t.Cleanup(func() { model.setUIScale(100) })
	applyUIScale(100)
	model.split.Pos, model.rsplit.Pos, model.hsplit.Pos = 260, 260, 460
	baseFont := model.theme.Metrics.FontSizeM

	model.setUIScale(125)
	if model.settings.UIScale != 125 || model.ui.scalePick != 125 {
		t.Fatalf("偏好没记住缩放：settings=%d pick=%d", model.settings.UIScale, model.ui.scalePick)
	}
	if model.split.Pos != 325 || model.rsplit.Pos != 325 || model.hsplit.Pos != 575 {
		t.Fatalf("分隔条没按比例搬：%v / %v / %v",
			model.split.Pos, model.rsplit.Pos, model.hsplit.Pos)
	}
	if want := baseFont * 1.25; model.theme.Metrics.FontSizeM != want {
		t.Fatalf("主题字号没缩放：%v，应为 %v", model.theme.Metrics.FontSizeM, want)
	}
	if model.tree.RowHeight != 38 {
		t.Fatalf("树行高没缩放：%v", model.tree.RowHeight)
	}
}

// TestScaleIndexSnaps 未知百分比要落到最近的一档，别让按钮显示成 87%。
func TestScaleIndexSnaps(t *testing.T) {
	for pct, want := range map[int]int{80: 0, 87: 1, 100: 2, 120: 4, 400: 4} {
		if got := scaleIndex(pct); got != want {
			t.Fatalf("%d%% 应落到第 %d 档，实际 %d", pct, want, got)
		}
	}
}
