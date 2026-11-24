package view

import (
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
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

// emptyBox 是组件目录的空状态：标题 + 说明居中，垂直居中需要给定高度。
func emptyBox(c ui.C, title, description string) ui.Box {
	return ui.Column(c).FillWidth().MinHeight(px(180)).Draw(func(d *ui.DrawContext) {
		components.Empty(d.Canvas, *d.Theme, d.Rect, components.EmptyData{
			Title:       title,
			Description: description,
		})
	})
}

// cardDraw 是卡片底的绘制回调（背景 + 边框 + 阴影），给 ui.Box.Draw 用。
func cardDraw(d *ui.DrawContext) {
	components.Card(d.Canvas, *d.Theme, d.Rect)
}

// table 是 ui.Table（可选中、可排序、行虚拟化）的薄封装；列与表头由调用方给。
func table(c ui.C, state *ui.TableState, source ui.TableSource, headers []string, columns []components.TableColumn) ui.Box {
	return ui.Table(c, state, ui.TableSpec{
		Columns: columns,
		Headers: headers,
		Source:  source,
		Striped: true,
	}).FillWidth()
}
