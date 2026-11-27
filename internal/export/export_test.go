package export

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// buildCollection 造一个带分组、请求、文档的集合，返回导出用的 Doc。
func buildCollection(t *testing.T) Doc {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	write("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 演示集合\n")
	write("健康检查.yml", `info:
  name: 健康检查
  type: http
  seq: 1
meta:
  uid: exp-root
http:
  method: GET
  url: "{{host}}/health"
  headers: []
  body:
    type: none
`)
	write("用户/登录.yml", `info:
  name: 登录
  type: http
  seq: 1
meta:
  uid: exp-login
summary: 账号密码换令牌
http:
  method: POST
  url: "{{host}}/api/v1/login"
  headers:
    - name: Content-Type
      value: application/json
      enabled: true
      description: 请求体类型
  params:
    - name: from
      value: web
      enabled: true
      description: 来源
  body:
    type: json
    raw: '{"user":"a"}'
    fields:
      - name: user
        type: string
        required: true
        example: admin
        description: 用户名
  auth:
    type: bearer
    token: "{{token}}"
docs: |-
  ## 说明

  - 支持 **游客** 与注册用户
`)
	write("docs/接口约定.md", "---\nname: 接口约定\n---\n\n# 约定\n\n出错时返回 `error.code`。\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return Doc{Name: col.Name, Col: col}
}

// TestMarkdownExport 导出的 Markdown 要有标题层级、地址、参数表、请求体、文档。
func TestMarkdownExport(t *testing.T) {
	out := Markdown([]Doc{buildCollection(t)})
	for _, want := range []string{
		"# 演示集合",
		"## 用户",
		"GET 健康检查",
		"POST 登录",
		"{{host}}/api/v1/login",
		"| 名称 | 值 | 说明 |",
		"| from | web | 来源 |",
		"**请求体**（json）",
		`{"user":"a"}`,
		"| 字段名 | 类型 | 必填 | 示例 | 说明 |",
		"| user | string | 是 | admin | 用户名 |",
		"Bearer Token",
		"## 说明",
		"接口约定",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Markdown 缺 %q：\n%s", want, out)
		}
	}
}

// TestHTMLLExportStructure HTML 要能单文件打开：一段脚本、目录与锚点一一对应、表格与转义都在。
func TestHTMLLExportStructure(t *testing.T) {
	out := HTML([]Doc{buildCollection(t)})
	if !strings.HasPrefix(out, "<!DOCTYPE html>") || !strings.HasSuffix(out, "</html>\n") {
		t.Fatalf("不是完整的 HTML 文档：\n%.200s", out)
	}
	if got := strings.Count(out, "<script>"); got != 1 {
		t.Fatalf("只应有 1 段内嵌脚本，实际 %d", got)
	}
	if !strings.Contains(out, "document.querySelector('.toc-list')") {
		t.Fatal("缺目录搜索脚本")
	}
	if !strings.Contains(out, "<table>") || !strings.Contains(out, "<pre><code>") {
		t.Fatal("缺表格或代码块")
	}
	// 目录链接与正文锚点必须一一对应。
	links := regexp.MustCompile(`href="#toc-(\d+)"`).FindAllStringSubmatch(out, -1)
	anchors := regexp.MustCompile(`id="toc-(\d+)"`).FindAllStringSubmatch(out, -1)
	if len(links) == 0 || len(links) != len(anchors) {
		t.Fatalf("目录 %d 条、锚点 %d 个，应一一对应", len(links), len(anchors))
	}
	seen := map[string]bool{}
	for _, anchor := range anchors {
		if seen[anchor[1]] {
			t.Fatalf("锚点 toc-%s 重复", anchor[1])
		}
		seen[anchor[1]] = true
	}
}

// TestHTMLExportEscapesUserText 请求名与文档里的尖括号必须转义，不能变成标签。
func TestHTMLExportEscapesUserText(t *testing.T) {
	doc := buildCollection(t)
	for _, entry := range doc.Col.Entries {
		if entry.Payload != nil {
			entry.Payload.Docs = "<script>alert(1)</script>"
			entry.Name = "登录<b>注入</b>"
		}
	}
	out := HTML([]Doc{doc})
	if strings.Contains(out, "alert(1)") && strings.Contains(out, "<script>alert") {
		t.Fatalf("文档里的脚本没有转义：\n%s", out)
	}
	if strings.Contains(out, "登录<b>注入</b>") {
		t.Fatal("请求名里的标签没有转义")
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("转义后的脚本文本应当作为文字出现")
	}
}

// TestExportHeadingLevelsStayInsideParents：正文小节与文档内标题都必须比承载它们的
// 标题更深一级 —— 导出的「单节展示」是按标题层级切片的，同级标题会把请求正文截断
// （之前「参数」被写成 h4、和请求标题同级，点开只看到 summary）。
func TestExportHeadingLevelsStayInsideParents(t *testing.T) {
	col := buildCollection(t)

	// Markdown：文档里的 # 标题要被压到条目标题之下
	md := Markdown([]Doc{col})
	if strings.Contains(md, "\n# 约定") {
		t.Fatalf("文档标题没有降级：\n%s", md)
	}
	if !strings.Contains(md, "\n#### 约定") {
		t.Fatalf("文档标题应降为 ####：\n%s", md)
	}

	// HTML：带 POST 徽标的请求标题之后，小节标题必须比它深一级
	out := HTML([]Doc{col})
	body := out[strings.Index(out, `<main class="content">`):strings.Index(out, "</main>")]
	badge := strings.Index(body, `class="tm m-post">POST<`)
	if badge < 0 {
		t.Fatal("找不到 POST 请求标题")
	}
	hStart := strings.LastIndex(body[:badge], "<h")
	if hStart < 0 {
		t.Fatal("请求标题不在正文里")
	}
	reqLevel := int(body[hStart+2] - '0')
	want := "<h" + strconv.Itoa(reqLevel+1) + ">参数</h" + strconv.Itoa(reqLevel+1) + ">"
	if !strings.Contains(body, want) {
		t.Fatalf("「参数」应是 h%d（比请求标题 h%d 深一级）", reqLevel+1, reqLevel)
	}
	// 请求标题之后紧跟的第一个标题也必须更深，否则单节展示会从这里截断。
	after := body[badge:]
	next := regexp.MustCompile(`<h(\d)[ >]`).FindStringSubmatch(after)
	if next != nil {
		if lvl, _ := strconv.Atoi(next[1]); lvl <= reqLevel {
			t.Fatalf("请求标题后紧跟 h%d，不深于 h%d，单节展示会从这里截断", lvl, reqLevel)
		}
	}
}

// TestHTMLExportUsesCollapsibleTOC：目录是可折叠树，右侧有当前页大纲。
func TestHTMLExportUsesCollapsibleTOC(t *testing.T) {
	out := HTML([]Doc{buildCollection(t)})
	for _, want := range []string{
		`<ul class="toc-list">`,
		`class="toc-folder" aria-expanded="true" data-default="true"`,
		`class="toc-toggle"`,
		`<span class="tm m-get">GET</span>`,
		`<nav class="ol" aria-label="当前页目录">`,
		`class="ol-open"`,
		`<main class="content">`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("HTML 缺 %q", want)
		}
	}
	// 只数目录那一段：脚本里可能提到标签名，会把整篇统计带偏。
	tocStart := strings.Index(out, `<nav class="toc"`)
	tocEnd := strings.Index(out, "</nav>")
	if tocStart < 0 || tocEnd < tocStart {
		t.Fatal("找不到目录段")
	}
	toc := out[tocStart : tocEnd+len("</nav>")]
	if open, close := strings.Count(toc, "<ul"), strings.Count(toc, "</ul>"); open != close {
		t.Fatalf("<ul> 不配平：%d 开 %d 闭", open, close)
	}
	if open, close := strings.Count(toc, "<li"), strings.Count(toc, "</li>"); open != close {
		t.Fatalf("<li> 不配平：%d 开 %d 闭", open, close)
	}
}

// TestSelectedExportSkipsUnchecked 勾选导出：没勾的条目不出现在正文与目录里，
// 勾空了的分组整节省略，计数与实际条数一致。
func TestSelectedExportSkipsUnchecked(t *testing.T) {
	doc := buildCollection(t)
	// 只勾「登录」（在「用户」分组里），根目录那条与文档都不勾。
	sel := Selection{}
	for _, entry := range doc.Col.Entries {
		if entry.Name == "登录" {
			sel[entry.File] = true
		}
	}
	requests, files := Count([]Doc{doc}, sel)
	if requests != 1 || files != 0 {
		t.Fatalf("计数不对：请求 %d、文档 %d", requests, files)
	}

	md := MarkdownSelected([]Doc{doc}, sel)
	if !strings.Contains(md, "POST 登录") {
		t.Fatalf("勾选的条目应当出现：\n%s", md)
	}
	for _, unwanted := range []string{"GET 健康检查", "接口约定"} {
		if strings.Contains(md, unwanted) {
			t.Fatalf("没勾的 %q 不该出现：\n%s", unwanted, md)
		}
	}

	out := HTMLSelected([]Doc{doc}, sel)
	// 正文标题现在是「方法徽标 + 名称」（对齐 Wails 版），不再是「POST 登录」连写。
	if !strings.Contains(out, `class="tm m-post">POST<`) || !strings.Contains(out, "登录") {
		t.Fatalf("HTML 里应含带徽标的 POST 登录")
	}
	if strings.Contains(out, "健康检查") {
		t.Fatalf("没勾的健康检查不该出现在 HTML 里")
	}
	// 目录只应剩集合名 + 分组 + 登录三段。
	links := regexp.MustCompile(`href="#toc-\d+"`).FindAllString(out, -1)
	if len(links) != 3 {
		t.Fatalf("目录应有 3 项（集合 / 用户 / 登录），实际 %d：\n%s", len(links), out)
	}

	// 什么都不勾：正文里没有任何请求，但仍然是完整文档。
	empty := Selection{}
	md = MarkdownSelected([]Doc{doc}, empty)
	if strings.Contains(md, "POST 登录") || strings.Contains(md, "GET 健康检查") {
		t.Fatalf("空选择不该导出条目：\n%s", md)
	}
	if requests, files := Count([]Doc{doc}, empty); requests != 0 || files != 0 {
		t.Fatalf("空选择计数应为 0：%d %d", requests, files)
	}
	if out := HTMLSelected([]Doc{doc}, empty); !strings.Contains(out, "</html>") {
		t.Fatal("空选择的 HTML 仍应是完整文档")
	}
}

// TestMarkdownEscapesTablePipe 表格单元格里的竖线不能把表格撑歪。
func TestMarkdownEscapesTablePipe(t *testing.T) {
	rows := [][]string{{"a|b", "1", "x"}}
	var b strings.Builder
	writeMarkdownTable(&b, "参数", []string{"名称", "值", "说明"}, rows)
	if !strings.Contains(b.String(), `a\|b`) {
		t.Fatalf("竖线没转义：\n%s", b.String())
	}
}

// TestEmptyCollection 空集合也要能导出（只有标题，不报错）。
func TestEmptyCollection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencollection.yml"),
		[]byte("opencollection: 1.0.0\ninfo:\n  name: 空集合\n"), 0o644); err != nil {
		t.Fatalf("写: %v", err)
	}
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	md := Markdown([]Doc{{Name: col.Name, Col: col}})
	if !strings.Contains(md, "# 空集合") {
		t.Fatalf("空集合也应有标题：%q", md)
	}
	out := HTML([]Doc{{Name: col.Name, Col: col}})
	if !strings.Contains(out, "</html>") {
		t.Fatal("空集合的 HTML 不完整")
	}
}
