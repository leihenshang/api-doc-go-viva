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
	params := make([]collection.KV, 0, len(raw.Query))
	parsed, err := neturl.Parse(target)
	if err == nil {
		for key, values := range parsed.Query() {
			for _, value := range values {
				params = append(params, collection.KV{Name: key, Value: value, Enabled: true})
			}
		}
		parsed.RawQuery = ""
		target = parsed.String()
	}
	if len(params) == 0 {
		for _, query := range raw.Query {
			if strings.TrimSpace(query.Key) == "" {
				continue
			}
			params = append(params, collection.KV{
				Name: query.Key, Value: query.Value, Description: query.Desc, Enabled: !query.Disabled,
			})
		}
	}
	return target, params
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
