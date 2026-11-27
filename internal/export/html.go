package export

import (
	"html"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/markdown"
)

// HTML 生成单文件 HTML：内联样式、左侧目录树（可折叠 / 可搜索）、正文带锚点。
// 交付时一个文件拷走就能看，不需要服务器。
func HTML(docs []Doc) string { return HTMLSelected(docs, nil) }

// HTMLSelected 只导出 sel 里的条目；空集合（什么都没选）会得到一个只有标题的文档。
func HTMLSelected(docs []Doc, sel Selection) string {
	var b strings.Builder
	title := html.EscapeString(docTitle(docs))
	b.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + title + "</title>\n<style>" + htmlStyle + "</style>\n</head>\n<body>\n")
	body, entries := renderBody(docs, sel)
	b.WriteString("<div class=\"layout\">\n")
	b.WriteString(buildTOC(entries))
	b.WriteString("\n<main class=\"content\">\n" + body + "</main>\n")
	// 右侧大纲：内容由脚本按「当前这一节」生成；收起后正文铺满，右缘留展开把手。
	b.WriteString(`<nav class="ol" aria-label="当前页目录">`)
	b.WriteString(`<div class="ol-head"><div class="ol-title">目录</div>`)
	b.WriteString(`<button type="button" class="ol-toggle" aria-expanded="true" aria-label="收起目录" title="收起目录">›</button>`)
	b.WriteString(`</div><div class="ol-list"></div></nav>`)
	b.WriteString(`<button type="button" class="ol-open" aria-label="展开目录" title="展开目录">‹</button>`)
	b.WriteString("\n</div>\n<script>" + tocScript + "</script>\n</body>\n</html>\n")
	return b.String()
}

// tocEntry 是目录里的一项：正文与目录在同一次遍历里产出，锚点编号一一对应。
type tocEntry struct {
	id     string
	level  int    // 1 集合 / 2 分组（嵌套越深越大）/ 3 请求
	kind   string // root / folder / request
	text   string // 已转义
	method string
}

// buildTOC 把扁平条目拼成可折叠的 <ul> 嵌套树：层级用数组当栈，
// 遇到更浅的层级就把已打开的 </ul></li> 补上。
func buildTOC(entries []tocEntry) string {
	var b strings.Builder
	b.WriteString(`<nav class="toc" aria-label="目录"><div class="toc-title">目录</div>`)
	b.WriteString(`<input class="toc-search" type="search" placeholder="搜索目录…" aria-label="搜索目录">`)
	b.WriteString(`<ul class="toc-list">`)
	var open []int
	closeTo := func(level int) {
		for len(open) > 0 && open[len(open)-1] >= level {
			b.WriteString("</ul></li>")
			open = open[:len(open)-1]
		}
	}
	for _, e := range entries {
		switch e.kind {
		case "root":
			closeTo(1)
			b.WriteString(`<li class="toc-root"><a href="#` + e.id + `" title="` + e.text + `">` + e.text + `</a></li>`)
		case "folder":
			closeTo(e.level)
			b.WriteString(`<li class="toc-folder" aria-expanded="true" data-default="true">`)
			b.WriteString(`<button type="button" class="toc-toggle" aria-label="展开或收起"></button>`)
			b.WriteString(`<a href="#` + e.id + `">` + e.text + `</a><ul>`)
			open = append(open, e.level)
		case "request":
			b.WriteString(`<li class="toc-request">`)
			// 文档条目没有方法，不占徽标位（否则会留一个空胶囊）
			if e.method != "" {
				b.WriteString(`<span class="tm ` + methodClass(e.method) + `">` + e.method + `</span>`)
			}
			b.WriteString(`<a href="#` + e.id + `" title="` + e.text + `">` + e.text + `</a></li>`)
		}
	}
	closeTo(1)
	b.WriteString(`</ul></nav>`)
	return b.String()
}

// methodClass 把请求方法映射到徽标配色（沿用 Wails 版那套语义色）。
func methodClass(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "m-get"
	case "POST":
		return "m-post"
	case "PUT":
		return "m-put"
	case "DELETE":
		return "m-delete"
	case "PATCH":
		return "m-patch"
	case "GRPC":
		return "m-grpc"
	case "":
		return "m-doc"
	}
	return "m-other"
}

// renderBody 同时生成正文与目录条目：锚点在一次遍历里编号，保证两边一一对应。
func renderBody(docs []Doc, sel Selection) (body string, entries []tocEntry) {
	var out strings.Builder
	id := 0
	emitHeading := func(level int, text string) string {
		tag := "h" + strconv.Itoa(clampLevel(level))
		anchor := tocID(id)
		out.WriteString("<" + tag + " id=\"" + anchor + "\">" + html.EscapeString(text) + "</" + tag + ">\n")
		id++
		return anchor
	}
	for _, doc := range docs {
		rootAnchor := emitHeading(1, doc.Name)
		entries = append(entries, tocEntry{id: rootAnchor, level: 1, kind: "root", text: html.EscapeString(doc.Name)})
		for _, sec := range sections(doc.Col, sel) {
			level := 2
			if sec.Depth > 0 || sec.Title != doc.Col.Name {
				level = min(sec.Depth+2, 5)
				anchor := emitHeading(level, sec.Title)
				entries = append(entries, tocEntry{id: anchor, level: level, kind: "folder", text: html.EscapeString(sec.Title)})
			}
			for _, entry := range sec.Entries {
				// 标题 = 方法徽标 + 名称（与目录里一致，样式也同一套 .tm）。
				title := strings.TrimSpace(entry.Name)
				if entry.Kind == explorer.KindDoc {
					title = strings.TrimSpace("文档 " + entry.Name)
				}
				tag := "h" + strconv.Itoa(clampLevel(min(sec.Depth+3, 6)))
				anchor := tocID(id)
				out.WriteString("<" + tag + " id=\"" + anchor + "\">")
				if entry.Kind != explorer.KindDoc && entry.Method != "" {
					out.WriteString("<span class=\"tm " + methodClass(entry.Method) + "\">" + html.EscapeString(entry.Method) + "</span>")
				}
				out.WriteString(html.EscapeString(title) + "</" + tag + ">\n")
				entries = append(entries, tocEntry{
					id: anchor, level: level + 1, kind: "request",
					text: html.EscapeString(title), method: strings.ToUpper(entry.Method),
				})
				id++
				writeEntryBody(&out, entry, 4)
			}
		}
	}
	return out.String(), entries
}

// writeEntryBody 写一条请求的内容：概述、地址、参数 / 请求头 / 表单 / 请求体 / 字段 / 认证 / 文档。
func writeEntryBody(b *strings.Builder, entry *explorer.Entry, headingLevel int) {
	if entry.Kind == explorer.KindDoc {
		if text := strings.TrimSpace(entry.Content); text != "" {
			b.WriteString(renderMarkdown(text))
		}
		return
	}
	payload := entry.Payload
	if payload == nil {
		return
	}
	heading := "h" + strconv.Itoa(clampLevel(headingLevel))
	if payload.Summary != "" {
		b.WriteString("<p class=\"summary\">" + html.EscapeString(payload.Summary) + "</p>\n")
	}
	if payload.HTTP.URL != "" {
		b.WriteString("<p class=\"url\"><code>" + html.EscapeString(payload.HTTP.URL) + "</code></p>\n")
	}
	writeHTMLTable(b, "参数", []string{"名称", "值", "说明"}, kvRows(payload.HTTP.Params))
	writeHTMLTable(b, "请求头", []string{"名称", "值", "说明"}, kvRows(payload.HTTP.Headers))
	switch payload.HTTP.Body.Type {
	case "", "none":
	case "form", "multipart":
		writeHTMLTable(b, "表单字段", []string{"名称", "值", "类型", "说明"}, formRows(payload.HTTP.Body, payload.HTTP.Body.Type == "multipart"))
	default:
		if raw := strings.TrimSpace(payload.HTTP.Body.Raw); raw != "" {
			b.WriteString("<" + heading + ">请求体（" + html.EscapeString(payload.HTTP.Body.Type) + "）</" + heading + ">\n")
			b.WriteString("<pre><code>" + html.EscapeString(raw) + "</code></pre>\n")
		}
	}
	writeHTMLTable(b, "请求体字段", []string{"字段名", "类型", "必填", "示例", "说明"}, fieldRows(payload.HTTP.Body.Fields))
	if auth := authLine(payload.HTTP.Auth); auth != "" {
		b.WriteString("<p class=\"auth\"><strong>认证</strong>：" + html.EscapeString(auth) + "</p>\n")
	}
	if docs := strings.TrimSpace(payload.Docs); docs != "" {
		b.WriteString("<" + heading + ">文档</" + heading + ">\n" + renderMarkdown(docs))
	}
}

// writeHTMLTable 写一张表；没有行就整节省略。
func writeHTMLTable(b *strings.Builder, title string, head []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	b.WriteString("<h4>" + html.EscapeString(title) + "</h4>\n<table>\n<thead><tr>")
	for _, cell := range head {
		b.WriteString("<th>" + html.EscapeString(cell) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range rows {
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<td>" + html.EscapeString(cell) + "</td>")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n")
}

// entryTitle 是条目在标题与目录里的名字：「方法 名称」。
func entryTitle(entry *explorer.Entry) string {
	title := strings.TrimSpace(entry.Method + " " + entry.Name)
	if entry.Kind == explorer.KindDoc {
		title = strings.TrimSpace("文档 " + entry.Name)
	}
	return title
}

// renderMarkdown 把 Markdown 渲成 HTML：标题 / 段落 / 列表 / 代码块 / 表格 / 引用 / 分隔线。
// 用 viva 自己的解析器，所以文档在界面里和导出里是同一套语义。
func renderMarkdown(src string) string {
	var doc markdown.Document
	doc.Parse(src)
	var b strings.Builder
	openLists := make([]int, 0, 4) // 打开着的列表：正数 = ul 的层级+1，负数 = ol 的层级+1 取负
	closeLists := func(to int) {
		for len(openLists) > to {
			mark := openLists[len(openLists)-1]
			tag := "ul"
			if mark < 0 {
				tag = "ol"
			}
			b.WriteString("</li></" + tag + ">\n")
			openLists = openLists[:len(openLists)-1]
		}
	}
	for index := range doc.Blocks {
		block := &doc.Blocks[index]
		if block.Kind != markdown.Item {
			closeLists(0)
		}
		quoted := block.Quote > 0
		if quoted {
			b.WriteString("<blockquote>")
		}
		switch block.Kind {
		case markdown.Heading:
			tag := "h" + strconv.Itoa(clampLevel(block.Level+2))
			b.WriteString("<" + tag + ">" + renderSpans(doc.Spans(block)) + "</" + tag + ">\n")
		case markdown.Paragraph:
			b.WriteString("<p>" + renderSpans(doc.Spans(block)) + "</p>\n")
		case markdown.Code:
			attr := ""
			if info := strings.TrimSpace(block.Info); info != "" {
				attr = " class=\"language-" + html.EscapeString(info) + "\""
			}
			b.WriteString("<pre><code" + attr + ">" + html.EscapeString(block.Text) + "</code></pre>\n")
		case markdown.Rule:
			b.WriteString("<hr>\n")
		case markdown.Table:
			writeASTTable(&b, &doc, block)
		case markdown.Item:
			level := block.Level + 1
			for len(openLists) > level {
				closeLists(len(openLists) - 1)
			}
			if len(openLists) == level {
				b.WriteString("</li>\n")
			}
			if len(openLists) < level {
				tag, mark := "ul", level
				if block.Number > 0 {
					tag, mark = "ol", -level
				}
				b.WriteString("<" + tag + ">\n")
				openLists = append(openLists, mark)
			}
			b.WriteString("<li>" + renderSpans(doc.Spans(block)))
		}
		if quoted {
			b.WriteString("</blockquote>\n")
		}
	}
	closeLists(0)
	return b.String()
}

// writeASTTable 把 markdown 表格的 AST 写成 <table>。
func writeASTTable(b *strings.Builder, doc *markdown.Document, block *markdown.Block) {
	cells := doc.Cells(block)
	if len(cells) == 0 || block.Columns <= 0 {
		return
	}
	b.WriteString("<table>\n<thead><tr>")
	for index := 0; index < block.Columns && index < len(cells); index++ {
		cell := cells[index]
		b.WriteString("<th>" + renderSpans(doc.CellSpans(cell)) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for index := block.Columns; index < len(cells); index++ {
		if index%block.Columns == 0 {
			if index > block.Columns {
				b.WriteString("</tr>\n")
			}
			b.WriteString("<tr>")
		}
		cell := cells[index]
		b.WriteString("<td>" + renderSpans(doc.CellSpans(cell)) + "</td>")
	}
	b.WriteString("</tr>\n</tbody>\n</table>\n")
}

// renderSpans 把行内片段写成 HTML：转义所有文本，链接只留 http/https/mailto。
func renderSpans(spans []markdown.Span) string {
	var b strings.Builder
	for _, span := range spans {
		text := html.EscapeString(span.Text)
		if span.Text == markdown.Newline {
			b.WriteString("<br>")
			continue
		}
		if span.Style&markdown.Mono != 0 {
			text = "<code>" + text + "</code>"
		}
		if span.Style&markdown.Bold != 0 {
			text = "<strong>" + text + "</strong>"
		}
		if span.Style&markdown.Italic != 0 {
			text = "<em>" + text + "</em>"
		}
		if span.Style&markdown.Strike != 0 {
			text = "<del>" + text + "</del>"
		}
		if span.Link != "" {
			text = "<a href=\"" + html.EscapeString(span.Link) + "\" rel=\"noreferrer noopener\">" + text + "</a>"
		}
		if span.Image != "" {
			text = "<span class=\"image-alt\">" + text + "</span>"
		}
		b.WriteString(text)
	}
	return b.String()
}
