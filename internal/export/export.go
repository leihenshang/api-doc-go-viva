// Package export 把集合导出成一份能直接交付的文档：Markdown 或单文件 HTML。
//
// 导出的是「读得懂的那部分」——方法、地址、参数、请求头、请求体、响应字段与文档，
// 不含界面状态、不含历史与偏好。多个集合根合成一份文档（HTML 用一级标题分卷）。
package export

import (
	"fmt"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// Doc 是一个要导出的集合根。
type Doc struct {
	Name string
	Col  *explorer.Collection
}

// Selection 是要导出的条目：键是条目相对路径；nil 表示全选。
// 分组不单独表示——勾一个分组就是把它下面的条目都放进来，导出层只认条目。
type Selection map[string]bool

// Has 报告某条条目是否被选中。
func (sel Selection) Has(file string) bool { return sel == nil || sel[file] }

// Count 数一下这次会导出多少条（请求 / 文档分开数）。
func Count(docs []Doc, sel Selection) (requests, files int) {
	for _, doc := range docs {
		for _, entry := range doc.Col.Entries {
			if !sel.Has(entry.File) {
				continue
			}
			if entry.Kind == explorer.KindDoc {
				files++
				continue
			}
			requests++
		}
	}
	return requests, files
}

// Row 是「导出范围」列表里的一行：分组行带 Group，条目行带 File。
// 顺序与文档里的顺序一致，界面上看到的树就是导出的顺序。
type Row struct {
	File  string // 条目相对路径；分组行是目录
	Title string
	Depth int
	Group bool
}

// Rows 列出可勾选的条目（按集合分组顺序，分组行在它的条目之前）。
func Rows(docs []Doc) []Row {
	out := make([]Row, 0, 16)
	for _, doc := range docs {
		for _, sec := range sections(doc.Col, nil) {
			isRoot := sec.Depth == 0 && sec.Title == doc.Col.Name
			if !isRoot {
				out = append(out, Row{File: sec.Title, Title: sec.Title, Depth: sec.Depth, Group: true})
			}
			for _, entry := range sec.Entries {
				out = append(out, Row{
					File: entry.File, Title: entryTitle(entry), Depth: sec.Depth + 1,
				})
			}
		}
	}
	return out
}

// section 是文档里的一个分组（集合根也算一节，Depth 为 0）。
type section struct {
	Depth   int
	Title   string
	Entries []*explorer.Entry
}

// sections 按「根目录条目 → 各分组」整理成扁平顺序：分组按相对路径排，
// 条目挂在各自目录下，和树里的顺序一致，导出的文档读起来也是同一棵树。
func sections(col *explorer.Collection, sel Selection) []section {
	byDir := make(map[string][]*explorer.Entry, len(col.Folders)+1)
	for _, entry := range col.Entries {
		if !sel.Has(entry.File) {
			continue
		}
		dir := ""
		if slash := strings.LastIndex(entry.File, "/"); slash >= 0 {
			dir = entry.File[:slash]
		}
		byDir[dir] = append(byDir[dir], entry)
	}
	out := make([]section, 0, len(col.Folders)+1)
	if entries := byDir[""]; len(entries) > 0 {
		out = append(out, section{Depth: 0, Title: col.Name, Entries: entries})
	}
	folders := append([]*explorer.Folder(nil), col.Folders...)
	sort.Slice(folders, func(i, j int) bool { return folders[i].Dir < folders[j].Dir })
	for _, folder := range folders {
		entries := byDir[folder.Dir]
		if len(entries) == 0 {
			continue
		}
		depth := strings.Count(folder.Dir, "/") + 1
		out = append(out, section{Depth: depth, Title: folder.Name, Entries: entries})
	}
	return out
}

// Markdown 生成整份文档的 Markdown（全选）。
func Markdown(docs []Doc) string { return MarkdownSelected(docs, nil) }

// MarkdownSelected 只导出 sel 里的条目；返回的 Markdown 不留空的分组。
func MarkdownSelected(docs []Doc, sel Selection) string {
	var b strings.Builder
	wrote := false
	for _, doc := range docs {
		// 一条都没勾就整卷跳过；空集合本身（没有任何条目）仍然留标题。
		if len(sections(doc.Col, sel)) == 0 && len(doc.Col.Entries) > 0 {
			continue
		}
		if wrote {
			b.WriteString("\n---\n\n")
		}
		writeMarkdownDoc(&b, doc, sel)
		wrote = true
	}
	return b.String()
}

func writeMarkdownDoc(b *strings.Builder, doc Doc, sel Selection) {
	b.WriteString("# " + doc.Name + "\n\n")
	for _, sec := range sections(doc.Col, sel) {
		if len(doc.Col.Entries) > 0 && sec.Depth == 0 && sec.Title != doc.Col.Name {
			b.WriteString("## " + sec.Title + "\n\n")
		} else if sec.Depth > 0 {
			b.WriteString(strings.Repeat("#", min(sec.Depth+1, 5)) + " " + sec.Title + "\n\n")
		}
		for _, entry := range sec.Entries {
			writeMarkdownEntry(b, entry, min(sec.Depth+2, 6))
		}
	}
}

// writeMarkdownEntry 写一条条目：标题 + 概述 + 地址 + 参数 / 请求头 / 请求体 / 字段 / 文档。
func writeMarkdownEntry(b *strings.Builder, entry *explorer.Entry, level int) {
	heading := strings.Repeat("#", clampLevel(level))
	b.WriteString(heading + " " + strings.TrimSpace(entry.Method+" "+entry.Name) + "\n\n")
	payload := entry.Payload
	if entry.Kind == explorer.KindDoc {
		if text := strings.TrimSpace(entry.Content); text != "" {
			// 文档自己的 # 标题要压到条目标题之下，否则导出后结构被顶乱
			// （「### 接口约定」下面紧接着一个「# 约定」）。
			b.WriteString(demoteMarkdown(text, clampLevel(level)) + "\n\n")
		}
		return
	}
	if payload == nil {
		return
	}
	if payload.Summary != "" {
		b.WriteString(payload.Summary + "\n\n")
	}
	if payload.HTTP.URL != "" {
		b.WriteString("**地址**：`" + payload.HTTP.URL + "`\n\n")
	}
	writeMarkdownTable(b, "参数", []string{"名称", "值", "说明"}, kvRows(payload.HTTP.Params))
	writeMarkdownTable(b, "请求头", []string{"名称", "值", "说明"}, kvRows(payload.HTTP.Headers))
	switch payload.HTTP.Body.Type {
	case "", "none":
	case "form", "multipart":
		writeMarkdownTable(b, "表单字段", []string{"名称", "值", "类型", "说明"}, formRows(payload.HTTP.Body, payload.HTTP.Body.Type == "multipart"))
	default:
		if raw := strings.TrimSpace(payload.HTTP.Body.Raw); raw != "" {
			b.WriteString("**请求体**（" + payload.HTTP.Body.Type + "）\n\n")
			b.WriteString("```" + fenceLang(payload.HTTP.Body.Type) + "\n" + raw + "\n```\n\n")
		}
	}
	writeMarkdownTable(b, "请求体字段", []string{"字段名", "类型", "必填", "示例", "说明"}, fieldRows(payload.HTTP.Body.Fields))
	if auth := authLine(payload.HTTP.Auth); auth != "" {
		b.WriteString("**认证**：" + auth + "\n\n")
	}
	if docs := strings.TrimSpace(payload.Docs); docs != "" {
		b.WriteString("**文档**\n\n" + demoteMarkdown(docs, clampLevel(level)) + "\n\n")
	}
}

// demoteMarkdown 把 Markdown 里的标题整体加深 shift 级（上限 h6），
// 让文档内容永远落在承载它的标题之下。代码块里的 # 不动。
func demoteMarkdown(text string, shift int) string {
	if shift <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		hashes := 0
		for hashes < len(trimmed) && hashes < 6 && trimmed[hashes] == '#' {
			hashes++
		}
		if hashes == 0 || hashes >= len(trimmed) || trimmed[hashes] != ' ' {
			continue
		}
		level := hashes + shift
		if level > 6 {
			level = 6
		}
		lines[i] = strings.Repeat("#", level) + trimmed[hashes:]
	}
	return strings.Join(lines, "\n")
}

// writeMarkdownTable 写一张两列以上的表；没有行就整节省略。
func writeMarkdownTable(b *strings.Builder, title string, head []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	b.WriteString("**" + title + "**\n\n")
	b.WriteString("| " + strings.Join(head, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(head)) + "\n")
	for _, row := range rows {
		escaped := make([]string, len(row))
		for index, cell := range row {
			escaped[index] = escapeCell(cell)
		}
		b.WriteString("| " + strings.Join(escaped, " | ") + " |\n")
	}
	b.WriteString("\n")
}

// escapeCell 把竖线与换行转义成 Markdown 表格里合法的单格文本。
func escapeCell(text string) string {
	text = strings.ReplaceAll(text, "|", `\|`)
	text = strings.ReplaceAll(text, "\n", " ")
	if strings.TrimSpace(text) == "" {
		return "—"
	}
	return text
}

// kvRows 把参数 / 请求头转成表格行（只有启用项，和发送时一致）。
func kvRows(rows []collection.KV) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		out = append(out, []string{row.Name, row.Value, row.Description})
	}
	return out
}

// formRows 把 form / multipart 的逐行字段转成表格行。
func formRows(body collection.Body, multipart bool) [][]string {
	out := make([][]string, 0, len(body.Form))
	for _, row := range body.Form {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		typ := "text"
		if multipart && row.Type != "" {
			typ = row.Type
		}
		if !multipart {
			out = append(out, []string{row.Name, row.Value, row.Description})
			continue
		}
		out = append(out, []string{row.Name, row.Value, typ, row.Description})
	}
	return out
}

// fieldRows 把请求体字段表转成表格行。
func fieldRows(fields []collection.BodyField) [][]string {
	out := make([][]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Name) == "" {
			continue
		}
		required := ""
		if field.Required {
			required = "是"
		}
		typ := field.Type
		if typ == "" {
			typ = "string"
		}
		out = append(out, []string{field.Name, typ, required, field.Example, field.Description})
	}
	return out
}

// authLine 把认证写成人话：只说类型与关键字段，不把密码令牌打进文档。
func authLine(auth *collection.Auth) string {
	if auth == nil || auth.Type == "" || auth.Type == "none" {
		return ""
	}
	switch auth.Type {
	case "basic":
		if auth.Username != "" {
			return "Basic（用户 " + auth.Username + "）"
		}
		return "Basic"
	case "bearer":
		return "Bearer Token"
	case "apikey":
		where := "header"
		if auth.In != "" {
			where = auth.In
		}
		if auth.Key != "" {
			return "API Key（" + where + "：" + auth.Key + "）"
		}
		return "API Key（" + where + "）"
	}
	return auth.Type
}

// fenceLang 给代码块选语言标记。
func fenceLang(bodyType string) string {
	switch bodyType {
	case "json":
		return "json"
	case "xml":
		return "xml"
	}
	return ""
}

func clampLevel(level int) int {
	if level < 1 {
		return 1
	}
	if level > 6 {
		return 6
	}
	return level
}

// docTitle 是报告的标题：多个集合时用逗号连起来。
func docTitle(docs []Doc) string {
	names := make([]string, 0, len(docs))
	for _, doc := range docs {
		names = append(names, doc.Name)
	}
	return strings.Join(names, " / ")
}

// tocID 给目录项与正文标题生成一一对应的锚点。
func tocID(index int) string { return fmt.Sprintf("toc-%d", index) }
