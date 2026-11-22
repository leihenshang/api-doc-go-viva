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
	// 只接受对象根：字段表描述的是「一条记录有哪些键」，
	// 数组根（或标量根）没有键可写，硬推会生成一个名字为空的行。
	if _, ok := root.(map[string]any); !ok {
		return nil, errNotJSON
	}
	rows := make([]*fieldRow, 0, 16)
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
	if !m.hasResp || strings.TrimSpace(m.bodyText) == "" {
		m.copyNote = "先发一次请求：字段表从当前响应推出来"
		m.invalidator.Invalidate()
		return
	}
	rows, err := inferFields(m.bodyText)
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

// clearFields 清空字段表（生成错了能一键重来）。
func (m *Model) clearFields() {
	if len(m.req.fields) == 0 {
		return
	}
	m.req.fields = nil
	m.markEdited()
	m.invalidator.Invalidate()
}
