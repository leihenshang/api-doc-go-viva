package view

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 从响应体推字段表：把一次真实返回摊平成 `body.fields` 的行。
//
// 口径：
//   - 路径用点号连（user.name），数组统一记成 `list[]` 这一行，不下钻；
//   - 类型取 json 的实际类型（string / number / bool / object / array / null）；
//   - 示例只填标量，对象与数组留空（填一段 JSON 进单行输入框没有意义）；
//   - 最深层数默认 3，行数上限 200：再深的接口描述用字段表本来也说不清。
const (
	fieldDepth   = 3
	fieldMaxRows = 200
)

// errNotJSON 表示响应体不是 JSON 对象（或数组）。
var errNotJSON = errors.New("响应不是 JSON，没法推字段")

// inferFields 从 JSON 文本推字段表。
func inferFields(body string) ([]*fieldRow, error) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber() // 保留大整数的原文，别变成 1e+06
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, errNotJSON
	}
	// 只接受对象根与数组根：字段表描述的是「一条记录有哪些键」，标量根没有键可写。
	// 数组根按 Wails 版的口径展开首个对象元素，路径以 `[]` 开头（如 `[].id`）。
	rows := make([]*fieldRow, 0, 16)
	if _, ok := root.(map[string]any); !ok {
		list, ok := root.([]any)
		if !ok {
			return nil, errNotJSON
		}
		for _, item := range list {
			typed, isObject := item.(map[string]any)
			if !isObject {
				continue
			}
			walkFields("[]", typed, 1, &rows)
			break
		}
		if len(rows) == 0 {
			return nil, errNotJSON
		}
		return rows, nil
	}
	walkFields("", root, 1, &rows)
	if len(rows) == 0 {
		return nil, errNotJSON
	}
	return rows, nil
}

// walkFields 递归摊平：对象展开键，数组只记一行。
func walkFields(prefix string, value any, depth int, rows *[]*fieldRow) {
	if len(*rows) >= fieldMaxRows {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if depth > fieldDepth {
			return
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys) // 输出稳定，便于对比与 diff
		for _, key := range keys {
			name := key
			if prefix != "" {
				name = prefix + "." + key
			}
			walkFields(name, typed[key], depth+1, rows)
		}
	case []any:
		*rows = append(*rows, &fieldRow{name: prefix + "[]", typ: "array"})
	case string:
		*rows = append(*rows, &fieldRow{name: prefix, typ: "string", example: clipExample(typed)})
	case json.Number:
		*rows = append(*rows, &fieldRow{name: prefix, typ: "number", example: typed.String()})
	case bool:
		*rows = append(*rows, &fieldRow{name: prefix, typ: "bool", example: strconv.FormatBool(typed)})
	case nil:
		*rows = append(*rows, &fieldRow{name: prefix, typ: "null"})
	}
}

// clipExample 把过长的标量截短：字段表是描述，不是数据备份。
func clipExample(value string) string {
	const limit = 60
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// mergeFields 用新推出来的行覆盖字段表，但保留同名行里人写的东西：
// 说明、示例、必填标记都以已有的为准——自动生成不该抹掉注释。
func mergeFields(existing []*fieldRow, generated []*fieldRow) []*fieldRow {
	byName := make(map[string]*fieldRow, len(existing))
	for _, row := range existing {
		if row != nil && row.name != "" {
			byName[row.name] = row
		}
	}
	out := make([]*fieldRow, 0, len(generated))
	for _, row := range generated {
		if old, ok := byName[row.name]; ok {
			merged := *row
			if old.desc != "" {
				merged.desc = old.desc
			}
			if old.example != "" {
				merged.example = old.example
			}
			merged.required = old.required
			out = append(out, &merged)
			continue
		}
		out = append(out, row)
	}
	return out
}

// generateFields 是界面上「从当前响应生成」：把响应体摊成字段表。
func (m *Model) generateFields() {
	if !m.hasResp || strings.TrimSpace(m.resp.Body) == "" {
		m.copyNote = "先发一次请求：字段表从当前响应推出来"
		m.invalidator.Invalidate()
		return
	}
	// 用完整正文而不是界面上的截断文本：4 KB 之后还有字段的话，截断版会漏掉它们。
	rows, err := inferFields(m.resp.Body)
	if err != nil {
		m.copyNote = "当前响应不是 JSON 对象，没法推字段"
		m.invalidator.Invalidate()
		return
	}
	m.req.fields = mergeFields(m.req.fields, rows)
	m.markEdited()
	m.copyNote = fmt.Sprintf("已生成 %d 个字段（同名字段保留原有说明）", len(rows))
	m.invalidator.Invalidate()
}

// generateRespFields 是「响应字段」分段的生成：追加（overwrite=false）或按本次响应重建。
//
// 与请求体字段表的口径分开：respdoc 记录的是一条接口的返回结构，生成时同时留存响应快照
// （状态码 / 内容类型 / 稳定响应头），切回这条请求时能看出这张表是照着什么响应写的。
func (m *Model) generateRespFields(overwrite bool) {
	if !m.hasResp || strings.TrimSpace(m.resp.Body) == "" {
		m.copyNote = "先发一次请求：响应字段从当前响应推出来"
		m.invalidator.Invalidate()
		return
	}
	rows, err := inferFields(m.resp.Body)
	if err != nil {
		m.copyNote = "当前响应不是 JSON，没法推响应字段"
		m.invalidator.Invalidate()
		return
	}
	if overwrite {
		m.req.resp.fields = overwriteFieldRows(m.req.resp.fields, rows)
	} else {
		m.req.resp.fields = appendFieldRows(m.req.resp.fields, rows)
	}
	m.req.resp.sample = m.resp.Body
	m.req.resp.ctype = normalizeContentType(m.resp.ContentType)
	m.req.resp.status = m.resp.Status
	m.req.resp.headers = respHeaderRows(m.resp.Headers)
	m.markEdited()
	m.copyNote = fmt.Sprintf("响应字段已更新：%d 行", len(m.req.resp.fields))
	m.invalidator.Invalidate()
}

// clearRespFields 清空响应字段表与生成时留下的响应快照。
func (m *Model) clearRespFields() {
	if len(m.req.resp.fields) == 0 && m.req.resp.sample == "" {
		return
	}
	m.req.resp = respDocDraft{}
	m.markEdited()
	m.invalidator.Invalidate()
}

// appendFieldRows 追加生成行：已有路径一律不动（说明 / 必填 / 示例都不改），只补表里没有的。
func appendFieldRows(current, generated []*fieldRow) []*fieldRow {
	seen := make(map[string]bool, len(current))
	out := make([]*fieldRow, 0, len(current)+len(generated))
	for _, row := range current {
		if row == nil {
			continue
		}
		seen[strings.TrimSpace(row.name)] = true
		out = append(out, row)
	}
	for _, row := range generated {
		if seen[strings.TrimSpace(row.name)] {
			continue
		}
		seen[strings.TrimSpace(row.name)] = true
		out = append(out, row)
	}
	return out
}

// overwriteFieldRows 按本次生成结果重建：路径相同的旧行只继承「说明」，其余列取新值。
func overwriteFieldRows(current, generated []*fieldRow) []*fieldRow {
	prev := make(map[string]*fieldRow, len(current))
	for _, row := range current {
		if row != nil {
			prev[strings.TrimSpace(row.name)] = row
		}
	}
	out := make([]*fieldRow, 0, len(generated))
	for _, row := range generated {
		next := *row
		next.required = true
		if old, ok := prev[strings.TrimSpace(row.name)]; ok {
			next.desc = old.desc
		}
		out = append(out, &next)
	}
	return out
}

// normalizeContentType 把响应的 MIME 归一到界面认的五个取值（json / text / xml / html / binary）。
func normalizeContentType(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "":
		return ""
	case s == "json" || s == "text" || s == "xml" || s == "html" || s == "binary":
		return s
	case strings.Contains(s, "json"):
		return "json"
	case strings.Contains(s, "xml"):
		return "xml"
	case strings.Contains(s, "html"):
		return "html"
	case strings.HasPrefix(s, "text/"):
		return "text"
	default:
		return "binary"
	}
}

// clearFields 清空字段表（生成错了能一键重来）。
func (m *Model) clearFields() {
	if len(m.req.fields) == 0 {
		return
	}
	m.req.fields = nil
	m.markEdited()
	m.invalidator.Invalidate()
}
