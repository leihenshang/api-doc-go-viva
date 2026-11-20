package runner

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"gopkg.in/yaml.v3"
)

// 规则存在请求文件的顶层 run: 键里（走 RequestFile.Extra 原样保留，不动共享格式）。
//
//	run:
//	  extract:
//	    - name: token
//	      from: json          # json | header | status
//	      path: $.data.token  # json 路径；header 时是头名
//	  assert:
//	    - kind: status        # status | json | header | time
//	      path: $.code        # json / header 用
//	      op: equals          # equals | contains | exists | lt
//	      want: "200"
type Rules struct {
	Extract []Extract `yaml:"extract,omitempty" json:"extract,omitempty"`
	Assert  []Assert  `yaml:"assert,omitempty" json:"assert,omitempty"`
}

// Extract 是一条变量提取规则。
type Extract struct {
	Name string `yaml:"name" json:"name"`
	From string `yaml:"from" json:"from"` // json | header | status
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
}

// Assert 是一条断言规则。
type Assert struct {
	Kind string `yaml:"kind" json:"kind"`                     // status | json | header | time
	Path string `yaml:"path,omitempty" json:"path,omitempty"` // json / header 用
	Op   string `yaml:"op" json:"op"`                         // equals | contains | exists | lt
	Want string `yaml:"want,omitempty" json:"want,omitempty"`
}

// Key 是规则在请求文件里的顶层键。
const Key = "run"

// RulesOf 从请求文件里读出规则；没有就返回空规则。
func RulesOf(file *collection.RequestFile) (Rules, error) {
	if file == nil || file.Extra == nil {
		return Rules{}, nil
	}
	raw, ok := file.Extra[Key]
	if !ok || raw == nil {
		return Rules{}, nil
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return Rules{}, fmt.Errorf("读运行规则: %w", err)
	}
	var rules Rules
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return Rules{}, fmt.Errorf("解析运行规则: %w", err)
	}
	return rules, nil
}

// Apply 把规则写回请求文件（没有规则就删掉这个键）。
func Apply(file *collection.RequestFile, rules Rules) {
	if file == nil {
		return
	}
	if len(rules.Extract) == 0 && len(rules.Assert) == 0 {
		if file.Extra != nil {
			delete(file.Extra, Key)
		}
		return
	}
	if file.Extra == nil {
		file.Extra = map[string]any{}
	}
	file.Extra[Key] = map[string]any{
		"extract": ruleMaps(rules.Extract),
		"assert":  assertMaps(rules.Assert),
	}
}

// ruleMaps / assertMaps 把规则转成可编码进 Extra 的普通结构。
func ruleMaps(rows []Extract) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{"name": row.Name, "from": row.From}
		if row.Path != "" {
			item["path"] = row.Path
		}
		out = append(out, item)
	}
	return out
}

func assertMaps(rows []Assert) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{"kind": row.Kind, "op": row.Op}
		if row.Path != "" {
			item["path"] = row.Path
		}
		if row.Want != "" {
			item["want"] = row.Want
		}
		out = append(out, item)
	}
	return out
}

// Observation 是一次响应的可断言视图。
type Observation struct {
	Status   int
	Body     string
	Headers  []send.Header
	Duration time.Duration
}

// Evaluate 跑一遍规则：返回提取到的变量与断言失败的原因（按规则顺序）。
func Evaluate(rules Rules, observed Observation) (map[string]string, []string) {
	vars := map[string]string{}
	for _, rule := range rules.Extract {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			continue
		}
		if value, ok := lookup(observed, rule.From, rule.Path); ok {
			vars[name] = value
		}
	}
	var failures []string
	for _, rule := range rules.Assert {
		failure := check(observed, rule)
		if failure != "" {
			failures = append(failures, failure)
		}
	}
	return vars, failures
}

// check 检查一条断言，通过返回空串，失败返回「期望 vs 实际」。
func check(observed Observation, rule Assert) string {
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	op := strings.ToLower(strings.TrimSpace(rule.Op))
	if op == "" {
		op = "equals"
	}
	actual, found := lookup(observed, kind, rule.Path)
	label := kind
	if rule.Path != "" {
		label += " " + rule.Path
	}
	if !found {
		if op == "exists" {
			return label + "：期望有值，实际没找到"
		}
		return label + "：找不到这个值"
	}
	switch op {
	case "exists":
		return ""
	case "equals":
		if actual != rule.Want {
			return label + "：期望 " + quote(rule.Want) + "，实际 " + quote(actual)
		}
	case "contains":
		if !strings.Contains(actual, rule.Want) {
			return label + "：期望包含 " + quote(rule.Want) + "，实际 " + quote(actual)
		}
	case "lt":
		want, err := strconv.ParseFloat(strings.TrimSpace(rule.Want), 64)
		if err != nil {
			return label + "：阈值 " + quote(rule.Want) + " 不是数字"
		}
		got, err := strconv.ParseFloat(strings.TrimSpace(actual), 64)
		if err != nil {
			return label + "：实际值 " + quote(actual) + " 不是数字"
		}
		if !(got < want) {
			return label + "：期望小于 " + quote(rule.Want) + "，实际 " + quote(actual)
		}
	default:
		return "不认识的比较方式 " + quote(rule.Op)
	}
	return ""
}

// quote 给文案加引号，空值写成「空」。
func quote(value string) string {
	if strings.TrimSpace(value) == "" {
		return "空"
	}
	return strconv.Quote(value)
}

// lookup 按来源取值：status 是状态码，header 是响应头，json 走路径，time 是耗时毫秒。
func lookup(observed Observation, source, path string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "status":
		return strconv.Itoa(observed.Status), observed.Status != 0
	case "time", "duration":
		return strconv.FormatInt(observed.Duration.Milliseconds(), 10), true
	case "header":
		want := strings.ToLower(strings.TrimSpace(path))
		for _, header := range observed.Headers {
			if strings.EqualFold(header.Name, want) {
				return header.Value, true
			}
		}
		return "", false
	case "json", "body":
		value, ok := jsonLookup(observed.Body, path)
		return value, ok
	}
	return "", false
}

// jsonLookup 按 $.a.b[0].c 这样的路径取值，标量转成字符串。
func jsonLookup(body, path string) (string, bool) {
	path = strings.TrimSpace(path)
	if strings.TrimSpace(body) == "" {
		return "", false
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", false
	}
	if path == "" || path == "$" {
		return scalar(root)
	}
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			continue
		}
		name, indices := splitIndices(segment)
		if name != "" {
			object, ok := root.(map[string]any)
			if !ok {
				return "", false
			}
			value, exists := object[name]
			if !exists {
				return "", false
			}
			root = value
		}
		for _, index := range indices {
			list, ok := root.([]any)
			if !ok || index < 0 || index >= len(list) {
				return "", false
			}
			root = list[index]
		}
	}
	return scalar(root)
}

// splitIndices 把 items[0][2] 拆成名字与下标。
func splitIndices(segment string) (string, []int) {
	name := segment
	var indices []int
	if bracket := strings.Index(segment, "["); bracket >= 0 {
		name = segment[:bracket]
		rest := segment[bracket:]
		for {
			open := strings.Index(rest, "[")
			closing := strings.Index(rest, "]")
			if open < 0 || closing < 0 || closing < open {
				break
			}
			index, err := strconv.Atoi(rest[open+1 : closing])
			if err != nil {
				break
			}
			indices = append(indices, index)
			rest = rest[closing+1:]
		}
	}
	return name, indices
}

// scalar 把 JSON 标量转成字符串（对象 / 数组用紧凑 JSON）。
func scalar(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", false
	case string:
		return typed, true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	case map[string]any, []any:
		data, err := json.Marshal(typed)
		if err != nil {
			return "", false
		}
		return string(data), true
	}
	return fmt.Sprintf("%v", value), true
}

// SortedVars 把变量表拍平成「名 = 值」的说明行，供结果展示。
func SortedVars(vars map[string]string) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, name+" = "+vars[name])
	}
	return out
}
