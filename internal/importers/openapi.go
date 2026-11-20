package importers

import (
	"fmt"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	share "github.com/leihenshang/api-doc-go-share/openapi"
)

// OpenAPI 把 OpenAPI 3（JSON）文档转成条目：一个「路径 + 方法」一条请求。
//
// base 是地址前缀（文档里没有可用的 servers 时由用户决定，一般写 {{host}}）；
// 路径参数在地址里保留 {id} 原样，查询参数与请求头落进各自的表，描述进文档。
func OpenAPI(data []byte, base string) ([]Item, Summary, error) {
	document, err := share.Decode(data)
	if err != nil {
		return nil, Summary{}, fmt.Errorf("解析 OpenAPI: %w", err)
	}
	if len(document.Ops) == 0 {
		return nil, Summary{}, fmt.Errorf("这份文档里没有可导入的操作（paths 为空或只有 $ref）")
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	summary := Summary{}
	items := make([]Item, 0, len(document.Ops))
	for _, op := range document.Ops {
		method := strings.ToUpper(strings.TrimSpace(op.Method))
		if method == "" {
			method = "GET"
		}
		name := strings.TrimSpace(op.Summary)
		if name == "" {
			name = strings.TrimSpace(op.OperationID)
		}
		if name == "" {
			name = method + " " + op.Path
		}
		file := &collection.RequestFile{
			Info: collection.FileInfo{Name: name, Type: "http"},
			HTTP: collection.HTTPBlock{Method: method, URL: base + op.Path},
		}
		if strings.TrimSpace(op.Description) != "" {
			file.Docs = op.Description
		}
		for _, param := range op.Params {
			row := collection.KV{
				Name: param.Name, Value: param.Example, Enabled: true, Description: param.Description,
			}
			switch strings.ToLower(strings.TrimSpace(param.In)) {
			case "query":
				file.HTTP.Params = append(file.HTTP.Params, row)
			case "header":
				file.HTTP.Headers = append(file.HTTP.Headers, row)
			default:
				// path / cookie：地址里保留 {id} 原样，只留一条提示，不臆造参数表。
				if strings.TrimSpace(param.Name) != "" {
					summary.Notes = append(summary.Notes,
						"「"+name+"」的 "+param.In+" 参数 "+param.Name+" 没有导入（地址里保留 {"+param.Name+"}）")
				}
			}
		}
		items = append(items, Item{Name: name, Request: file})
	}
	return items, summary, nil
}
