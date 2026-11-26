package importers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	share "github.com/leihenshang/api-doc-go-share/openapi"
	"gopkg.in/yaml.v3"
)

// OpenAPI 把 OpenAPI 3 文档转成条目：一个「路径 + 方法」一条请求。
//
// base 是地址前缀（文档里没有可用的 servers 时由用户决定，一般写 {{host}}）；
// 路径参数在地址里保留 {id} 原样，查询参数与请求头落进各自的表，描述进文档。
//
// 文档既可以是 JSON 也可以是 YAML：分享库的 Decode 只吃 JSON，所以 YAML 先在这里
// 转成 JSON 再交给它（转换发生在验证版内，不动 share 这个共用模块）。
func OpenAPI(data []byte, base string) ([]Item, Summary, error) {
	normalized, fromYAML, err := normalizeOpenAPIDoc(data)
	if err != nil {
		return nil, Summary{}, err
	}
	document, err := share.Decode(normalized)
	if err != nil {
		return nil, Summary{}, fmt.Errorf("解析 OpenAPI: %w", err)
	}
	if len(document.Ops) == 0 {
		return nil, Summary{}, fmt.Errorf("这份文档里没有可导入的操作（paths 为空或只有 $ref）")
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	summary := Summary{}
	if fromYAML {
		summary.Notes = append(summary.Notes, "YAML 已转换为 JSON 后导入")
	}
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

// normalizeOpenAPIDoc 把 YAML 文档转成 JSON；返回 (字节, 是否做了转换, 错误)。
//
// 判定只看第一个非空字符：`{` / `[` 当 JSON 直接交给 Decode（它自己会报语法错），
// 其余一律按 YAML 解析——JSON 本身是 YAML 的子集，但这里不给 JSON 走 YAML 慢路。
func normalizeOpenAPIDoc(data []byte) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, false, fmt.Errorf("文档是空的：把 OpenAPI 的 JSON / YAML 粘进来，或选一个文件")
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return data, false, nil
	}
	var node any
	if err := yaml.Unmarshal(trimmed, &node); err != nil {
		// yaml 自己就会拒绝复合键（`? [a, b]`）：换成看得懂的话，而不是 invalid map key。
		if strings.Contains(err.Error(), "invalid map key") {
			return nil, false, fmt.Errorf("YAML 里有复合键（数组 / 对象作键）；OpenAPI 的键必须是字符串或数字")
		}
		return nil, false, fmt.Errorf("解析 YAML: %w", err)
	}
	plain, err := stringKeys(node, "$")
	if err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(plain)
	if err != nil {
		return nil, false, fmt.Errorf("YAML 转 JSON: %w", err)
	}
	return encoded, true, nil
}

// stringKeys 把 YAML 解出来的 map[any]any 换成 map[string]any。
//
// 标量键（数字 / 布尔）按字面转成字符串：OpenAPI 的 responses 习惯写成无引号的
// `200:`，那在 yaml 里是 int 键，报错会让绝大多数真实文档都导不进来。
// 复合键（map / 数组作键）在 JSON 里没有对应表示，直接给出带路径的错误，
// 而不是让 json.Marshal 抛一句 "unsupported type" 让人猜是哪个键。
func stringKeys(value any, path string) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			converted, err := stringKeys(child, path+"."+key)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			text, ok := scalarKey(key)
			if !ok {
				return nil, fmt.Errorf("YAML 里有复合键（位置 %s）；OpenAPI 的键必须是字符串或数字", path)
			}
			converted, err := stringKeys(child, path+"."+text)
			if err != nil {
				return nil, err
			}
			out[text] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			converted, err := stringKeys(child, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			out[index] = converted
		}
		return out, nil
	default:
		return value, nil
	}
}

// scalarKey 把 YAML 的标量键转成字符串；复合键返回 false。
func scalarKey(key any) (string, bool) {
	switch typed := key.(type) {
	case string:
		return typed, true
	case int, int64, uint64, float64, bool:
		return fmt.Sprint(typed), true
	default:
		return "", false
	}
}
