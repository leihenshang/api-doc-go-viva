package importers

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// Postman 解析 Postman v2.x 导出的集合 JSON，转成待落盘的条目树。
//
// 只认验证版支持得住的部分：方法、地址（查询参数拆进 params）、请求头、
// raw / urlencoded / formdata 三种 body、bearer / basic / apikey 三种认证、
// 集合级变量。脚本（event）、pre-request、测试断言一概忽略并记一条提示。
func Postman(data []byte) ([]Item, Summary, error) {
	// Postman v1 与 v2 的顶层结构不同：v1 是 requests / folders 平铺 + order 数组，
	// v2 是嵌套的 item 树。顶层没有 item 但有这两者之一，就按 v1 走。
	var probe struct {
		Item     json.RawMessage `json:"item"`
		Requests json.RawMessage `json:"requests"`
		Folders  json.RawMessage `json:"folders"`
	}
	if json.Unmarshal(data, &probe) == nil && len(probe.Item) == 0 &&
		(len(probe.Requests) > 0 || len(probe.Folders) > 0) {
		return postmanV1(data)
	}
	var doc pmCollection
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, Summary{}, fmt.Errorf("不是 Postman 集合 JSON: %w", err)
	}
	if doc.Info.Schema != "" && !strings.Contains(doc.Info.Schema, "collection") {
		return nil, Summary{}, fmt.Errorf("不识别的 schema: %s", doc.Info.Schema)
	}
	summary := Summary{}
	auth := doc.Auth // 集合级认证作为请求的兜底
	items := convertItems(doc.Item, auth, &summary)
	for _, variable := range doc.Variable {
		if strings.TrimSpace(variable.Key) == "" {
			continue
		}
		summary.Vars = append(summary.Vars, collection.EnvVar{
			Name: variable.Key, Value: variable.Value, Enabled: true, Secret: false,
		})
	}
	if len(items) == 0 {
		return nil, summary, fmt.Errorf("集合里没有可导入的请求")
	}
	return items, summary, nil
}

// pmCollection 是 Postman 集合的顶层结构（只取用得到的字段）。
type pmCollection struct {
	Info struct {
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item     []pmItem `json:"item"`
	Auth     pmAuth   `json:"auth"`
	Variable []pmKV   `json:"variable"`
}

// pmItem 是文件夹或一条请求（两者用 Request 区分，Postman 里文件夹也有 item）。
type pmItem struct {
	Name    string     `json:"name"`
	Item    []pmItem   `json:"item"`
	Request *pmRequest `json:"request"`
}

type pmRequest struct {
	Method string `json:"method"`
	Header []pmKV `json:"header"`
	URL    pmURL  `json:"url"`
	Body   pmBody `json:"body"`
	Auth   pmAuth `json:"auth"`
	Desc   string `json:"description"`
}

type pmKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Disabled bool   `json:"disabled"`
	Desc     string `json:"description"`
}

// pmURL 的 host / path 既可能是字符串也可能是数组，这里统一按数组收。
type pmURL struct {
	Raw      string   `json:"raw"`
	Protocol string   `json:"protocol"`
	Host     flexList `json:"host"`
	Path     flexList `json:"path"`
	Query    []pmKV   `json:"query"`
}

// UnmarshalJSON 让 url 同时接受对象（v2.1）与纯字符串（老版本 / 手写）。
func (u *pmURL) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, `"`) {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*u = pmURL{Raw: raw}
		return nil
	}
	type plain pmURL
	var parsed plain
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*u = pmURL(parsed)
	return nil
}

type pmBody struct {
	Mode       string          `json:"mode"`
	Raw        string          `json:"raw"`
	URLEncoded []pmKV          `json:"urlencoded"`
	FormData   []pmKV          `json:"formdata"`
	Options    json.RawMessage `json:"options"`
}

type pmAuth struct {
	Type   string `json:"type"`
	Bearer []pmKV `json:"bearer"`
	Basic  []pmKV `json:"basic"`
	APIKey []pmKV `json:"apikey"`
}

// flexList 接受 ["a","b"] 或 "a" 两种写法。
type flexList []string

func (list *flexList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*list = many
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err != nil {
		return fmt.Errorf("host/path 既不是字符串也不是数组: %s", trimmed)
	}
	if one == "" {
		return nil
	}
	*list = []string{one}
	return nil
}

// convertItems 递归转换文件夹与请求。
func convertItems(items []pmItem, inherited pmAuth, summary *Summary) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Request == nil {
			if len(item.Item) == 0 {
				summary.Notes = append(summary.Notes, "跳过空文件夹："+item.Name)
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = "导入的分组"
			}
			out = append(out, Item{Name: name, Items: convertItems(item.Item, inherited, summary)})
			continue
		}
		file, note := convertRequest(item, inherited, summary)
		if note != "" {
			summary.Notes = append(summary.Notes, note)
			continue
		}
		out = append(out, Item{Name: file.Info.Name, Request: file})
	}
	return out
}

// convertRequest 把一条 Postman 请求变成 collection.RequestFile（uid / seq 由落盘时生成）。
func convertRequest(item pmItem, inherited pmAuth, summary *Summary) (*collection.RequestFile, string) {
	request := item.Request
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = "导入的请求"
	}
	target, params := pmTarget(request.URL)
	if target == "" {
		return nil, "跳过没有地址的请求：" + name
	}
	headers := make([]collection.KV, 0, len(request.Header))
	for _, header := range request.Header {
		if strings.TrimSpace(header.Key) == "" {
			continue
		}
		headers = append(headers, collection.KV{
			Name: header.Key, Value: header.Value, Description: header.Desc,
			Enabled: !header.Disabled,
		})
	}
	auth := request.Auth
	if auth.Type == "" {
		auth = inherited
	}
	body := convertBody(request.Body, name, summary)
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: name, Type: "http"},
		HTTP: collection.HTTPBlock{
			Method:  strings.ToUpper(strings.TrimSpace(request.Method)),
			URL:     target,
			Params:  params,
			Headers: headers,
			Body:    body,
			Auth:    convertAuth(auth, summary),
		},
		Docs: strings.TrimSpace(request.Desc),
	}
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	if len(file.HTTP.Params) == 0 {
		file.HTTP.Params = []collection.KV{{Enabled: true}}
	}
	return file, ""
}

// pmTarget 取地址并把查询参数拆进 params（集合里查询参数是独立可编辑的一张表）。
func pmTarget(raw pmURL) (string, []collection.KV) {
	target := strings.TrimSpace(raw.Raw)
	if target == "" && len(raw.Host) > 0 {
		host := strings.Join(raw.Host, ".")
		path := strings.Join(raw.Path, "/")
		if path != "" {
			path = "/" + strings.TrimPrefix(path, "/")
		}
		target = host + path
		if raw.Protocol != "" {
			target = raw.Protocol + "://" + target
		}
	}
	if target == "" {
		return "", nil
	}
	return splitTarget(target, raw.Query)
}

// splitTarget 把地址里的查询串拆进 params（集合里查询参数是独立可编辑的一张表）。
// 地址里的优先；地址里没有时才用文档里的 query 行（保留说明与启用状态）。
//
// 不用 url.Parse(target).String() 重建地址：那会把 `{{host}}` 变成 `%7B%7Bhost%7D%7D`，
// 占位符就此失效。这里只在 `?` 处切一刀，参数按原顺序逐个解码（map 会把顺序打乱）。
func splitTarget(target string, extra []pmKV) (string, []collection.KV) {
	base, rawQuery, hasQuery := strings.Cut(strings.TrimSpace(target), "?")
	params := make([]collection.KV, 0, 4)
	if hasQuery {
		for _, pair := range strings.Split(rawQuery, "&") {
			if strings.TrimSpace(pair) == "" {
				continue
			}
			key, value, _ := strings.Cut(pair, "=")
			name := decodeQueryPart(key)
			if strings.TrimSpace(name) == "" {
				continue
			}
			params = append(params, collection.KV{Name: name, Value: decodeQueryPart(value), Enabled: true})
		}
	}
	if len(params) == 0 {
		for _, query := range extra {
			if strings.TrimSpace(query.Key) == "" {
				continue
			}
			params = append(params, collection.KV{
				Name: query.Key, Value: query.Value, Description: query.Desc, Enabled: !query.Disabled,
			})
		}
	}
	return base, params
}

// decodeQueryPart 解 %XX；解不开就保留原文（脏地址不该让整条请求导不进来）。
func decodeQueryPart(part string) string {
	decoded, err := neturl.QueryUnescape(part)
	if err != nil {
		return part
	}
	return decoded
}

// convertBody 把 raw / urlencoded / formdata 映射成集合里的三种 body。
func convertBody(body pmBody, name string, summary *Summary) collection.Body {
	switch body.Mode {
	case "", "none":
		return collection.Body{Type: "none"}
	case "raw":
		typ := "text"
		if language := rawLanguage(body.Options); language == "json" {
			typ = "json"
		}
		return collection.Body{Type: typ, Raw: body.Raw}
	case "urlencoded":
		rows := make([]collection.KV, 0, len(body.URLEncoded))
		for _, field := range body.URLEncoded {
			rows = append(rows, collection.KV{
				Name: field.Key, Value: field.Value, Description: field.Desc,
				Enabled: !field.Disabled, Type: "text",
			})
		}
		return collection.Body{Type: "form", Form: rows}
	case "formdata":
		rows := make([]collection.KV, 0, len(body.FormData))
		for _, field := range body.FormData {
			typ := "text"
			value := field.Value
			if field.Type == "file" {
				typ = "file"
				// Postman 里 file 字段的 value 是 null / 绝对路径，这里只留文件名做提示。
				if value == "" {
					summary.Notes = append(summary.Notes, "「"+name+"」的文件字段没有路径，导入后需自己填")
				}
			}
			rows = append(rows, collection.KV{
				Name: field.Key, Value: value, Description: field.Desc,
				Enabled: !field.Disabled, Type: typ,
			})
		}
		return collection.Body{Type: "multipart", Form: rows}
	default:
		summary.Notes = append(summary.Notes, "「"+name+"」的 "+body.Mode+" body 不支持，已留空")
		return collection.Body{Type: "none"}
	}
}

// rawLanguage 读 options.raw.language（Postman 用它标 json / xml / text）。
func rawLanguage(options json.RawMessage) string {
	if len(options) == 0 {
		return ""
	}
	var parsed struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	}
	if json.Unmarshal(options, &parsed) != nil {
		return ""
	}
	return parsed.Raw.Language
}

// convertAuth 映射 bearer / basic / apikey（其余类型忽略并提示）。
func convertAuth(auth pmAuth, summary *Summary) *collection.Auth {
	pick := func(list []pmKV, key string) string {
		for _, item := range list {
			if strings.EqualFold(item.Key, key) {
				return item.Value
			}
		}
		return ""
	}
	switch auth.Type {
	case "":
		return nil
	case "bearer":
		if token := pick(auth.Bearer, "token"); token != "" {
			return &collection.Auth{Type: "bearer", Token: token}
		}
		summary.Notes = append(summary.Notes, "bearer 认证没有 token，已忽略")
		return nil
	case "basic":
		return &collection.Auth{Type: "basic", Username: pick(auth.Basic, "username"), Password: pick(auth.Basic, "password")}
	case "apikey":
		in := strings.ToLower(pick(auth.APIKey, "in"))
		if in != "query" {
			in = "header"
		}
		return &collection.Auth{
			Type: "apikey", Key: pick(auth.APIKey, "key"), Value: pick(auth.APIKey, "value"), In: in,
		}
	default:
		summary.Notes = append(summary.Notes, auth.Type+" 认证不支持，已忽略")
		return nil
	}
}

// ---------- Postman v1 ----------
//
// v1 的顶层是平铺的 requests / folders 加一个 order 数组（子项 id 顺序），
// 与 v2 的嵌套 item 完全不同。字段差异：
//   - 请求在 `requests[]`，含 method / url（字符串）/ headers / dataMode / data / rawModeData；
//   - 文件夹在 `folders[]`，子项顺序在自己的 `order` 里；
//   - 变量叫 `variables`（v2 是 `variable`）；
//   - 请求头可能是数组，也可能是一整段字符串（"A: 1\nB: 2"）。

type pmV1Doc struct {
	Name      string       `json:"name"`
	Desc      string       `json:"description"`
	Order     []string     `json:"order"`
	Folders   []pmV1Folder `json:"folders"`
	Requests  []pmV1Req    `json:"requests"`
	Variables []pmKV       `json:"variables"`
}

type pmV1Folder struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Order []string `json:"order"`
}

type pmV1Req struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	Headers     json.RawMessage `json:"headers"`
	DataMode    string          `json:"dataMode"`
	Data        []pmV1KV        `json:"data"`
	RawModeData string          `json:"rawModeData"`
	Description string          `json:"description"`
}

// pmV1KV 是 v1 的键值行：启用状态写的是 enabled（v2 写 disabled），两者都容忍。
type pmV1KV struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Enabled     *bool  `json:"enabled"`
	Disabled    bool   `json:"disabled"`
}

func (kv pmV1KV) on() bool {
	if kv.Enabled != nil {
		return *kv.Enabled
	}
	return !kv.Disabled
}

// postmanV1 把 v1 的平铺结构还原成条目树（顺序按 order，缺失的按数组顺序补齐）。
func postmanV1(data []byte) ([]Item, Summary, error) {
	var doc pmV1Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, Summary{}, fmt.Errorf("不是 Postman 集合 JSON: %w", err)
	}
	if len(doc.Requests) == 0 && len(doc.Folders) == 0 {
		return nil, Summary{}, fmt.Errorf("集合里没有可导入的请求（Postman v1 的 requests 为空）")
	}
	summary := Summary{}
	folders := make(map[string]pmV1Folder, len(doc.Folders))
	for _, folder := range doc.Folders {
		folders[folder.ID] = folder
	}
	requests := make(map[string]pmV1Req, len(doc.Requests))
	for _, request := range doc.Requests {
		requests[request.ID] = request
	}
	visited := make(map[string]bool, len(doc.Requests)+len(doc.Folders))

	var build func(ids []string) []Item
	build = func(ids []string) []Item {
		out := make([]Item, 0, len(ids))
		for _, id := range ids {
			if visited[id] {
				continue // 防环：v1 的 order 理论上不会有环，但脏数据不该让导入挂住
			}
			visited[id] = true
			if request, ok := requests[id]; ok {
				file, note := convertV1Request(request, &summary)
				if note != "" {
					summary.Notes = append(summary.Notes, note)
					continue
				}
				out = append(out, Item{Name: file.Info.Name, Request: file})
				continue
			}
			if folder, ok := folders[id]; ok {
				name := strings.TrimSpace(folder.Name)
				if name == "" {
					name = "导入的分组"
				}
				out = append(out, Item{Name: name, Items: build(folder.Order)})
			}
		}
		return out
	}
	items := build(doc.Order)

	// 有些导出没有 order（或只写了一半）：剩下的按原数组顺序补在最后，宁可顺序乱也不丢条目。
	for _, folder := range doc.Folders {
		if visited[folder.ID] {
			continue
		}
		visited[folder.ID] = true
		name := strings.TrimSpace(folder.Name)
		if name == "" {
			name = "导入的分组"
		}
		items = append(items, Item{Name: name, Items: build(folder.Order)})
	}
	for _, request := range doc.Requests {
		if visited[request.ID] {
			continue
		}
		visited[request.ID] = true
		file, note := convertV1Request(request, &summary)
		if note != "" {
			summary.Notes = append(summary.Notes, note)
			continue
		}
		items = append(items, Item{Name: file.Info.Name, Request: file})
	}

	for _, variable := range doc.Variables {
		if strings.TrimSpace(variable.Key) == "" {
			continue
		}
		summary.Vars = append(summary.Vars, collection.EnvVar{
			Name: variable.Key, Value: variable.Value, Enabled: true, Secret: false,
		})
	}
	if len(items) == 0 {
		return nil, summary, fmt.Errorf("集合里没有可导入的请求")
	}
	return items, summary, nil
}

// convertV1Request 把一条 v1 请求转成请求文件。
func convertV1Request(request pmV1Req, summary *Summary) (*collection.RequestFile, string) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "导入的请求"
	}
	target := strings.TrimSpace(request.URL)
	if target == "" {
		return nil, "跳过没有地址的请求：" + name
	}
	target, params := splitTarget(target, nil)
	if len(params) == 0 {
		params = []collection.KV{{Enabled: true}}
	}
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: name, Type: "http"},
		HTTP: collection.HTTPBlock{
			Method:  strings.ToUpper(strings.TrimSpace(request.Method)),
			URL:     target,
			Params:  params,
			Headers: v1Headers(request.Headers),
			Body:    v1Body(request, name, summary),
		},
		Docs: strings.TrimSpace(request.Description),
	}
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	return file, ""
}

// v1Headers 兼容两种写法：数组（key/value/enabled）与一整段 "A: 1\nB: 2" 文本。
func v1Headers(raw json.RawMessage) []collection.KV {
	if len(raw) == 0 {
		return nil
	}
	var rows []pmV1KV
	if err := json.Unmarshal(raw, &rows); err == nil {
		out := make([]collection.KV, 0, len(rows))
		for _, row := range rows {
			if strings.TrimSpace(row.Key) == "" {
				continue
			}
			out = append(out, collection.KV{
				Name: row.Key, Value: row.Value, Description: row.Description, Enabled: row.on(),
			})
		}
		return out
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil
	}
	out := make([]collection.KV, 0, 4)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out = append(out, collection.KV{Name: strings.TrimSpace(key), Value: strings.TrimSpace(value), Enabled: true})
	}
	return out
}

// v1Body 按 dataMode 映射 body：raw / urlencoded / params（form-data）/ binary。
func v1Body(request pmV1Req, name string, summary *Summary) collection.Body {
	switch strings.ToLower(strings.TrimSpace(request.DataMode)) {
	case "", "none":
		return collection.Body{Type: "none"}
	case "raw":
		typ := "text"
		if looksJSON(request.RawModeData) {
			typ = "json"
		}
		return collection.Body{Type: typ, Raw: request.RawModeData}
	case "urlencoded":
		rows := make([]collection.KV, 0, len(request.Data))
		for _, field := range request.Data {
			if strings.TrimSpace(field.Key) == "" {
				continue
			}
			rows = append(rows, collection.KV{
				Name: field.Key, Value: field.Value, Description: field.Description,
				Enabled: field.on(), Type: "text",
			})
		}
		return collection.Body{Type: "form", Form: rows}
	case "params":
		rows := make([]collection.KV, 0, len(request.Data))
		for _, field := range request.Data {
			if strings.TrimSpace(field.Key) == "" {
				continue
			}
			typ := "text"
			if strings.EqualFold(field.Type, "file") {
				typ = "file"
				if strings.TrimSpace(field.Value) == "" {
					summary.Notes = append(summary.Notes, "「"+name+"」的文件字段没有路径，导入后需自己填")
				}
			}
			rows = append(rows, collection.KV{
				Name: field.Key, Value: field.Value, Description: field.Description,
				Enabled: field.on(), Type: typ,
			})
		}
		return collection.Body{Type: "multipart", Form: rows}
	default:
		summary.Notes = append(summary.Notes, "「"+name+"」的 "+request.DataMode+" body 不支持，已留空")
		return collection.Body{Type: "none"}
	}
}

// looksJSON 只看首个非空字符：判断 raw 体该记成 json 还是 text。
func looksJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}
