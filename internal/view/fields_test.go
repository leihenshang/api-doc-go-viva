package view

import (
	"strings"
	"testing"
)

// fieldNames 把行摊成「名字=类型」便于断言。
func fieldNames(rows []*fieldRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.name+"="+row.typ)
	}
	return out
}

// TestInferFieldsWalksJSON 嵌套对象展开、数组只记一行、示例只填标量。
func TestInferFieldsWalksJSON(t *testing.T) {
	body := `{
		"id": 12,
		"ok": true,
		"name": "neo",
		"user": {"nick": "n", "score": 1.5},
		"list": [{"a": 1}, {"a": 2}],
		"none": null
	}`
	rows, err := inferFields(body)
	if err != nil {
		t.Fatalf("inferFields: %v", err)
	}
	got := strings.Join(fieldNames(rows), ",")
	want := "id=number,list[]=array,name=string,none=null,ok=bool,user.nick=string,user.score=number"
	if got != want {
		t.Fatalf("字段不对：\n got %s\nwant %s", got, want)
	}
	// 示例：标量有、对象/数组没有。
	byName := map[string]*fieldRow{}
	for _, row := range rows {
		byName[row.name] = row
	}
	if byName["id"].example != "12" || byName["ok"].example != "true" {
		t.Fatalf("标量示例没填：%+v / %+v", byName["id"], byName["ok"])
	}
	if byName["list[]"].example != "" || byName["user.nick"].example != "n" {
		t.Fatalf("数组不该填示例：%+v", byName["list[]"])
	}
}

// TestInferFieldsRejectsNonObject 非 JSON / 标量根都算「推不出来」。
func TestInferFieldsRejectsNonObject(t *testing.T) {
	for _, body := range []string{"", "not json", "42", `"text"`, "[]"} {
		if _, err := inferFields(body); err == nil {
			t.Fatalf("%q 不该推出字段", body)
		}
	}
}

// TestMergeFieldsKeepsHumanNotes 自动生成不能抹掉人写的说明 / 必填 / 示例。
func TestMergeFieldsKeepsHumanNotes(t *testing.T) {
	existing := []*fieldRow{
		{name: "id", typ: "int", required: true, example: "1", desc: "主键"},
		{name: "legacy", typ: "string", desc: "旧字段"},
	}
	generated := []*fieldRow{
		{name: "id", typ: "number", example: "12"},
		{name: "name", typ: "string", example: "neo"},
	}
	merged := mergeFields(existing, generated)
	if len(merged) != 2 {
		t.Fatalf("应只保留生成的两行：%v", fieldNames(merged))
	}
	if merged[0].desc != "主键" || !merged[0].required || merged[0].example != "1" {
		t.Fatalf("同名字段的人写内容被覆盖了：%+v", merged[0])
	}
	if merged[0].typ != "number" {
		t.Fatalf("类型应更新为响应里的真实类型：%q", merged[0].typ)
	}
	if merged[1].name != "name" {
		t.Fatalf("新字段没进来：%+v", merged[1])
	}
}

// TestGenerateFieldsNeedsResponse 没发过请求时给一句人话，不生成空表。
func TestGenerateFieldsNeedsResponse(t *testing.T) {
	model := &Model{}
	model.generateFields()
	if len(model.req.fields) != 0 {
		t.Fatal("没有响应时不该生成字段")
	}
	if !strings.Contains(model.copyNote, "先发一次请求") {
		t.Fatalf("提示不对：%q", model.copyNote)
	}
}
