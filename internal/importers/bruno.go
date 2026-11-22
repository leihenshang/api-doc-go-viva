package importers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// Bruno 集合导入：把 **原生 .bru** 目录转成本仓库的请求文件。
//
// .bru 不是 YAML，是「块 + 花括号」：
//
//	meta {
//	  name: 列表
//	}
//	get {
//	  url: {{host}}/api/users
//	}
//	params:query {
//	  page: 1
//	  ~draft: 不启用
//	}
//	body:json {
//	  {"a": 1}
//	}
//
// 解析按「顶层块」切开，块内再逐行取 key: value；`~` 前缀表示未启用，
// 与 Bruno 自己的语义一致。
const brunoDepth = 16

// Bruno 把 dir 下的 .bru 目录树转成导入条目；folder 是落到当前集合的哪个分组。
func Bruno(dir, folder string) ([]Item, Summary, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, Summary{}, fmt.Errorf("先选一个 Bruno 集合目录")
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, Summary{}, fmt.Errorf("不是目录：%s", dir)
	}
	summary := Summary{}
	items, ok := walkBruno(dir, 0, &summary)
	if !ok {
		return nil, summary, fmt.Errorf("这个目录里没有 .bru 文件（Bruno 集合应有 bruno.json 或 *.bru）")
	}
	if len(summary.Notes) == 0 {
		summary.Notes = append(summary.Notes, "Bruno 集合：脚本与断言不导入（本版没有 JS 引擎）")
	}
	return items, summary, nil
}

// walkBruno 递归读一个目录：子目录成组，.bru 文件成请求。
func walkBruno(dir string, depth int, summary *Summary) ([]Item, bool) {
	if depth > brunoDepth {
		return nil, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	names := make([]string, 0, len(entries))
	byName := map[string]os.DirEntry{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		names = append(names, name)
		byName[name] = entry
	}
	sort.Strings(names)

	items := make([]Item, 0, len(names))
	found := false
	for _, name := range names {
		entry := byName[name]
		full := filepath.Join(dir, name)
		if entry.IsDir() {
			children, ok := walkBruno(full, depth+1, summary)
			if ok {
				found = true
				summary.Folders++
				items = append(items, Item{Name: name, Items: children})
			}
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".bru") {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			summary.Notes = append(summary.Notes, "读 "+name+" 失败: "+err.Error())
			continue
		}
		request, ok := parseBru(string(data), summary)
		if !ok {
			summary.Notes = append(summary.Notes, "跳过无法解析的 "+name)
			continue
		}
		found = true
		summary.Requests++
		items = append(items, Item{Name: request.Info.Name, Request: request})
	}
	return items, found
}

// bruBlock 是切开后的一个顶层块。
type bruBlock struct {
	name string
	body string
}

// splitBruno 按「name {」切开顶层块：块内不再嵌套（Bruno 的 .bru 就是这样）。
func splitBruno(text string) []bruBlock {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	blocks := make([]bruBlock, 0, 12)
	var current *bruBlock
	var body strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if current == nil {
			if strings.HasSuffix(trimmed, "{") && !strings.HasPrefix(trimmed, "}") {
				name := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
				current = &bruBlock{name: name}
				body.Reset()
			}
			continue
		}
		if trimmed == "}" {
			current.body = body.String()
			blocks = append(blocks, *current)
			current = nil
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if current != nil { // 没收尾的块也收下，别整份丢掉
		current.body = body.String()
		blocks = append(blocks, *current)
	}
	return blocks
}

// blockLines 返回块内的 `key: value`（去掉 `~` 前缀，第二个返回值是是否启用）。
func blockLines(body string) [][3]string {
	out := make([][3]string, 0, 8)
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		enabled := "1"
		if strings.HasPrefix(trimmed, "~") {
			enabled = "0"
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "~"))
		}
		key, value := trimmed, ""
		if at := strings.Index(trimmed, ":"); at >= 0 {
			key, value = trimmed[:at], strings.TrimSpace(trimmed[at+1:])
		}
		out = append(out, [3]string{strings.TrimSpace(key), strings.Trim(value, `"'`), enabled})
	}
	return out
}

// parseBru 把一个 .bru 文件解析成请求文件。
func parseBru(text string, summary *Summary) (*collection.RequestFile, bool) {
	blocks := splitBruno(text)
	file := &collection.RequestFile{Info: collection.FileInfo{Type: "http"}}
	file.HTTP.Body.Type = "none"

	for _, block := range blocks {
		switch {
		case block.name == "meta":
			for _, row := range blockLines(block.body) {
				switch row[0] {
				case "name":
					file.Info.Name = row[1]
				case "seq":
					if seq, err := strconv.Atoi(row[1]); err == nil {
						file.Info.Seq = seq
					}
				}
			}
		case isBrunoMethodBlock(block.name):
			file.HTTP.Method = strings.ToUpper(block.name)
			for _, row := range blockLines(block.body) {
				switch row[0] {
				case "url":
					file.HTTP.URL = row[1]
				case "auth":
					if row[1] == "inherit" {
						file.HTTP.Auth = &collection.Auth{Type: "inherit"}
					}
				}
			}
		case block.name == "headers":
			for _, row := range blockLines(block.body) {
				file.HTTP.Headers = append(file.HTTP.Headers, collection.KV{
					Name: row[0], Value: row[1], Enabled: row[2] == "1",
				})
			}
		case block.name == "params:query":
			for _, row := range blockLines(block.body) {
				file.HTTP.Params = append(file.HTTP.Params, collection.KV{
					Name: row[0], Value: row[1], Enabled: row[2] == "1",
				})
			}
		case block.name == "params:path":
			// 路径参数在地址里已经是 {id}，只留一条提示，不臆造参数表。
			if names := blockLines(block.body); len(names) > 0 {
				summary.Notes = append(summary.Notes,
					"「"+file.Info.Name+"」的路径参数保留在地址里（"+names[0][0]+"…）")
			}
		case strings.HasPrefix(block.name, "body:"):
			applyBrunoBody(file, strings.TrimPrefix(block.name, "body:"), block.body, summary)
		case strings.HasPrefix(block.name, "auth:"):
			applyBrunoAuth(file, strings.TrimPrefix(block.name, "auth:"), block.body)
		case block.name == "docs":
			file.Docs = strings.TrimSpace(block.body)
		case strings.HasPrefix(block.name, "script:") || block.name == "tests" || block.name == "assert":
			summary.Notes = append(summary.Notes,
				"「"+file.Info.Name+"」的脚本 / 断言没有导入（脚本段："+block.name+"）")
		}
	}

	if strings.TrimSpace(file.Info.Name) == "" {
		file.Info.Name = "未命名请求"
	}
	if file.HTTP.Method == "" {
		file.HTTP.Method = "GET"
	}
	if strings.TrimSpace(file.HTTP.URL) == "" {
		return nil, false
	}
	return file, true
}

// isBrunoMethodBlock 判断块名是不是 HTTP 方法（Bruno 用 get/post/... 小写块）。
func isBrunoMethodBlock(name string) bool {
	switch strings.ToLower(name) {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace", "connect":
		return true
	}
	return false
}

// applyBrunoBody 按 body 子类型写请求体。
func applyBrunoBody(file *collection.RequestFile, kind, body string, summary *Summary) {
	raw := strings.TrimSpace(body)
	switch kind {
	case "json":
		file.HTTP.Body.Type = "json"
		file.HTTP.Body.Raw = raw
		file.HTTP.Body.Data = raw // Bruno 原字段，往返保留
	case "text", "xml", "sparql", "graphql":
		file.HTTP.Body.Type = "text"
		file.HTTP.Body.Raw = raw
	case "form-urlencoded":
		file.HTTP.Body.Type = "form"
		for _, row := range blockLines(body) {
			file.HTTP.Body.Form = append(file.HTTP.Body.Form, collection.KV{
				Name: row[0], Value: row[1], Enabled: row[2] == "1",
			})
		}
	case "multipart-form":
		file.HTTP.Body.Type = "multipart"
		for _, row := range blockLines(body) {
			kv := collection.KV{Name: row[0], Value: row[1], Enabled: row[2] == "1"}
			if strings.HasPrefix(kv.Value, "@") { // @file(...) 表示文件行
				kv.Type = "file"
				kv.Value = strings.TrimPrefix(strings.TrimPrefix(kv.Value, "@file("), ")")
			}
			file.HTTP.Body.Form = append(file.HTTP.Body.Form, kv)
		}
	case "none", "":
		file.HTTP.Body.Type = "none"
	default:
		summary.Notes = append(summary.Notes, "「"+file.Info.Name+"」的 body:"+kind+" 暂不支持，只保留了地址")
	}
}

// applyBrunoAuth 把 auth:* 块转成本仓库的认证结构。
func applyBrunoAuth(file *collection.RequestFile, kind, body string) {
	rows := blockLines(body)
	value := func(name string) string {
		for _, row := range rows {
			if row[0] == name {
				return row[1]
			}
		}
		return ""
	}
	switch kind {
	case "bearer":
		file.HTTP.Auth = &collection.Auth{Type: "bearer", Token: value("token")}
	case "basic":
		file.HTTP.Auth = &collection.Auth{
			Type: "basic", Username: value("username"), Password: value("password"),
		}
	case "apikey":
		place := value("placement")
		if place == "" {
			place = "header"
		}
		file.HTTP.Auth = &collection.Auth{
			Type: "apikey", Key: value("key"), Value: value("value"), In: place,
		}
	case "none", "":
		file.HTTP.Auth = nil
	}
}
