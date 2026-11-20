package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// 工具分组与读写标记：界面上的「工具组」勾选与「只读」开关都按这两个字段过滤。
const (
	GroupCollection = "collection" // 集合与请求（读）
	GroupDocs       = "docs"       // 文档
	GroupEnv        = "env"        // 环境变量
	GroupExample    = "example"    // 响应示例
	GroupNetwork    = "network"    // 真实发送
	GroupWrite      = "write"      // 写工具：改磁盘
)

// Groups 是界面里可勾选的分组（顺序固定，README 与设置面板都用它）。
var Groups = []string{GroupCollection, GroupDocs, GroupEnv, GroupExample, GroupNetwork, GroupWrite}

// 以下是新增工具的入参：字段名即 MCP 的工具参数名。

// EnvArgs 选一个环境（不填就是全部）。
type EnvArgs struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"集合别名（省略用第一个）"`
	Env       string `json:"env,omitempty" jsonschema:"环境名；省略则列出全部环境"`
}

// SetEnvArgs 写一个环境变量。
type SetEnvArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Env       string `json:"env" jsonschema:"环境名（不存在则新建）"`
	Name      string `json:"name" jsonschema:"变量名"`
	Value     string `json:"value" jsonschema:"变量值"`
	Secret    bool   `json:"secret,omitempty" jsonschema:"写成敏感值（落 *.secrets.yml）"`
	Delete    bool   `json:"delete,omitempty" jsonschema:"为真时删除这个变量"`
}

// ExampleArgs 列出一条请求的响应示例。
type ExampleArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path" jsonschema:"请求的相对路径（list_requests 给的 path）"`
}

// SaveRequestArgs 改一条已有请求（只给要改的字段，其余保持原样）。
type SaveRequestArgs struct {
	Workspace string            `json:"workspace,omitempty"`
	Path      string            `json:"path" jsonschema:"请求的相对路径"`
	Name      string            `json:"name,omitempty" jsonschema:"新名称（留空不改）"`
	Method    string            `json:"method,omitempty" jsonschema:"HTTP 方法"`
	URL       string            `json:"url,omitempty" jsonschema:"请求地址"`
	Headers   map[string]string `json:"headers,omitempty" jsonschema:"要设置的请求头（同名覆盖）"`
}

// CreateRequestArgs 新建一条请求。
type CreateRequestArgs struct {
	Workspace string            `json:"workspace,omitempty"`
	Folder    string            `json:"folder,omitempty" jsonschema:"目标分组（相对路径，留空落集合根）"`
	Name      string            `json:"name" jsonschema:"请求名"`
	Method    string            `json:"method" jsonschema:"HTTP 方法"`
	URL       string            `json:"url" jsonschema:"请求地址"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// ListEnvs 列出环境与变量（敏感值只给掩码）。
func (s *Service) ListEnvs(_ context.Context, in EnvArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	names, err := explorer.ListEnvs(ws.Dir)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "这个集合还没有环境。", nil
	}
	filter := strings.TrimSpace(in.Env)
	var b strings.Builder
	for _, name := range names {
		if filter != "" && name != filter {
			continue
		}
		vars, loadErr := explorer.LoadEnvVars(ws.Dir, name)
		if loadErr != nil {
			continue
		}
		fmt.Fprintf(&b, "环境 %s（%d 个变量）\n", name, len(vars))
		for _, v := range vars {
			value := v.Value
			if v.Secret {
				value = "••••"
			}
			fmt.Fprintf(&b, "- %s = %s\n", v.Name, value)
		}
	}
	if b.Len() == 0 {
		return "没有找到环境 " + filter, nil
	}
	return b.String(), nil
}

// SetEnvVar 写 / 删一个环境变量。
func (s *Service) SetEnvVar(_ context.Context, in SetEnvArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	env, name := strings.TrimSpace(in.Env), strings.TrimSpace(in.Name)
	if env == "" || name == "" {
		return "", fmt.Errorf("env 与 name 都不能为空")
	}
	vars, _ := explorer.LoadEnvVars(ws.Dir, env)
	updated := make([]collection.EnvVar, 0, len(vars)+1)
	replaced := false
	for _, v := range vars {
		if v.Name != name {
			updated = append(updated, v)
			continue
		}
		replaced = true
		if in.Delete {
			continue
		}
		v.Value, v.Secret = in.Value, in.Secret
		updated = append(updated, v)
	}
	if !replaced && !in.Delete {
		updated = append(updated, collection.EnvVar{Name: name, Value: in.Value, Secret: in.Secret})
	}
	if err := explorer.SaveEnvAll(ws.Dir, env, updated); err != nil {
		return "", err
	}
	if in.Delete {
		return fmt.Sprintf("已删除环境 %s 里的变量 %s", env, name), nil
	}
	return fmt.Sprintf("已写入环境 %s：%s", env, name), nil
}

// ListExamples 列出一条请求保存过的响应示例。
func (s *Service) ListExamples(_ context.Context, in ExampleArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	examples := explorer.Examples(ws.Dir, in.Path)
	if len(examples) == 0 {
		return "这条请求还没有保存过响应示例。", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s 有 %d 份响应示例：\n", in.Path, len(examples))
	for _, example := range examples {
		fmt.Fprintf(&b, "- %s：%d，耗时 %d ms，%d 字节\n",
			example.Name, example.Status, example.DurationMS, example.Size)
	}
	return b.String(), nil
}

// SaveRequest 改一条已有请求：只动入参里给了的字段。
func (s *Service) SaveRequest(_ context.Context, in SaveRequestArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	entry := findRequest(ws.Dir, in.Path)
	if entry == nil || entry.Payload == nil {
		return "", fmt.Errorf("没有找到请求 %q", in.Path)
	}
	file := entry.Payload
	if name := strings.TrimSpace(in.Name); name != "" {
		file.Info.Name = name
	}
	if method := strings.TrimSpace(in.Method); method != "" {
		file.HTTP.Method = strings.ToUpper(method)
	}
	if url := strings.TrimSpace(in.URL); url != "" {
		file.HTTP.URL = url
	}
	for key, value := range in.Headers {
		setKV(&file.HTTP.Headers, key, value)
	}
	if err := explorer.SaveRequest(ws.Dir, entry.File, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新请求 %s", entry.File), nil
}

// CreateRequest 新建一条请求。
func (s *Service) CreateRequest(_ context.Context, in CreateRequestArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = "GET"
	}
	rel, err := explorer.CreateRequest(ws.Dir, strings.TrimSpace(in.Folder), name, method, strings.TrimSpace(in.URL))
	if err != nil {
		return "", err
	}
	if len(in.Headers) > 0 {
		if entry := findRequest(ws.Dir, rel); entry != nil && entry.Payload != nil {
			file := entry.Payload
			for key, value := range in.Headers {
				setKV(&file.HTTP.Headers, key, value)
			}
			_ = explorer.SaveRequest(ws.Dir, rel, file)
		}
	}
	return "已新建请求 " + rel, nil
}

// findRequest 按相对路径在集合里找条目。
func findRequest(dir, rel string) *explorer.Entry {
	col, err := explorer.Load(dir)
	if err != nil {
		return nil
	}
	for _, entry := range col.Entries {
		if entry.File == rel {
			return entry
		}
	}
	return nil
}

// setKV 在键值表里写一项：同名改值，没有就追加。
func setKV(rows *[]collection.KV, name, value string) {
	for i := range *rows {
		if (*rows)[i].Name == name {
			(*rows)[i].Value = value
			(*rows)[i].Enabled = true
			return
		}
	}
	*rows = append(*rows, collection.KV{Name: name, Value: value, Enabled: true})
}

// toolCall 包一层：审计记录（工具名 / 是否写 / 用时 / 结果）。
func (a *Audit) wrap(tool string, write bool, run func() (string, error)) (string, error) {
	started := time.Now()
	text, err := run()
	a.Record(tool, write, nil, started, err)
	return text, err
}
