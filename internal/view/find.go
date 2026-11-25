package view

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

// 响应正文里查找（Ctrl/Cmd+F）：命中全部高亮，当前命中更深一档，↑/↓ 跳转并滚过去。
//
// 为什么要自己算行：正文是画在 canvas 上的文本（见 jsonselect.go），没有浏览器的
// 查找能力；好在我们手里已经有「可见行」列表（折叠后的 JSON 行 / 原始报文的切行），
// 逐行交给 text/find 匹配就够了，命中位置还能直接换算成像素，用来画底色与滚动。

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

	// 匹配模式开关：默认与浏览器查找一致——纯文本、不分大小写、不要求整词。
	caseSensitive bool
	regexp        bool
	wholeWord     bool

	// bad 表示上一次匹配没编译过（正则写了一半），界面据此提示而不是显示 0/0。
	bad bool

	hits  []matchHit
	index int

	// key 是命中缓存的键：查询词 + 匹配模式 + 行数 + 视图；变了才重扫。
	key string
	// jump 待执行的滚动目标（行号），-1 表示不需要滚动。
	jump int
}

// options 把三个开关翻成 text/find 的选项。
func (f *findState) options() find.Options {
	return find.Options{CaseSensitive: f.caseSensitive, WholeWord: f.wholeWord, Regexp: f.regexp}
}

// mode 是三个开关的位掩码，用来拼命中缓存的键。
func (f *findState) mode() int {
	mode := 0
	if f.caseSensitive {
		mode |= 1
	}
	if f.regexp {
		mode |= 2
	}
	if f.wholeWord {
		mode |= 4
	}
	return mode
}

// findMatches 在一组可见行里按 options 找命中，同一行可以有多处。
//
// 纯空白查询照旧当作没查（免得一个空格把整篇都点亮）；正则编译失败返回 error，
// 由查找条显示「正则无效」。text/find 报的是字节区间，这里换算成绘制用的 rune 列。
func findMatches(lines []string, pattern string, options find.Options) ([]matchHit, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, nil
	}
	query, err := find.Compile(pattern, options)
	if err != nil {
		return nil, err
	}
	out := make([]matchHit, 0, 8)
	var spans []find.Match
	for index, line := range lines {
		// 逐行匹配：正文是折行后的可见行，跨行的命中不在查找条的范围里。
		// spans 跨行复用容量，避免每行都为新命中分配一次。
		spans, _ = query.Matches(spans[:0], line, 0)
		for _, span := range spans {
			out = append(out, matchHit{
				line:  index,
				start: utf8.RuneCountInString(line[:span.Start]),
				end:   utf8.RuneCountInString(line[:span.End]),
			})
		}
	}
	return out, nil
}

// refreshFind 在行内容或查询词变化时重算命中。
func (m *Model) refreshFind(lines []string, view string) {
	if !m.find.open {
		return // 关着就不算也不算命中，免得正文每帧多扫一遍
	}
	key := view + "\x00" + m.find.query + "\x00" + strconv.Itoa(len(lines)) + "\x00" +
		strconv.Itoa(len(m.resp.Body)) + "\x00" + strconv.Itoa(m.find.mode())
	if key == m.find.key {
		return
	}
	m.find.key = key
	hits, err := findMatches(lines, m.find.query, m.find.options())
	m.find.bad = err != nil
	m.find.hits = hits
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
	m.find.bad = false
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
		m.findToggles(c)
		// 输入框与开关刚改动过状态，这里再刷一次：否则计数用的是上一帧的命中。
		m.refreshFind(m.jsonSel.vis, view)
		count := "0/0"
		switch {
		case m.find.bad:
			count = "正则无效"
		case len(m.find.hits) > 0:
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

// findToggles 是查找条上的三个匹配开关：Aa 区分大小写、.* 正则、ab 整词。
func (m *Model) findToggles(c ui.C) {
	m.findToggle(c, "Aa", "区分大小写", &m.find.caseSensitive)
	m.findToggle(c, ".*", "正则表达式", &m.find.regexp)
	m.findToggle(c, "ab", "整词匹配", &m.find.wholeWord)
}

// findToggle 画一个开关：点亮时描边，熄灭时只有文字。
// 改模式要清掉命中缓存，否则计数会停在旧模式的命中数上。
func (m *Model) findToggle(c ui.C, label, tooltip string, on *bool) {
	// 同一个开关只能声明一次：立即模式里 ui.Button 每调一次就多一个控件。
	button := ui.Button(c, label).XSmall().Tooltip(tooltip)
	if *on {
		button = button.Outline()
	} else {
		button = button.Ghost()
	}
	if !button.Clicked() {
		return
	}
	*on = !*on
	m.find.index = 0
	m.find.jump = -1
	m.find.key = ""
	m.invalidator.Invalidate()
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
