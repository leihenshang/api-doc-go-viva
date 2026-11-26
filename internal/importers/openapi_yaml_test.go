package importers

import (
	"strings"
	"testing"
)

// openapiJSON / openapiYAML 是同一份文档的两种写法：导入结果必须逐项相同。
const openapiJSON = `{
  "openapi": "3.0.0",
  "info": {"title": "示例", "version": "1.0.0"},
  "paths": {
    "/users/{id}": {
      "get": {
        "summary": "用户详情",
        "description": "按 id 取用户",
        "parameters": [
          {"name": "id", "in": "path", "required": true},
          {"name": "with", "in": "query", "example": "teams", "description": "附带信息"},
          {"name": "X-Trace", "in": "header", "example": "abc"}
        ],
        "responses": {"200": {"description": "ok"}}
      }
    },
    "/health": {"get": {"operationId": "healthCheck", "responses": {"200": {"description": "ok"}}}}
  }
}`

const openapiYAML = `openapi: 3.0.0
info:
  title: 示例
  version: 1.0.0
paths:
  /users/{id}:
    get:
      summary: 用户详情
      description: 按 id 取用户
      parameters:
        - name: id
          in: path
          required: true
        - name: with
          in: query
          example: teams
          description: 附带信息
        - name: X-Trace
          in: header
          example: abc
      responses:
        200:
          description: ok
  /health:
    get:
      operationId: healthCheck
      responses:
        200:
          description: ok
`

// YAML 与 JSON 两个版本导入后：条目数、方法、地址、参数、请求头、文档逐项相同。
func TestOpenAPIYAMLMatchesJSON(t *testing.T) {
	jsonItems, _, err := OpenAPI([]byte(openapiJSON), "{{host}}/api")
	if err != nil {
		t.Fatalf("JSON 导入: %v", err)
	}
	yamlItems, summary, err := OpenAPI([]byte(openapiYAML), "{{host}}/api")
	if err != nil {
		t.Fatalf("YAML 导入: %v", err)
	}
	if len(jsonItems) != len(yamlItems) {
		t.Fatalf("条目数不同：JSON %d / YAML %d", len(jsonItems), len(yamlItems))
	}
	for index := range jsonItems {
		want, got := jsonItems[index], yamlItems[index]
		if want.Name != got.Name || want.Request.HTTP.Method != got.Request.HTTP.Method ||
			want.Request.HTTP.URL != got.Request.HTTP.URL || want.Request.Docs != got.Request.Docs {
			t.Fatalf("第 %d 条不一致：\nJSON %+v\nYAML %+v", index, want.Request, got.Request)
		}
		if len(want.Request.HTTP.Params) != len(got.Request.HTTP.Params) ||
			len(want.Request.HTTP.Headers) != len(got.Request.HTTP.Headers) {
			t.Fatalf("第 %d 条表长不一致：%+v vs %+v", index, want.Request.HTTP, got.Request.HTTP)
		}
		for param := range want.Request.HTTP.Params {
			if want.Request.HTTP.Params[param] != got.Request.HTTP.Params[param] {
				t.Fatalf("查询参数不一致：%+v vs %+v", want.Request.HTTP.Params[param], got.Request.HTTP.Params[param])
			}
		}
	}
	if !strings.Contains(strings.Join(summary.Notes, "；"), "YAML 已转换") {
		t.Fatalf("导入摘要应注明 YAML 已转换：%v", summary.Notes)
	}
}

// Swagger 2.0 的 YAML 仍要走「请先转换」那条提示，而不是被当成 YAML 语法错误。
func TestOpenAPIYAMLSwagger2Rejected(t *testing.T) {
	doc := `swagger: "2.0"
info:
  title: 老文档
  version: 1.0.0
paths:
  /ping:
    get:
      responses:
        200:
          description: ok
`
	_, _, err := OpenAPI([]byte(doc), "")
	if err == nil || !strings.Contains(err.Error(), "请先转换") {
		t.Fatalf("Swagger 2.0 应提示先转换，实际 %v", err)
	}
}

// 复合键（数组作键）在 JSON 里没有表示：给出带位置的可读错误，而不是 unsupported type。
func TestOpenAPIYAMLCompositeKeyRejected(t *testing.T) {
	doc := `openapi: 3.0.0
paths:
  ? [a, b]
  : {}
`
	_, _, err := OpenAPI([]byte(doc), "")
	if err == nil || !strings.Contains(err.Error(), "复合键") {
		t.Fatalf("复合键应报可定位错误，实际 %v", err)
	}
}

// 空文档给出可操作的提示。
func TestOpenAPIEmptyDoc(t *testing.T) {
	if _, _, err := OpenAPI([]byte("   \n"), ""); err == nil || !strings.Contains(err.Error(), "文档是空的") {
		t.Fatalf("空文档提示不对：%v", err)
	}
}
