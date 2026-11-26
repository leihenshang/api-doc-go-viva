package explorer

import (
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// wailsRequestYAML 是 Wails 版写出的一条请求（含脚本侧四段 + 一个第三方未知键）。
const wailsRequestYAML = `info:
    name: 登录
    type: http
    seq: 1
meta:
    uid: u-1
http:
    method: POST
    url: /login
    body:
        type: none
docs: ""
vars:
    pre-request:
        - name: traceId
          value: "{{$uuid}}"
          enabled: true
script:
    pre-request: bru.setVar('ts', 1)
    post-response: bru.setVar('id', res.body.id)
assert:
    - name: 状态 200
      expr: res.status === 200
respdoc:
    type: object
    params:
        - fieldName: id
          type: string
          required: true
          description: 主键
          example: u-1
    sample: '{"id":"u-1"}'
    contentType: json
    status: 200
    headers:
        - name: Content-Type
          value: application/json
          enabled: true
custom-key: keep-me
`

// 读：Wails 写出的四段要能原样读进来（键名与形状必须对齐）。
func TestReadExtrasFromWailsFile(t *testing.T) {
	file, err := collection.DecodeRequest([]byte(wailsRequestYAML))
	if err != nil {
		t.Fatalf("解码: %v", err)
	}
	ex := ReadExtras(file)
	if len(ex.Vars) != 1 || ex.Vars[0].Name != "traceId" || !ex.Vars[0].Enabled {
		t.Fatalf("vars 未读出: %+v", ex.Vars)
	}
	if ex.ScriptPre == "" || ex.ScriptPost != "bru.setVar('id', res.body.id)" {
		t.Fatalf("script 未读出: %+v", ex)
	}
	if len(ex.Asserts) != 1 || ex.Asserts[0].Name != "状态 200" || ex.Asserts[0].Expr != "res.status === 200" {
		t.Fatalf("assert 未读出: %+v", ex.Asserts)
	}
	if ex.Resp == nil || ex.Resp.Status != 200 || ex.Resp.ContentType != "json" {
		t.Fatalf("respdoc 未读出: %+v", ex.Resp)
	}
	if len(ex.Resp.Params) != 1 || ex.Resp.Params[0].FieldName != "id" || !ex.Resp.Params[0].Required {
		t.Fatalf("respdoc.params 未读出: %+v", ex.Resp.Params)
	}
	if len(ex.Resp.Headers) != 1 || ex.Resp.Headers[0].Name != "Content-Type" {
		t.Fatalf("respdoc.headers 未读出: %+v", ex.Resp.Headers)
	}
}

// 写：值不变、键名不变、第三方未知键原样保留。
func TestWriteExtrasRoundTrip(t *testing.T) {
	file, err := collection.DecodeRequest([]byte(wailsRequestYAML))
	if err != nil {
		t.Fatalf("解码: %v", err)
	}
	ex := ReadExtras(file)
	WriteExtras(file, ex)
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	text := string(data)
	for _, want := range []string{"vars:", "pre-request:", "script:", "post-response:", "assert:", "respdoc:", "fieldName: id", "custom-key: keep-me"} {
		if !strings.Contains(text, want) {
			t.Fatalf("写回缺少 %q:\n%s", want, text)
		}
	}
	again, err := collection.DecodeRequest(data)
	if err != nil {
		t.Fatalf("再解码: %v", err)
	}
	round := ReadExtras(again)
	if len(round.Vars) != 1 || round.Vars[0].Value != "{{$uuid}}" {
		t.Fatalf("vars 往返丢失: %+v", round.Vars)
	}
	if round.Resp == nil || len(round.Resp.Params) != 1 || round.Resp.Params[0].Example != "u-1" {
		t.Fatalf("respdoc 往返丢失: %+v", round.Resp)
	}
}

// 清空：四个键都要删掉，不留空壳（老文件字节不变）。
func TestWriteExtrasRemovesEmptySections(t *testing.T) {
	file, err := collection.DecodeRequest([]byte(wailsRequestYAML))
	if err != nil {
		t.Fatalf("解码: %v", err)
	}
	WriteExtras(file, RequestExtras{})
	data, err := file.Encode()
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	text := string(data)
	for _, gone := range []string{"vars:", "script:", "assert:", "respdoc:"} {
		if strings.Contains(text, gone) {
			t.Fatalf("空段仍落盘 %q:\n%s", gone, text)
		}
	}
	if !strings.Contains(text, "custom-key: keep-me") {
		t.Fatalf("未知键被误删:\n%s", text)
	}
	if !ReadExtras(file).Empty() {
		t.Fatal("清空后应判空")
	}
}

// 形状不符（比如第三方把 vars 写成字符串）时按空处理，不 panic 也不报错。
func TestReadExtrasTolerant(t *testing.T) {
	file := &collection.RequestFile{Extra: map[string]any{
		"vars":   "not-a-map",
		"assert": "not-a-list",
		"script": 3,
	}}
	ex := ReadExtras(file)
	if !ex.Empty() {
		t.Fatalf("形状不符应为空: %+v", ex)
	}
}
