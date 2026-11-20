package view

import (
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// methodLabel 是角标文字：文档用 MD，gRPC 用协议名，其余用 http 方法。
func methodLabel(entry *explorer.Entry) string {
	switch entry.Kind {
	case explorer.KindDoc:
		return "MD"
	case explorer.KindGRPC:
		return "gRPC"
	}
	req := entry
	if req.Method == "" {
		return "HTTP"
	}
	return req.Method
}

// methodVariant 按动词选组件的变体：读用次要色、写用主色、删用危险色。
func methodVariant(entry *explorer.Entry) components.BadgeVariant {
	if entry.Kind == explorer.KindDoc || entry.Kind == explorer.KindGRPC {
		return components.BadgeOutline
	}
	req := entry
	switch req.Method {
	case "GET", "HEAD", "OPTIONS":
		return components.BadgeSecondary
	case "DELETE":
		return components.BadgeDestructive
	default:
		return components.BadgeDefault
	}
}

// kvRows 只收启用中的行：与真正发出去的请求保持同一份口径。
func kvRows(items []collection.KV) ui.TableRows {
	rows := make(ui.TableRows, 0, len(items))
	for _, kv := range items {
		if !kv.Enabled {
			continue
		}
		rows = append(rows, []string{kv.Name, kv.Value, kv.Description})
	}
	return rows
}

// newFieldEditor 把落盘的字段表转成可编辑行。
func newFieldEditor(items []collection.BodyField) []*fieldRow {
	rows := make([]*fieldRow, 0, len(items))
	for _, field := range items {
		rows = append(rows, &fieldRow{
			name: field.Name, typ: field.Type, required: field.Required,
			example: field.Example, desc: field.Description,
		})
	}
	return rows
}

// fieldsFromEditor 把可编辑行写回落盘结构。
func fieldsFromEditor(rows []*fieldRow) []collection.BodyField {
	out := make([]collection.BodyField, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.name) == "" {
			continue
		}
		out = append(out, collection.BodyField{
			Name: row.name, Type: row.typ, Required: row.required,
			Example: row.example, Description: row.desc,
		})
	}
	return out
}

// bodyRaw 取请求体正文：json/text 的原文在 raw，Bruno 兼容字段是 data。
func bodyRaw(body collection.Body) string {
	if body.Raw != "" {
		return body.Raw
	}
	return body.Data
}

// countLabel 是「4 条请求」/「1 / 4 条请求」这类计数文案，只在过滤词变化时生成。
func countLabel(shown, total int, filter string) string {
	if filter == "" || shown == total {
		return strconv.Itoa(total) + " 条内容"
	}
	return strconv.Itoa(shown) + " / " + strconv.Itoa(total) + " 条内容"
}

// envLabel 把「没选环境」写成中文，免得界面出现空括号。
func envLabel(name string) string {
	if name == "" {
		return "无"
	}
	return name
}

// orDash 把空值显示成一个破折号。
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// clip 截断过长文本：先按字节粗筛，超了才转 rune，避免每帧无谓分配。
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "\n…（已截断）"
}

// sameStrings 比较两个字符串切片是否一致（顺序敏感；分组顺序固定）。
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
