package view

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 响应正文里查找（Ctrl/Cmd+F）：命中全部高亮，当前命中更深一档，↑/↓ 跳转并滚过去。
//
// 为什么要自己算行：正文是画在 canvas 上的文本（见 jsonselect.go），没有浏览器的
// 查找能力；好在我们手里已经有「可见行」列表（折叠后的 JSON 行 / 原始报文的切行），
// 直接按子串扫一遍就够了，命中位置还能直接换算成像素，用来画底色与滚动。

// matchHit 是一处命中：第几行、行内起止（rune 列）。
type matchHit struct {
	line  int
	start int
	end   int
}

// findState 是查找条的界面状态。
type findState struct {
	open  bool
	query string

	hits  []matchHit
	index int

	// key 是命中缓存的键：查询词 + 行数 + 视图；变了才重扫。
	key string
	// jump 待执行的滚动目标（行号），-1 表示不需要滚动。
	jump int
}

// findMatches 在一组可见行里找子串，忽略大小写；同一行可以有多处命中。
func findMatches(lines []string, query string) []matchHit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	needle := strings.ToLower(query)
	out := make([]matchHit, 0, 8)
	for index, line := range lines {
		haystack := strings.ToLower(line)
		from := 0
		for from < len(haystack) {
			at := strings.Index(haystack[from:], needle)
			if at < 0 {
				break
			}
			start := from + at
			out = append(out, matchHit{
				line:  index,
				start: len([]rune(line[:start])),
				end:   len([]rune(line[:start+len(needle)])),
			})
			from = start + len(needle)
		}
	}
	return out
}

// refreshFind 在行内容或查询词变化时重算命中。
func (m *Model) refreshFind(lines []string, view string) {
	if !m.find.open {
		return // 关着就不算也不算命中，免得正文每帧多扫一遍
	}
	key := view + "\x00" + m.find.query + "\x00" + strconv.Itoa(len(lines)) + "\x00" +
		strconv.Itoa(len(m.resp.Body))
	if key == m.find.key {
		return
	}
	m.find.key = key
	m.find.hits = findMatches(lines, m.find.query)
	if len(m.find.hits) == 0 {
		m.find.index = 0
		return
	}
	m.find.index = clampInt(m.find.index, 0, len(m.find.hits)-1)
}

// stepFind 跳到下一处 / 上一处，绕回两端；同时安排一次滚动。
func (m *Model) stepFind(delta int) {
	if len(m.find.hits) == 0 {
		return
	}
	m.find.index = (m.find.index + delta + len(m.find.hits)) % len(m.find.hits)
	m.find.jump = m.find.hits[m.find.index].line
	m.invalidator.Invalidate()
}

// openFind 打开查找条（Ctrl+F）。
func (m *Model) openFind() {
	m.find.open = true
	m.find.key = "" // 强制重算
	m.invalidator.Invalidate()
}

// closeFind 关掉查找条并清掉高亮。
func (m *Model) closeFind() {
	m.find.open = false
	m.find.hits = nil
	m.find.key = ""
	m.find.jump = -1
	m.invalidator.Invalidate()
}

// findBar 是响应区顶部的查找条：输入框 + 命中计数 + 上/下 + 关闭。
func (m *Model) findBar(c ui.C) {
	// 先按上一帧的可见行算一次：查找条比正文先声明，不这样的话
	// 计数会比输入慢一帧，看起来像「输入没生效」。
	view := "json"
	if m.rawResp {
		view = "raw"
	}
	m.refreshFind(m.jsonSel.vis, view)
	ui.Row(c).FillWidth().Gap(px(6)).Items(ui.Center).Children(func() {
		if ui.TextField(c, &m.find.query, "在响应里查找", "查找…").Grow(1).Changed() {
			m.find.index = 0
			m.find.jump = -1
		}
		// 输入框刚把查询词写进 m.find.query，这里再刷一次：否则计数用的是上一帧的命中。
		m.refreshFind(m.jsonSel.vis, view)
		count := "0/0"
		if len(m.find.hits) > 0 {
			count = strconv.Itoa(m.find.index+1) + "/" + strconv.Itoa(len(m.find.hits))
		}
		ui.Text(c, count).FontSize(fs(11)).Mono().Muted().Shrink(0)
		if ui.Button(c, "上一处").Icon(IconChevronUp).Ghost().IconOnly().
			Tooltip("上一处命中").Clicked() {
			c.After(func() { m.stepFind(-1) })
		}
		if ui.Button(c, "下一处").Icon(IconChevronDown).Ghost().IconOnly().
			Tooltip("下一处命中").Clicked() {
			c.After(func() { m.stepFind(1) })
		}
		if ui.Button(c, "关闭查找").Icon(IconX).Ghost().IconOnly().
			Tooltip("关闭查找（Esc）").Clicked() {
			c.After(m.closeFind)
		}
	})
}

// paintFind 画命中底色：所有命中用主色浅底，当前命中更实一档。
// 与选区底色画在同一层（文字之前），所以不会被文字盖住。
func (m *Model) paintFind(d *ui.DrawContext, sel *selView) {
	if len(m.find.hits) == 0 {
		return
	}
	for index, hit := range m.find.hits {
		if hit.line >= len(sel.vis) {
			continue
		}
		text := sel.vis[hit.line]
		x0 := sel.x + prefixWidth(d.Canvas, text, hit.start)
		x1 := sel.x + prefixWidth(d.Canvas, text, hit.end)
		if x1 <= x0 {
			x1 = x0 + sel.charW
		}
		tint := m.pal.findTint
		if index == m.find.index {
			tint = m.pal.findActive
		}
		top := d.Rect.Y + float32(hit.line)*selLineH
		d.Canvas.Rounded(gfx.Rect{
			X: x0, Y: top + 1, Width: x1 - x0, Height: selLineH - 2,
		}, 2, tint)
	}
}

// scrollToFind 在执行滚动：把当前命中的行放大约三分之一屏处。
// 传进来的是正文所在的滚动容器；容器没声明（比如响应头分段）就什么都不做。
func (m *Model) scrollToFind(scroll ui.Box) {
	if m.find.jump < 0 {
		return
	}
	line := m.find.jump
	m.find.jump = -1
	state := scroll.ScrollState()
	if state == nil {
		return
	}
	target := float32(line)*selLineH - state.Viewport/3
	state.ScrollTo(max(target, 0))
}
