package importers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// Insomnia 解析 Insomnia 导出的 insomnia.json（v4 的 resources 结构），转成条目树。
//
// 只读导入：方法、地址、查询参数、请求头、body（JSON / 文本 / 表单 / 多部分）、
// bearer / basic / apikey 认证，以及第一个环境的变量。
// 脚本（after-response / pre-request）、测试断言、OAuth 之类一律忽略并记提示。
//
// 与 viva 的 {{变量}} 语法不同，Insomnia 写的是 {{ _.变量 }}：这里顺手转成
// viva / Wails 都能认的写法，省得导入完每个请求都要手改一遍。
func Insomnia(data []byte) ([]Item, Summary, error) {
	var doc insomniaExport
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, Summary{}, fmt.Errorf("不是 Insomnia 导出的 JSON: %w", err)
	}
	if len(doc.Resources) == 0 {
		return nil, Summary{}, fmt.Errorf("文件里没有 resources：确认是 Insomnia 的 Export Data 产物")
	}
	summary := Summary{}
	kind := make(map[string]string, len(doc.Resources))
	children := make(map[string][]insomniaResource, len(doc.Resources))
	envs := make([]insomniaResource, 0, 2)
	for _, resource := range doc.Resources {
		kind[resource.ID] = resource.Type
		children[resource.ParentID] = append(children[resource.ParentID], resource)
		if resource.Type == "environment" {
			envs = append(envs, resource)
		}
	}

	// 顶层 = 不挂在另一个分组下的分组与请求（workspace / collection 的直系子项，
	// 或者干脆没有父级）。挂在 request_group 下的由父级递归带出来。
	roots := make([]insomniaResource, 0, 8)
	for _, resource := range doc.Resources {
		if resource.Type != "request_group" && resource.Type != "request" {
			continue
		}
		if kind[resource.ParentID] == "request_group" {
			continue
		}
		roots = append(roots, resource)
	}

	var build func(group insomniaResource) Item
	build = func(group insomniaResource) Item {
		item := Item{Name: groupName(group)}
		for _, child := range children[group.ID] {
			switch child.Type {
			case "request_group":
				item.Items = append(item.Items, build(child))
			case "request":
				file, note := convertInsomniaRequest(child, &summary)
				if note != "" {
					summary.Notes = append(summary.Notes, note)
					continue
				}
				item.Items = append(item.Items, Item{Name: file.Info.Name, Request: file})
			}
		}
		return item
	}

	items := make([]Item, 0, len(roots))
	for _, root := range roots {
		switch root.Type {
		case "request_group":
			items = append(items, build(root))
		case "request":
			file, note := convertInsomniaRequest(root, &summary)
			if note != "" {
				summary.Notes = append(summary.Notes, note)
				continue
			}
			items = append(items, Item{Name: file.Info.Name, Request: file})
		}
	}
	if len(items) == 0 {
		return nil, summary, fmt.Errorf("导出里没有可导入的请求")
	}

	// 变量：Insomnia 每个环境一份。这里只取第一份（通常是 Base Environment），
	// 多环境要人工拆——把几份混进一个环境只会互相覆盖。
	if len(envs) > 0 {
		if len(envs) > 1 {
			summary.Notes = append(summary.Notes,
				"导出里有 "+fmt.Sprint(len(envs))+" 个环境，只导入了「"+envLabel(envs[0])+"」")
		}
		summary.Vars = append(summary.Vars, insomniaVars(envs[0].Data)...)
	}
	return items, summary, nil
}

// insomniaExport 是导出文件的顶层（v4）。
type insomniaExport struct {
	Type      string             `json:"_type"`
	Format    int                `json:"__export_format"`
	Resources []insomniaResource `json:"resources"`
}

// insomniaResource 是导出里的一条资源；按 _type 分派（workspace / environment /
// request_group / request / cookie_jar / unit_test …）。
type insomniaResource struct {
	ID       string          `json:"_id"`
	Type     string          `json:"_type"`
	ParentID string          `json:"parentId"`
	Name     string          `json:"name"`
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Headers  []insomniaKV    `json:"headers"`
	Params   []insomniaKV    `json:"parameters"`
	Body     insomniaBody    `json:"body"`
	Auth     json.RawMessage `json:"authentication"`
	Data     map[string]any  `json:"data"`
}

type insomniaKV struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
	Type     string `json:"type"`
	FileName string `json:"fileName"`
}

type insomniaBody struct {
	MimeType string       `json:"mimeType"`
	Text     string       `json:"text"`
	Params   []insomniaKV `json:"params"`
}

// insomniaVarPattern 匹配 Insomnia 的 {{ _.名字 }}（Nunjucks 变量）。
var insomniaVarPattern = regexp.MustCompile(`\{\{\s*_\.([A-Za-z0-9_]+)\s*\}\}`)

// toVivaVars 把 {{ _.host }} 换成 {{host}}。
func toVivaVars(text string) string {
	return insomniaVarPattern.ReplaceAllString(text, "{{$1}}")
}

func groupName(group insomniaResource) string {
	name := strings.TrimSpace(group.Name)
	if name == "" {
		return "导入的分组"
	}
	return name
}

// convertInsomniaRequest 把一条 request 资源转成请求文件。
func convertInsomniaRequest(request insomniaResource, summary *Summary) (*collection.RequestFile, string) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "导入的请求"
	}
	target := strings.TrimSpace(toVivaVars(request.URL))
	if target == "" {
		return nil, "跳过没有地址的请求：" + name
	}
	var query []pmKV
	for _, param := range request.Params {
		if strings.TrimSpace(param.Name) == "" {
			continue
		}
		query = append(query, pmKV{Key: param.Name, Value: param.Value, Disabled: param.Disabled})
	}
	target, params := splitTarget(target, query)
	if len(params) == 0 {
		params = []collection.KV{{Enabled: true}}
	}
	headers := make([]collection.KV, 0, len(request.Headers))
	for _, header := range request.Headers {
		if strings.TrimSpace(header.Name) == "" {
			continue
		}
		headers = append(headers, collection.KV{
			Name: header.Name, Value: toVivaVars(header.Value), Enabled: !header.Disabled,
		})
	}
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: name, Type: "http"},
		HTTP: collection.HTTPBlock{
			Method:  strings.ToUpper(strings.TrimSpace(request.Method)),
			URL:     target,
			Params:  params,
			Headers: headers,
			Body:    insomniaBodyOf(request.Body, name, summary),
			Auth:    insomniaAuth(request.Auth, name, summary),
		},
	}
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	// 请求的 description 在 Insomnia 里挂在 request 的 description 字段（不常出现），
	// 有就带进文档；没有就留空，不臆造。
	return file, ""
}

// insomniaBodyOf 按 mimeType 映射 body。
func insomniaBodyOf(body insomniaBody, name string, summary *Summary) collection.Body {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(body.MimeType, ";", 2)[0]))
	switch {
	case mime == "" && strings.TrimSpace(body.Text) == "" && len(body.Params) == 0:
		return collection.Body{Type: "none"}
	case mime == "application/json" || strings.HasSuffix(mime, "+json"):
		return collection.Body{Type: "json", Raw: toVivaVars(body.Text)}
	case mime == "application/x-www-form-urlencoded":
		rows := make([]collection.KV, 0, len(body.Params))
		for _, field := range body.Params {
			if strings.TrimSpace(field.Name) == "" {
				continue
			}
			rows = append(rows, collection.KV{
				Name: field.Name, Value: toVivaVars(field.Value), Enabled: !field.Disabled, Type: "text",
			})
		}
		return collection.Body{Type: "form", Form: rows}
	case mime == "multipart/form-data":
		rows := make([]collection.KV, 0, len(body.Params))
		for _, field := range body.Params {
			if strings.TrimSpace(field.Name) == "" {
				continue
			}
			typ := "text"
			value := toVivaVars(field.Value)
			if strings.EqualFold(field.Type, "file") {
				typ = "file"
				value = field.FileName
				if strings.TrimSpace(value) == "" {
					summary.Notes = append(summary.Notes, "「"+name+"」的文件字段没有文件名，导入后需自己填")
				}
			}
			rows = append(rows, collection.KV{
				Name: field.Name, Value: value, Enabled: !field.Disabled, Type: typ,
			})
		}
		return collection.Body{Type: "multipart", Form: rows}
	case strings.HasPrefix(mime, "text/"):
		return collection.Body{Type: "text", Raw: toVivaVars(body.Text)}
	default:
		summary.Notes = append(summary.Notes, "「"+name+"」的 "+body.MimeType+" body 不支持，已留空")
		return collection.Body{Type: "none"}
	}
}

// insomniaAuth 映射 bearer / basic / apikey；OAuth 之类忽略并提示。
func insomniaAuth(raw json.RawMessage, name string, summary *Summary) *collection.Auth {
	if len(raw) == 0 {
		return nil
	}
	var auth struct {
		Type     string `json:"type"`
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
		Key      string `json:"key"`
		Value    string `json:"value"`
		In       string `json:"in"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(auth.Type)) {
	case "":
		return nil
	case "bearer":
		return &collection.Auth{Type: "bearer", Token: toVivaVars(auth.Token)}
	case "basic":
		return &collection.Auth{
			Type: "basic", Username: toVivaVars(auth.Username), Password: toVivaVars(auth.Password),
		}
	case "apikey":
		in := strings.ToLower(strings.TrimSpace(auth.In))
		if in != "query" {
			in = "header"
		}
		return &collection.Auth{Type: "apikey", Key: auth.Key, Value: toVivaVars(auth.Value), In: in}
	default:
		summary.Notes = append(summary.Notes, "「"+name+"」的 "+auth.Type+" 认证不支持，已忽略")
		return nil
	}
}

// envLabel 是环境在提示里的名字。
func envLabel(env insomniaResource) string {
	if name := strings.TrimSpace(env.Name); name != "" {
		return name
	}
	return env.ID
}

// insomniaVars 把环境 data 摊成变量行（只收标量；嵌套对象跳过并留空）。
func insomniaVars(data map[string]any) []collection.EnvVar {
	if len(data) == 0 {
		return nil
	}
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]collection.EnvVar, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		value, ok := scalarText(data[name])
		if !ok {
			continue
		}
		out = append(out, collection.EnvVar{Name: name, Value: value, Enabled: true})
	}
	return out
}

// scalarText 把环境值转成字符串；对象 / 数组返回 false（环境是单层键值表）。
func scalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", true
	case string:
		return typed, true
	case bool:
		return fmt.Sprint(typed), true
	case float64:
		return strings.TrimSuffix(fmt.Sprint(typed), ".0"), true
	default:
		return "", false
	}
}
