package view

import (
	"math"

	"github.com/vivaui/viva/gfx"
)

// 界面缩放：设置里选百分比，布局与字号都按它放大 / 缩小。
//
// 单窗口应用，进程内一份够用；px/fs 是唯一入口，所有写死的尺寸都经过它们，
// 这样 100% 时是恒等（与改动前逐像素一致），换比例时不用改各处调用。

// uiScale 是当前缩放系数（1 = 100%）。
var uiScale float32 = 1

// px 把设计稿上的像素按缩放取整：布局落在整像素上，描边与文字边缘才不糊。
// 1–2px 的描边由调用处排除，保持物理像素。
func px(v float32) float32 {
	if uiScale == 1 {
		return v
	}
	return float32(math.Round(float64(v * uiScale)))
}

// fs 是字号：允许小数，字形本身按亚像素排布。
func fs(v float32) float32 {
	if uiScale == 1 {
		return v
	}
	return v * uiScale
}

// scaleMetrics 把主题度量按缩放放大：viva 的按钮、输入框、菜单、树都按它排版。
func scaleMetrics(m gfx.Metrics) gfx.Metrics {
	m.ControlHeight = px(m.ControlHeight)
	m.RadiusControl = px(m.RadiusControl)
	m.RadiusSurface = px(m.RadiusSurface)
	m.FontSizeS = fs(m.FontSizeS)
	m.FontSizeM = fs(m.FontSizeM)
	m.FontSizeL = fs(m.FontSizeL)
	m.PadControlX = px(m.PadControlX)
	m.PadSurface = px(m.PadSurface)
	m.GapItem = px(m.GapItem)
	return m
}

// applyUIScale 设定缩放并重算所有会参与布局的尺寸。
//
// pct 是百分比，100 是原始大小；50–200 之外的取值按边界处理，避免设置文件被
// 手改成 500 之类把界面撑没。
func applyUIScale(pct int) {
	if pct < 50 {
		pct = 50
	}
	if pct > 200 {
		pct = 200
	}
	uiScale = float32(pct) / 100

	splitterThickness = px(8)
	detailPadding = px(20)
	panePadding = px(12)
	tabWidth = px(132)
	tabPitch = tabWidth + px(2)
	titlebarHeight = px(44)
	formLabelWidth = px(90)
	formRowWidth = px(360)
	settingsBodyWidth = px(556)

	selGutter = px(18)
	selPad = px(2)
	selLineH = px(18)
	selFont = fs(11)
}
