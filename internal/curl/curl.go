// Package curl 把「Copy as cURL」命令解析成请求文件，并把请求导出成 cURL。
//
// 解析规则沿用 api-doc-go-client/internal/collection/curl.go 的口径（本次只搬 Viva 版
// 需要的最小子集）：
//   - 支持 -X/--request、-H/--header、-d/--data/--data-raw/--data-binary、--json、
//     -u/--user、-b/--cookie、-A/--user-agent、-e/--referer、-G/--get、-I/--head、--url；
//   - 引号支持 bash 单引号、双引号（含 \" 转义）、$'…' 与 \ / ^ 续行；
//   - -d @文件、-F '字段=<文件' 这类取文件内容的写法明确报错，不静默丢数据。
package curl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// Parse 解析 curl 命令，返回可直接落盘的请求文件（不含 uid / path）。
func Parse(text string) (*collection.RequestFile, error) {
	tokens, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("curl 命令为空")
	}
	if !strings.EqualFold(strings.TrimSpace(tokens[0]), "curl") {
		return nil, fmt.Errorf("不是 curl 命令（首词为 %q）", tokens[0])
	}

	var (
		method  string
		target  string
		headers []collection.KV
		body    string
		hasBody bool
		basic   string
		jsonOpt bool
		get     bool
		head    bool
	)
	positionals := make([]string, 0, 2)
	args := tokens[1:]
	for i := 0; i < len(args); i++ {
		tok := args[i]
		take := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s 缺少取值", tok)
			}
			i++
			return args[i], nil
		}
		switch {
		case tok == "-X" || tok == "--request":
			value, err := take()
			if err != nil {
				return nil, err
			}
			method = strings.ToUpper(value)
		case tok == "--url":
			value, err := take()
			if err != nil {
				return nil, err
			}
			target = value
		case tok == "-H" || tok == "--header":
			value, err := take()
			if err != nil {
				return nil, err
			}
			if name, val, ok := strings.Cut(value, ":"); ok {
				headers = append(headers, collection.KV{Name: strings.TrimSpace(name), Value: strings.TrimSpace(val), Enabled: true})
			}
		case tok == "-d" || tok == "--data" || tok == "--data-raw" || tok == "--data-binary" || tok == "--data-ascii":
			value, err := take()
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(value, "@") {
				return nil, fmt.Errorf("-d @文件 需要请求体文件，验证版不支持")
			}
			if hasBody {
				body += "&"
			}
			body += value
			hasBody = true
		case tok == "--json":
			value, err := take()
			if err != nil {
				return nil, err
			}
			if hasBody {
				body += "&"
			}
			body += value
			hasBody, jsonOpt = true, true
		case tok == "-F" || tok == "--form":
			if _, err := take(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("multipart 导入暂不支持（%s）", tok)
		case tok == "-u" || tok == "--user":
			value, err := take()
			if err != nil {
				return nil, err
			}
			basic = value
		case tok == "-b" || tok == "--cookie":
			value, err := take()
			if err != nil {
				return nil, err
			}
			headers = append(headers, collection.KV{Name: "Cookie", Value: value, Enabled: true})
		case tok == "-A" || tok == "--user-agent":
			value, err := take()
			if err != nil {
				return nil, err
			}
			headers = append(headers, collection.KV{Name: "User-Agent", Value: value, Enabled: true})
		case tok == "-e" || tok == "--referer":
			value, err := take()
			if err != nil {
				return nil, err
			}
			headers = append(headers, collection.KV{Name: "Referer", Value: value, Enabled: true})
		case tok == "-G" || tok == "--get":
			get = true
		case tok == "-I" || tok == "--head":
			head = true
		case strings.HasPrefix(tok, "-"):
			// 其余选项（-sS、--compressed、-k、-L…）不改变请求内容；带取值的按已知表跳过。
			if optionNeedsValue(tok) {
				if _, err := take(); err != nil {
					return nil, err
				}
			}
		default:
			positionals = append(positionals, tok)
		}
	}
	if target == "" {
		target = pickURL(positionals)
	}
	if target == "" {
		return nil, fmt.Errorf("命令里没有 URL")
	}
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}

	file := &collection.RequestFile{Info: collection.FileInfo{Type: "http", Name: deriveName(target)}, Docs: ""}
	file.HTTP.URL = target
	file.HTTP.Method = pickMethod(method, head, get, hasBody)
	file.HTTP.Headers = headers
	if basic != "" {
		user, pass, _ := strings.Cut(basic, ":")
		file.HTTP.Auth = &collection.Auth{Type: "basic", Username: user, Password: pass}
	}
	if hasBody {
		bodyType := "text"
		if jsonOpt || isJSON(body) || hasJSONHeader(headers) {
			bodyType = "json"
		}
		file.HTTP.Body = collection.Body{Type: bodyType, Raw: body}
	} else {
		file.HTTP.Body = collection.Body{Type: "none"}
	}
	if get && hasBody {
		// -G 把 -d 的内容拼进 query。
		if parsed, err := url.Parse(target); err == nil {
			query := parsed.Query()
			for key, value := range parseQuery(body) {
				query.Set(key, value)
			}
			parsed.RawQuery = query.Encode()
			file.HTTP.URL = parsed.String()
			file.HTTP.Body = collection.Body{Type: "none"}
			hasBody = false
		}
	}
	return file, nil
}

// Generate 把请求导出成一条可粘贴的 curl 命令。
func Generate(file *collection.RequestFile) string {
	if file == nil {
		return ""
	}
	block := file.HTTP
	var b strings.Builder
	b.WriteString("curl")
	method := strings.ToUpper(strings.TrimSpace(block.Method))
	if method != "" && method != "GET" {
		b.WriteString(" -X " + method)
	}
	if block.URL != "" {
		b.WriteString(" " + quote(block.URL))
	}
	for _, header := range block.Headers {
		if !header.Enabled || strings.TrimSpace(header.Name) == "" {
			continue
		}
		b.WriteString(" \\\n  -H " + quote(header.Name+": "+header.Value))
	}
	if auth := block.Auth; auth != nil && auth.Type == "basic" {
		b.WriteString(" \\\n  -u " + quote(auth.Username+":"+auth.Password))
	}
	if auth := block.Auth; auth != nil && auth.Type == "bearer" && auth.Token != "" {
		b.WriteString(" \\\n  -H " + quote("Authorization: Bearer "+auth.Token))
	}
	body := block.Body.Raw
	if body == "" {
		body = block.Body.Data
	}
	if body != "" && block.Body.Type != "" && block.Body.Type != "none" {
		b.WriteString(" \\\n  -d " + quote(body))
	}
	return b.String()
}

// optionNeedsValue 是带取值的选项（只用于跳过取值，避免把值当成 URL）。
func optionNeedsValue(tok string) bool {
	short := map[byte]bool{'o': true, 'm': true, 'x': true, 'U': true, 'r': true, 'E': true, 'T': true, 'w': true}
	long := map[string]bool{
		"--connect-timeout": true, "--max-time": true, "--proxy": true, "--proxy-user": true,
		"--cacert": true, "--cert": true, "--key": true, "--output": true, "--retry": true,
		"--max-redirs": true, "--cookie-jar": true, "--interface": true, "--resolve": true,
	}
	if strings.HasPrefix(tok, "--") {
		return long[tok]
	}
	return len(tok) == 2 && short[tok[1]]
}

// pickURL 从位置参数里取第一个像 URL 的项。
func pickURL(positionals []string) string {
	for _, candidate := range positionals {
		lower := strings.ToLower(candidate)
		if strings.Contains(candidate, "://") || strings.HasPrefix(lower, "localhost") ||
			strings.HasPrefix(candidate, "/") || strings.HasPrefix(lower, "www.") {
			return candidate
		}
	}
	if len(positionals) > 0 {
		return positionals[0]
	}
	return ""
}

func pickMethod(method string, head, get, hasBody bool) string {
	if method != "" {
		return method
	}
	switch {
	case head:
		return "HEAD"
	case get:
		return "GET"
	case hasBody:
		return "POST"
	default:
		return "GET"
	}
}

// deriveName 用路径最后一段当默认请求名。
func deriveName(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return "导入的请求"
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		return parsed.Host
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func hasJSONHeader(headers []collection.KV) bool {
	for _, header := range headers {
		if strings.EqualFold(header.Name, "Content-Type") && strings.Contains(header.Value, "json") {
			return true
		}
	}
	return false
}

func isJSON(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return false
	}
	return json.Valid([]byte(trimmed))
}

func parseQuery(body string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(body, "&") {
		if pair == "" {
			continue
		}
		if key, value, ok := strings.Cut(pair, "="); ok {
			out[key] = value
		} else {
			out[pair] = ""
		}
	}
	return out
}

// quote 用单引号包起来，内部单引号按 shell 规则拼接。
func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// tokenize 按 shell 规则切词：单/双引号、反斜杠转义、反斜杠或 ^ 续行。
func tokenize(text string) ([]string, error) {
	text = strings.ReplaceAll(text, "\\\r\n", " ")
	text = strings.ReplaceAll(text, "\\\n", " ")
	text = strings.ReplaceAll(text, "^\r\n", " ")
	text = strings.ReplaceAll(text, "^\n", " ")

	tokens := make([]string, 0, 8)
	var current strings.Builder
	inToken := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch ch {
		case ' ', '\t', '\n', '\r':
			if inToken {
				tokens = append(tokens, current.String())
				current.Reset()
				inToken = false
			}
		case '\'':
			inToken = true
			end := strings.IndexByte(text[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("单引号没有闭合")
			}
			current.WriteString(text[i+1 : i+1+end])
			i += end + 1
		case '"':
			inToken = true
			i++
			closed := false
			for ; i < len(text); i++ {
				if text[i] == '\\' && i+1 < len(text) && (text[i+1] == '"' || text[i+1] == '\\') {
					current.WriteByte(text[i+1])
					i++
					continue
				}
				if text[i] == '"' {
					closed = true
					break
				}
				current.WriteByte(text[i])
			}
			if !closed {
				return nil, fmt.Errorf("双引号没有闭合")
			}
		case '\\':
			inToken = true
			if i+1 < len(text) {
				i++
				current.WriteByte(text[i])
			}
		case '$':
			// $'…' 的 ANSI-C 引号：只去掉前缀，内容按普通单引号处理。
			if i+1 < len(text) && text[i+1] == '\'' {
				i++
				inToken = true
				end := strings.IndexByte(text[i+1:], '\'')
				if end < 0 {
					return nil, fmt.Errorf("$' 引号没有闭合")
				}
				current.WriteString(text[i+1 : i+1+end])
				i += end + 1
				continue
			}
			inToken = true
			current.WriteByte(ch)
		default:
			inToken = true
			current.WriteByte(ch)
		}
	}
	if inToken {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}
