package explorer

import (
	"os"

	"github.com/leihenshang/api-doc-go-share/collection"
	"gopkg.in/yaml.v3"
)

// 请求文件里与 Wails 版共用的四个顶层键（Bruno 超集）。
//
// 这四个键不属于 share 的 RequestFile 结构体，解码时会被收进 Extra 原样保留；
// 键名与形状必须与 Wails 版一致（api-doc-go-client/internal/collection），
// 否则同一个集合在两端打开会互相丢配置。
const (
	extraVars    = "vars"
	extraScript  = "script"
	extraAssert  = "assert"
	extraRespDoc = "respdoc"

	subPreRequest   = "pre-request"
	subPostResponse = "post-response"
)

// VarRow 是 vars.pre-request 的一行（请求级临时变量，覆盖环境同名值）。
type VarRow struct {
	Name    string `yaml:"name" json:"name"`
	Value   string `yaml:"value" json:"value"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// AssertRow 是 assert 列表的一条（expr 为 JS 布尔表达式，见 internal/script）。
type AssertRow struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	Expr string `yaml:"expr" json:"expr"`
}

// RespParam 是 respdoc.params 的一行，对齐 Wails 的 ResponseParam（fieldName 而非 name）。
type RespParam struct {
	FieldName   string `yaml:"fieldName" json:"fieldName"`
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required" json:"required"`
	Description string `yaml:"description" json:"description"`
	Example     string `yaml:"example" json:"example"`
}

// RespDoc 是响应字段表的落盘形态：字段表 + 生成时的响应快照。
//
// 为什么字段表独立于 body.fields：前者描述「这条接口返回什么」，后者描述「请求发什么」，
// Wails 版把两者分开存（respdoc 在顶层），合并会互相覆盖。
type RespDoc struct {
	Type   string      `yaml:"type" json:"type"`
	Params []RespParam `yaml:"params" json:"params"`
	Sample string      `yaml:"sample" json:"sample"`
	// ContentType / Status / Headers 是生成表格时的响应快照，只作说明用途。
	ContentType string          `yaml:"contentType,omitempty" json:"contentType,omitempty"`
	Status      int             `yaml:"status,omitempty" json:"status,omitempty"`
	Headers     []collection.KV `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// Empty 报告这份响应文档是否没有任何内容（全空时不落盘，保持老文件字节不变）。
func (d *RespDoc) Empty() bool {
	return d == nil || (d.Type == "" && len(d.Params) == 0 && d.Sample == "" &&
		d.ContentType == "" && d.Status == 0 && len(d.Headers) == 0)
}

// RequestExtras 是一条请求的脚本侧配置（变量 / 脚本 / 断言 / 响应字段）。
type RequestExtras struct {
	Vars       []VarRow
	ScriptPre  string
	ScriptPost string
	Asserts    []AssertRow
	Resp       *RespDoc
}

// Empty 报告四段是否全空。
func (e RequestExtras) Empty() bool {
	return len(e.Vars) == 0 && e.ScriptPre == "" && e.ScriptPost == "" &&
		len(e.Asserts) == 0 && e.Resp.Empty()
}

// ReadExtras 从请求文件读出脚本侧配置；缺段或形状不符时按空处理（不报错、不丢文件）。
func ReadExtras(file *collection.RequestFile) RequestExtras {
	var out RequestExtras
	if file == nil || file.Extra == nil {
		return out
	}
	if vars, ok := file.Extra[extraVars].(map[string]any); ok {
		if list, ok := vars[subPreRequest].([]any); ok {
			for _, item := range list {
				row, ok := decodeExtra[VarRow](item)
				if !ok {
					continue
				}
				out.Vars = append(out.Vars, row)
			}
		}
	}
	if scr, ok := file.Extra[extraScript].(map[string]any); ok {
		out.ScriptPre = extraString(scr[subPreRequest])
		out.ScriptPost = extraString(scr[subPostResponse])
	}
	if list, ok := file.Extra[extraAssert].([]any); ok {
		for _, item := range list {
			row, ok := decodeExtra[AssertRow](item)
			if !ok || row.Expr == "" {
				continue
			}
			out.Asserts = append(out.Asserts, row)
		}
	}
	if raw, ok := file.Extra[extraRespDoc]; ok && raw != nil {
		if doc, ok := decodeExtra[RespDoc](raw); ok && !doc.Empty() {
			out.Resp = &doc
		}
	}
	return out
}

// WriteExtras 把脚本侧配置写回请求文件的 Extra；为空的段删键而不是写空壳。
//
// 只动这四个键，其余未知顶层字段原样保留（Extra 里可能有第三方集合的自定义键）。
func WriteExtras(file *collection.RequestFile, ex RequestExtras) {
	if file == nil {
		return
	}
	if file.Extra == nil {
		if ex.Empty() {
			return
		}
		file.Extra = map[string]any{}
	}
	if len(ex.Vars) == 0 {
		delete(file.Extra, extraVars)
	} else {
		rows := make([]map[string]any, 0, len(ex.Vars))
		for _, v := range ex.Vars {
			rows = append(rows, map[string]any{"name": v.Name, "value": v.Value, "enabled": v.Enabled})
		}
		file.Extra[extraVars] = map[string]any{subPreRequest: rows}
	}
	if ex.ScriptPre == "" && ex.ScriptPost == "" {
		delete(file.Extra, extraScript)
	} else {
		scr := map[string]any{}
		if ex.ScriptPre != "" {
			scr[subPreRequest] = ex.ScriptPre
		}
		if ex.ScriptPost != "" {
			scr[subPostResponse] = ex.ScriptPost
		}
		file.Extra[extraScript] = scr
	}
	if len(ex.Asserts) == 0 {
		delete(file.Extra, extraAssert)
	} else {
		list := make([]map[string]any, 0, len(ex.Asserts))
		for _, a := range ex.Asserts {
			item := map[string]any{"expr": a.Expr}
			if a.Name != "" {
				item["name"] = a.Name
			}
			list = append(list, item)
		}
		file.Extra[extraAssert] = list
	}
	if ex.Resp.Empty() {
		delete(file.Extra, extraRespDoc)
	} else {
		file.Extra[extraRespDoc] = *ex.Resp
	}
	if len(file.Extra) == 0 {
		file.Extra = nil
	}
}

// SaveRequestExtras 给已存在的请求文件补写脚本侧四段（新建 / 另存为后调用）。
//
// 走「读回 → 改 Extra → 原子写」，因此不会覆盖同一次操作里别处写入的内容。
func SaveRequestExtras(collectionDir, rel string, ex RequestExtras) error {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	file, err := collection.DecodeRequest(data)
	if err != nil {
		return err
	}
	WriteExtras(file, ex)
	return SaveRequest(collectionDir, rel, file)
}

// decodeExtra 借助 yaml 把 Extra 里的通用值转成具体结构（键名按结构体 tag 匹配）。
func decodeExtra[T any](v any) (T, bool) {
	var out T
	if v == nil {
		return out, false
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return out, false
	}
	if yaml.Unmarshal(data, &out) != nil {
		return out, false
	}
	return out, true
}

// extraString 取一个 Extra 里的标量字符串；非字符串按空处理。
func extraString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
