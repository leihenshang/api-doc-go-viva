package view

import (
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/empty"
)

// 本文件只做一件事：把 Viva 组件目录（components）接进声明式页面。
// 组件是 canvas 级 API（自己算尺寸、自己画），页面负责布局，所以统一走 ui.Draw 钩子：
// 位置由 ui 排版给出，外观交给组件，避免再手写一套颜色与圆角。

// badgeWidth 是方法徽标的预留宽度。BadgeExtent 需要 canvas，布局期拿不到，
// 这里取最宽标签（"gRPC"）的整数宽度；绘制时按实际文字宽度左对齐画。
const badgeWidth = 56

// methodBadge 画方法标识：替代原先「手写彩色 Text」的做法。
func methodBadge(c ui.C, entry *explorer.Entry) ui.Box {
	label := methodLabel(entry)
	variant := methodVariant(entry)
	return ui.Slot(c).Width(badgeWidth).Height(px(20)).Label(label).Draw(func(d *ui.DrawContext) {
		components.PaintBadge(d.Canvas, *d.Theme, d.Rect, label, variant)
	})
}

// emptyBox 是空状态：标题 + 说明居中。
//
// v0.6.9 起用页面级的 `ui/empty`（原来走 components.Empty 的 canvas 钩子）：
// 它按页面自己的排版组件搭，跟随主题与密度，还能在说明下面放「下一步」的按钮。
func emptyBox(c ui.C, title, description string) ui.Box {
	return emptyBoxActions(c, title, description, nil)
}

// emptyBoxActions 是带「下一步」动作的空状态（actions 为 nil 时就是普通空状态）。
func emptyBoxActions(c ui.C, title, description string, actions func()) ui.Box {
	spec := empty.Spec{Title: title, Description: description}
	if actions == nil {
		actions = func() {}
	}
	return empty.View(c, spec, actions).FillWidth().MinHeight(px(180))
}
