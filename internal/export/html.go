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

	body, toc := renderBody(docs, sel)
	b.WriteString("<div class=\"layout\">\n<nav class=\"toc\"><div class=\"toc-title\">目录</div>")
	b.WriteString("<input class=\"toc-search\" type=\"search\" placeholder=\"搜索…\" aria-label=\"搜索目录\">")
	b.WriteString("<div class=\"toc-list\">" + toc + "</div></nav>\n<main>\n")
	b.WriteString(body)
	b.WriteString("</main>\n</div>\n<script>" + tocScript + "</script>\n</body>\n</html>\n")
	return b.String()
}

// renderBody 同时生成正文与目录：锚点在一次遍历里编号，保证两边一一对应。
func renderBody(docs []Doc, sel Selection) (body, toc string) {
	var out, nav strings.Builder
	id := 0
	emit := func(level int, title string, navClass, style string) {
		tag := "h" + strconv.Itoa(clampLevel(level))
		out.WriteString("<" + tag + " id=\"" + tocID(id) + "\">" + html.EscapeString(title) + "</" + tag + ">\n")
		nav.WriteString("<a class=\"toc-item " + navClass + "\" style=\"" + style + "\" href=\"#" +
			tocID(id) + "\">" + html.EscapeString(title) + "</a>\n")
		id++
	}
	for _, doc := range docs {
		emit(1, doc.Name, "toc-root", "")
		for _, sec := range sections(doc.Col, sel) {
			if sec.Depth > 0 || sec.Title != doc.Col.Name {
				emit(min(sec.Depth+2, 5), sec.Title, "toc-folder", "padding-left:"+strconv.Itoa(8+sec.Depth*14)+"px")
			}
			for _, entry := range sec.Entries {
				// 标题的 id 就是 emit 分配的那个：先写标题与目录项，再写正文。
				level := min(sec.Depth+3, 6)
				tag := "h" + strconv.Itoa(clampLevel(level))
				out.WriteString("<" + tag + " id=\"" + tocID(id) + "\">" + html.EscapeString(entryTitle(entry)) + "</" + tag + ">\n")
				nav.WriteString("<a class=\"toc-item\" style=\"padding-left:" + strconv.Itoa(22+sec.Depth*14) +
					"px\" href=\"#" + tocID(id) + "\">" + html.EscapeString(entryTitle(entry)) + "</a>\n")
				id++
				writeEntryBody(&out, entry, 4)
			}
		}
	}
	return out.String(), nav.String()
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

// htmlStyle 是内联样式：浅色、可打印、代码块与表格可横向滚动。
const htmlStyle = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
body { margin: 0; color: #202124; background: #fff;
       font: 14px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Noto Sans SC", sans-serif; }
.layout { display: flex; gap: 24px; max-width: 92%; margin: 0 auto; padding: 28px 20px; align-items: flex-start; }
.toc { flex: 0 0 240px; position: sticky; top: 16px; max-height: calc(100vh - 32px); overflow: hidden;
       display: flex; flex-direction: column; border: 1px solid #e3e3e3; border-radius: 10px;
       background: #f5f6f8; padding: 12px 14px; font-size: 13px; }
.toc-title { font-weight: 600; margin-bottom: 8px; }
.toc-search { width: 100%; margin: 0 0 10px; padding: 6px 8px; border: 1px solid #e3e3e3;
              border-radius: 6px; font: inherit; background: #fff; outline: none; }
.toc-search:focus { border-color: #18a058; }
.toc-list { flex: 1 1 auto; min-height: 0; overflow: auto; }
.toc-item { display: block; padding: 3px 6px; color: #202124; text-decoration: none; border-radius: 6px; }
.toc-item:hover { background: #e8f5ee; }
.toc-root { font-weight: 600; }
.toc-folder { color: #444; }
main { flex: 1 1 auto; min-width: 0; }
h1 { font-size: 26px; margin: 8px 0 16px; }
h2 { font-size: 20px; margin: 28px 0 10px; padding-bottom: 6px; border-bottom: 1px solid #ececec; }
h3 { font-size: 16px; margin: 22px 0 8px; }
h4 { font-size: 13px; margin: 16px 0 6px; color: #5f6368; }
p { margin: 8px 0; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12.5px;
       background: #f5f5f5; border-radius: 4px; padding: 1px 4px; }
pre { background: #f7f7f8; border: 1px solid #ececec; border-radius: 8px; padding: 12px; overflow: auto; }
pre code { background: none; padding: 0; }
table { border-collapse: collapse; width: 100%; margin: 8px 0 12px; font-size: 13px; }
th, td { border: 1px solid #e3e3e3; padding: 6px 10px; text-align: left; vertical-align: top; }
th { background: #f5f6f8; font-weight: 600; }
blockquote { margin: 10px 0; padding: 4px 12px; border-left: 3px solid #18a058; color: #444; background: #fafafa; }
.summary { color: #444; }
.url code { font-size: 13px; }
.image-alt { color: #5f6368; font-style: italic; }
@media print {
  .toc, .toc-search { display: none; }
  .layout { display: block; max-width: none; padding: 0; }
  pre, table { break-inside: avoid; }
  h1, h2, h3 { break-after: avoid; }
}
`

// tocScript 是唯一的交互脚本：目录搜索过滤。
const tocScript = `
(function () {
  var list = document.querySelector('.toc-list');
  var search = document.querySelector('.toc-search');
  if (!list || !search) { return; }
  search.addEventListener('input', function () {
    var needle = search.value.trim().toLowerCase();
    var items = list.querySelectorAll('.toc-item');
    for (var i = 0; i < items.length; i++) {
      var text = items[i].textContent.toLowerCase();
      items[i].style.display = (!needle || text.indexOf(needle) >= 0) ? '' : 'none';
    }
  });
})();
`
