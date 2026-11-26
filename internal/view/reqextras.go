package view

import (
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/script"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// 请求文件的「脚本侧」四段在草稿与磁盘之间的换算。
//
// 落盘形状由 explorer 负责（键名与 Wails 版一致）；这里只做 fieldRow / varRow /
// assertRow 与落盘结构之间的映射，避免界面层到处写转换。

// extrasFromDraft 把当前编辑草稿拍成落盘结构（保存与另存为共用）。
func (m *Model) extrasFromDraft() explorer.RequestExtras {
	ex := explorer.RequestExtras{
		ScriptPre:  m.req.scriptPre,
		ScriptPost: m.req.scriptPost,
	}
	for _, row := range m.req.vars {
		if row == nil {
			continue
		}
		ex.Vars = append(ex.Vars, explorer.VarRow{Name: row.name, Value: row.value, Enabled: row.enabled})
	}
	for _, row := range m.req.asserts {
		if row == nil || strings.TrimSpace(row.expr) == "" {
			continue
		}
		ex.Asserts = append(ex.Asserts, explorer.AssertRow{Name: row.name, Expr: row.expr})
	}
	doc := m.req.resp
	params := make([]explorer.RespParam, 0, len(doc.fields))
	for _, row := range doc.fields {
		if row == nil || strings.TrimSpace(row.name) == "" {
			continue
		}
		params = append(params, explorer.RespParam{
			FieldName:   row.name,
			Type:        row.typ,
			Required:    row.required,
			Description: row.desc,
			Example:     row.example,
		})
	}
	// 字段全空且没有样例/快照时不落 respdoc（老文件字节不变）。
	if len(params) > 0 || doc.sample != "" || doc.ctype != "" || doc.status != 0 || len(doc.headers) > 0 {
		ex.Resp = &explorer.RespDoc{
			Type:        rootJSONType(doc.sample),
			Params:      params,
			Sample:      doc.sample,
			ContentType: doc.ctype,
			Status:      doc.status,
			Headers:     doc.headers,
		}
	}
	return ex
}

// loadExtras 把磁盘上的脚本侧配置装进草稿。
func (d *requestDraft) loadExtras(ex explorer.RequestExtras) {
	for _, row := range ex.Vars {
		copied := &varRow{enabled: row.Enabled, name: row.Name, value: row.Value}
		d.vars = append(d.vars, copied)
	}
	d.scriptPre, d.scriptPost = ex.ScriptPre, ex.ScriptPost
	for _, row := range ex.Asserts {
		copied := &assertRow{name: row.Name, expr: row.Expr}
		d.asserts = append(d.asserts, copied)
	}
	if doc := ex.Resp; doc != nil {
		d.resp.sample = doc.Sample
		d.resp.ctype = doc.ContentType
		d.resp.status = doc.Status
		d.resp.headers = doc.Headers
		for _, param := range doc.Params {
			d.resp.fields = append(d.resp.fields, &fieldRow{
				name:     param.FieldName,
				typ:      param.Type,
				required: param.Required,
				example:  param.Example,
				desc:     param.Description,
			})
		}
	}
}

// scriptVars 是发送用的请求级变量（只取启用的行）。
func (m *Model) scriptVars() []script.Var {
	out := make([]script.Var, 0, len(m.req.vars))
	for _, row := range m.req.vars {
		if row == nil || !row.enabled || strings.TrimSpace(row.name) == "" {
			continue
		}
		out = append(out, script.Var{Name: row.name, Value: row.value, Enabled: true})
	}
	return out
}

// scriptAsserts 是发送用的断言列表（表达式为空的行跳过）。
func (m *Model) scriptAsserts() []script.Assert {
	out := make([]script.Assert, 0, len(m.req.asserts))
	for _, row := range m.req.asserts {
		if row == nil || strings.TrimSpace(row.expr) == "" {
			continue
		}
		out = append(out, script.Assert{Name: row.name, Expr: row.expr})
	}
	return out
}

// rootJSONType 按响应样例的首字符判根类型（{→object、[→array，其余空）。
func rootJSONType(sample string) string {
	s := strings.TrimSpace(sample)
	switch {
	case strings.HasPrefix(s, "{"):
		return "object"
	case strings.HasPrefix(s, "["):
		return "array"
	default:
		return ""
	}
}

// volatileRespHeaders 是易变 / 敏感响应头（小写）：写进集合只会让文件每次抖动或泄密。
var volatileRespHeaders = map[string]bool{
	"date": true, "connection": true, "keep-alive": true, "transfer-encoding": true,
	"te": true, "trailer": true, "upgrade": true,
	"proxy-authenticate": true, "proxy-authorization": true,
	"server": true, "content-length": true, "set-cookie": true,
}

// stableRespHeaders 过滤易变响应头，保留带业务语义的自定义头（如 X-Total-Count）。
func stableRespHeaders(headers []collection.KV) []collection.KV {
	out := make([]collection.KV, 0, len(headers))
	for _, header := range headers {
		if volatileRespHeaders[strings.ToLower(strings.TrimSpace(header.Name))] {
			continue
		}
		out = append(out, header)
	}
	return out
}

// headerMap 把响应头摊成脚本里的 res.headers 对象（同名后者覆盖）。
func headerMap(headers []send.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for _, header := range headers {
		out[header.Name] = header.Value
	}
	return out
}

// respHeaderRows 把一次发送的响应头转成落盘行（滤掉易变头）。
func respHeaderRows(headers []send.Header) []collection.KV {
	rows := make([]collection.KV, 0, len(headers))
	for _, header := range headers {
		rows = append(rows, collection.KV{Name: header.Name, Value: header.Value, Enabled: true})
	}
	return stableRespHeaders(rows)
}
