package export

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/openapi"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// placeholder 匹配 {{变量}}：生成 OpenAPI 时要把它换成可解析的 base。
var placeholder = regexp.MustCompile(`\{\{\s*[^{}]+?\s*\}\}`)

// OpenAPI 把集合生成为 OpenAPI 3.0 的 JSON。
//
// 与 importers.OpenAPI 用的是同一个中立文档模型，所以「导出再导入」能跑通：
// 路径、方法、查询参数、请求头、描述都能还原（请求体与其字段表不在这一版范围内）。
func OpenAPI(col *explorer.Collection, sel Selection) ([]byte, error) {
	if col == nil || col.Dir == "" {
		return nil, fmt.Errorf("没有可导出的集合")
	}
	doc := openapi.Document{Title: col.Name, Version: openapi.Version}
	seen := map[string]bool{}
	for _, entry := range col.Entries {
		if entry == nil || entry.Kind != explorer.KindHTTP || entry.Payload == nil {
			continue
		}
		if !sel.Has(entry.File) {
			continue
		}
		block := entry.Payload.HTTP
		method := strings.ToUpper(strings.TrimSpace(block.Method))
		if method == "" {
			method = "GET"
		}
		path := pathOf(block.URL)
		if path == "" {
			continue
		}
		key := method + " " + path
		if seen[key] {
			continue // 同一路径+方法的第二条请求不重复写
		}
		seen[key] = true

		op := openapi.Operation{
			Path:    path,
			Method:  method,
			Summary: firstNonEmpty(entry.Summary, entry.Name),
			Tags:    tagOf(entry.Group),
		}
		for _, kv := range block.Params {
			if !kv.Enabled || strings.TrimSpace(kv.Name) == "" {
				continue
			}
			op.Params = append(op.Params, openapi.Param{
				Name: kv.Name, In: "query", Type: "string",
				Description: kv.Description, Example: kv.Value,
			})
		}
		for _, kv := range block.Headers {
			if !kv.Enabled || strings.TrimSpace(kv.Name) == "" {
				continue
			}
			op.Params = append(op.Params, openapi.Param{
				Name: kv.Name, In: "header", Type: "string",
				Description: kv.Description, Example: kv.Value,
			})
		}
		op.Responses = []openapi.Response{{Code: "200", Description: openapi.DefaultResponseDescription}}
		doc.Ops = append(doc.Ops, op)
	}
	sort.SliceStable(doc.Ops, func(i, j int) bool {
		if doc.Ops[i].Path != doc.Ops[j].Path {
			return doc.Ops[i].Path < doc.Ops[j].Path
		}
		return doc.Ops[i].Method < doc.Ops[j].Method
	})
	data, err := json.MarshalIndent(doc.Encode(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成 OpenAPI: %w", err)
	}
	return data, nil
}

// pathOf 从请求地址里取出路径部分。
//
// 地址可能长成 {{host}}/api/users、https://x.dev/api/users?page=1 或 /api/users，
// 统一先抹掉变量，再按有无 scheme 分开处理；查询串丢掉（参数在 params 表里）。
func pathOf(raw string) string {
	cleaned := strings.TrimSpace(placeholder.ReplaceAllString(raw, ""))
	if cleaned == "" {
		return "/"
	}
	if strings.Contains(cleaned, "://") {
		if parsed, err := url.Parse(cleaned); err == nil {
			path := parsed.Path
			if path == "" {
				path = "/"
			}
			return path
		}
	}
	if index := strings.IndexAny(cleaned, "?#"); index >= 0 {
		cleaned = cleaned[:index]
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

// tagOf 用分组名当 OpenAPI 的 tag：导入回来时正好还能还原成目录。
func tagOf(group string) []string {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil
	}
	return []string{group}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
