package view

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/vivaui/viva/ui"
)

// jsonKind 是 JSON 高亮的词法类别；颜色在绘制期按主题取。
type jsonKind uint8

const (
	jsonPlain jsonKind = iota
	jsonKey
	jsonString
	jsonNumber
	jsonLiteral
	jsonPunct
)

// jsonSpan 是一段同色文本；一行由若干段拼成。
type jsonSpan struct {
	text string
	kind jsonKind
}

// jsonLine 是格式化后的一行。
type jsonLine []jsonSpan

// jsonLineText 把一行拼回纯文本（含缩进），复制某一行时用。
func jsonLineText(line jsonLine) string {
	var b strings.Builder
	for _, span := range line {
		b.WriteString(span.text)
	}
	return b.String()
}

// parseJSON 把响应体排成缩进 2 的 JSON 并切成带类别的行；不是 JSON 时返回 nil。
// 只在收到响应时算一次，绘制期只遍历，不再分配。
func parseJSON(body string) ([]jsonLine, map[int]int) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(trimmed), "", "  "); err != nil {
		return nil, nil
	}
	lines := strings.Split(out.String(), "\n")
	result := make([]jsonLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, tokenizeJSONLine(line))
	}
	return result, jsonFoldRanges(result)
}

// jsonFoldRanges 找出每个可折叠行对应的收尾行：以 { 或 [ 结尾的行开始，
// 扫到括号闭合的那一行为止。只数标点段里的括号，不碰字符串内容。
func jsonFoldRanges(lines []jsonLine) map[int]int {
	folds := make(map[int]int)
	stack := make([]int, 0, 8)
	for i, line := range lines {
		opens, closes := 0, 0
		for _, span := range line {
			if span.kind != jsonPunct {
				continue
			}
			for _, ch := range span.text {
				switch ch {
				case '{', '[':
					opens++
				case '}', ']':
					closes++
				}
			}
		}
		for k := 0; k < opens; k++ {
			stack = append(stack, i)
		}
		for k := 0; k < closes && len(stack) > 0; k++ {
			start := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if start != i {
				folds[start] = i
			}
		}
	}
	return folds
}

// tokenizeJSONLine 把一行 JSON 切成字符串 / 键 / 数字 / 字面量 / 标点。
func tokenizeJSONLine(line string) jsonLine {
	spans := make(jsonLine, 0, 6)
	for i := 0; i < len(line); {
		switch b := line[i]; {
		case b == '"':
			end := i + 1
			for end < len(line) {
				if line[end] == '\\' {
					end += 2
					continue
				}
				if line[end] == '"' {
					end++
					break
				}
				end++
			}
			kind := jsonString
			rest := strings.TrimLeft(line[end:], " \t")
			if strings.HasPrefix(rest, ":") {
				kind = jsonKey
			}
			spans = append(spans, jsonSpan{text: line[i:end], kind: kind})
			i = end
		case b == '-' || (b >= '0' && b <= '9'):
			end := i + 1
			for end < len(line) && strings.ContainsRune("0123456789+-.eE", rune(line[end])) {
				end++
			}
			spans = append(spans, jsonSpan{text: line[i:end], kind: jsonNumber})
			i = end
		case b == 't' || b == 'f' || b == 'n':
			end := i + 1
			for end < len(line) && line[end] >= 'a' && line[end] <= 'z' {
				end++
			}
			spans = append(spans, jsonSpan{text: line[i:end], kind: jsonLiteral})
			i = end
		case b == '{' || b == '}' || b == '[' || b == ']' || b == ',' || b == ':':
			spans = append(spans, jsonSpan{text: line[i : i+1], kind: jsonPunct})
			i++
		default:
			end := i + 1
			for end < len(line) && !strings.ContainsRune(`"{}[],:-0123456789tfn`, rune(line[end])) {
				end++
			}
			spans = append(spans, jsonSpan{text: line[i:end], kind: jsonPlain})
			i = end
		}
	}
	return spans
}

// jsonTint 按参考图的 --app-json-* 令牌取色（浅/暗成对）。
func (m *Model) jsonTint(kind jsonKind) ui.Color {
	light := m.isLight()
	switch kind {
	case jsonKey:
		if light {
			return ui.RGB(0x18, 0xa0, 0x58)
		}
		return ui.RGB(0x36, 0xad, 0x6a)
	case jsonString:
		if light {
			return ui.RGB(0xd4, 0x6b, 0x08)
		}
		return ui.RGB(0xf2, 0xc9, 0x7d)
	case jsonNumber:
		if light {
			return ui.RGB(0x05, 0x50, 0xae)
		}
		return ui.RGB(0x8e, 0xc5, 0xff)
	case jsonLiteral:
		if light {
			return ui.RGB(0xb2, 0x6a, 0x00)
		}
		return ui.RGB(0xf2, 0xc9, 0x7d)
	case jsonPunct, jsonPlain:
		if light {
			return ui.RGB(0x8a, 0x91, 0x99)
		}
		return ui.RGB(0x9a, 0xa0, 0xa6)
	}
	return m.theme.Foreground
}
