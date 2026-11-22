package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// 这一批是「补齐写侧与检索」的工具：搜索、文档增删改、分组增删改名、
// 请求删除、响应示例保存、环境增删改名。
//
// 统一约定：所有会动磁盘的操作用软删除（进回收站），不做静默永久删除；
// 返回文本里写清落在哪，方便 AI 与人事后确认。

// SearchArgs 是 search_requests 的入参。
type SearchArgs struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"集合别名（省略用第一个）"`
	Query     string `json:"query" jsonschema:"关键词：匹配名称、路径、方法与地址"`
	Limit     int    `json:"limit,omitempty" jsonschema:"最多返回多少条（默认 20）"`
}

// DocsArgs 是 list_docs 的入参。
type DocsArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Folder    string `json:"folder,omitempty" jsonschema:"只看某个分组（留空看全部）"`
}

// DeleteRequestArgs 删一条请求（进回收站）。
type DeleteRequestArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path" jsonschema:"请求的相对路径"`
}

// DeleteRequestsArgs 批量删请求（进回收站）。
type DeleteRequestsArgs struct {
	Workspace string   `json:"workspace,omitempty"`
	Paths     []string `json:"paths" jsonschema:"要删的请求相对路径列表"`
}

// GroupArgs 分组操作：CreateGroupArgs / RenameGroupArgs / DeleteGroupArgs 共用一部分字段。
type CreateGroupArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Parent    string `json:"parent,omitempty" jsonschema:"父分组（留空落集合根）"`
	Name      string `json:"name" jsonschema:"新分组名"`
}

// RenameGroupArgs 给分组改名。
type RenameGroupArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Folder    string `json:"folder" jsonschema:"分组的相对路径"`
	Name      string `json:"name" jsonschema:"新名称"`
}

// DeleteGroupArgs 删一个分组（连同里面的内容，进回收站）。
type DeleteGroupArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Folder    string `json:"folder" jsonschema:"分组的相对路径"`
}

// CreateDocArgs 新建一篇文档。
type CreateDocArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Name      string `json:"name" jsonschema:"文档名（落 docs/ 目录）"`
	Content   string `json:"content,omitempty" jsonschema:"正文（Markdown，留空则只写标题）"`
}

// UpdateDocArgs 改一篇文档的正文。
type UpdateDocArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path" jsonschema:"文档的相对路径"`
	Content   string `json:"content" jsonschema:"新正文（整篇替换）"`
	Append    bool   `json:"append,omitempty" jsonschema:"为真时追加到末尾而不是整篇替换"`
}

// DeleteDocArgs 删一篇文档（进回收站）。
type DeleteDocArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path" jsonschema:"文档的相对路径"`
}

// SaveExampleArgs 手工存一份响应示例。
type SaveExampleArgs struct {
	Workspace  string            `json:"workspace,omitempty"`
	Path       string            `json:"path" jsonschema:"挂在哪条请求上（请求相对路径）"`
	Name       string            `json:"name,omitempty" jsonschema:"示例名（留空按时间生成）"`
	Status     int               `json:"status,omitempty" jsonschema:"状态码（默认 200）"`
	Body       string            `json:"body,omitempty" jsonschema:"响应正文"`
	DurationMS int64             `json:"duration_ms,omitempty" jsonschema:"耗时（毫秒）"`
	Headers    map[string]string `json:"headers,omitempty" jsonschema:"响应头"`
}

// DeleteExampleArgs 删一份响应示例。
type DeleteExampleArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path" jsonschema:"请求的相对路径"`
	Name      string `json:"name" jsonschema:"示例名（或示例文件路径）"`
}

// CreateEnvArgs 新建环境。
type CreateEnvArgs struct {
	Workspace string            `json:"workspace,omitempty"`
	Name      string            `json:"name" jsonschema:"环境名"`
	Vars      map[string]string `json:"vars,omitempty" jsonschema:"初始变量（都按普通变量写）"`
}

// RenameEnvArgs 给环境改名。
type RenameEnvArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Old       string `json:"old" jsonschema:"原环境名"`
	New       string `json:"new" jsonschema:"新环境名"`
}

// DeleteEnvArgs 删一个环境。
type DeleteEnvArgs struct {
	Workspace string `json:"workspace,omitempty"`
	Name      string `json:"name" jsonschema:"环境名"`
}

// SearchRequests 按关键词在名称 / 路径 / 方法 / 地址里找。
func (s *Service) SearchRequests(_ context.Context, in SearchArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(in.Query))
	if needle == "" {
		return "", fmt.Errorf("query 不能为空")
	}
	col, err := explorer.Load(ws.Dir)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	var b strings.Builder
	hits := 0
	for _, entry := range col.Entries {
		haystack := strings.ToLower(entry.Name + " " + entry.File + " " + entry.Method + " " + entry.URL)
		if !strings.Contains(haystack, needle) {
			continue
		}
		hits++
		if hits > limit {
			continue
		}
		fmt.Fprintf(&b, "- [%s] %s（%s）%s\n", entry.Kind, entry.Name, entry.File, entry.Method+" "+entry.URL)
	}
	if hits == 0 {
		return "没有匹配「" + in.Query + "」的条目。", nil
	}
	if hits > limit {
		fmt.Fprintf(&b, "……共 %d 条，只列了前 %d 条\n", hits, limit)
	}
	return b.String(), nil
}

// ListDocs 列出集合里的文档。
func (s *Service) ListDocs(_ context.Context, in DocsArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	col, err := explorer.Load(ws.Dir)
	if err != nil {
		return "", err
	}
	folder := strings.Trim(strings.TrimSpace(in.Folder), "/")
	var b strings.Builder
	count := 0
	for _, entry := range col.Entries {
		if !entry.IsDoc() {
			continue
		}
		if folder != "" && !strings.HasPrefix(entry.File, folder+"/") {
			continue
		}
		count++
		fmt.Fprintf(&b, "- %s（%s）\n", entry.Name, entry.File)
	}
	if count == 0 {
		return "没有文档。", nil
	}
	fmt.Fprintf(&b, "共 %d 篇\n", count)
	return b.String(), nil
}

// DeleteRequest 把一条请求移进回收站。
func (s *Service) DeleteRequest(_ context.Context, in DeleteRequestArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	entry := findRequest(ws.Dir, in.Path)
	if entry == nil {
		return "", fmt.Errorf("没有找到请求 %q", in.Path)
	}
	if err := explorer.TrashEntry(ws.Dir, entry); err != nil {
		return "", err
	}
	return "已移入回收站：" + entry.File, nil
}

// DeleteRequests 批量把请求移进回收站。
func (s *Service) DeleteRequests(_ context.Context, in DeleteRequestsArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	if len(in.Paths) == 0 {
		return "", fmt.Errorf("paths 不能为空")
	}
	var done, missing []string
	for _, path := range in.Paths {
		entry := findRequest(ws.Dir, path)
		if entry == nil {
			missing = append(missing, path)
			continue
		}
		if err := explorer.TrashEntry(ws.Dir, entry); err != nil {
			missing = append(missing, path+"（"+err.Error()+"）")
			continue
		}
		done = append(done, entry.File)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已移入回收站 %d 条\n", len(done))
	for _, item := range done {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "没处理 %d 条：%s\n", len(missing), strings.Join(missing, "、"))
	}
	return b.String(), nil
}

// CreateGroup 新建一个分组。
func (s *Service) CreateGroup(_ context.Context, in CreateGroupArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	rel, err := explorer.CreateFolder(ws.Dir, strings.TrimSpace(in.Parent), name)
	if err != nil {
		return "", err
	}
	return "已新建分组 " + rel, nil
}

// RenameGroup 给分组改名。
func (s *Service) RenameGroup(_ context.Context, in RenameGroupArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	folder := findFolder(ws.Dir, in.Folder)
	if folder == nil {
		return "", fmt.Errorf("没有找到分组 %q", in.Folder)
	}
	if err := explorer.RenameFolder(ws.Dir, folder, name); err != nil {
		return "", err
	}
	return "已把分组 " + folder.Dir + " 改名为 " + name, nil
}

// DeleteGroup 把分组连同里面的内容移进回收站。
func (s *Service) DeleteGroup(_ context.Context, in DeleteGroupArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	folder := findFolder(ws.Dir, in.Folder)
	if folder == nil {
		return "", fmt.Errorf("没有找到分组 %q", in.Folder)
	}
	if err := explorer.TrashFolder(ws.Dir, folder); err != nil {
		return "", err
	}
	return "已把分组 " + folder.Name + "（含内容）移入回收站", nil
}

// CreateDoc 新建一篇文档，可同时写正文。
func (s *Service) CreateDoc(_ context.Context, in CreateDocArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	rel, err := explorer.CreateDoc(ws.Dir, name)
	if err != nil {
		return "", err
	}
	if body := strings.TrimSpace(in.Content); body != "" {
		if err := explorer.SaveDoc(ws.Dir, rel, "", body+"\n"); err != nil {
			return "", err
		}
	}
	return "已新建文档 " + rel, nil
}

// UpdateDoc 改文档正文：整篇替换或追加，frontmatter 原样保留。
func (s *Service) UpdateDoc(_ context.Context, in UpdateDocArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	entry := findDoc(ws.Dir, in.Path)
	if entry == nil {
		return "", fmt.Errorf("没有找到文档 %q", in.Path)
	}
	body := in.Content
	if in.Append {
		body = strings.TrimRight(entry.Content, "\n") + "\n\n" + strings.TrimLeft(in.Content, "\n")
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := explorer.SaveDoc(ws.Dir, entry.File, entry.Front, body); err != nil {
		return "", err
	}
	verb := "已整篇替换"
	if in.Append {
		verb = "已追加到末尾"
	}
	return verb + "：" + entry.File + "（" + strconv.Itoa(len([]rune(body))) + " 字）", nil
}

// DeleteDoc 把文档移进回收站。
func (s *Service) DeleteDoc(_ context.Context, in DeleteDocArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	entry := findDoc(ws.Dir, in.Path)
	if entry == nil {
		return "", fmt.Errorf("没有找到文档 %q", in.Path)
	}
	if err := explorer.TrashEntry(ws.Dir, entry); err != nil {
		return "", err
	}
	return "已移入回收站：" + entry.File, nil
}

// SaveExample 手工存一份响应示例（AI 可以先 send_request 再把它存下来）。
func (s *Service) SaveExample(_ context.Context, in SaveExampleArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	request := findRequest(ws.Dir, in.Path)
	if request == nil {
		return "", fmt.Errorf("没有找到请求 %q", in.Path)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = time.Now().Format("2006-01-02 15:04:05")
	}
	status := in.Status
	if status == 0 {
		status = 200
	}
	example := explorer.Example{
		Name:       name,
		Method:     request.Method,
		URL:        request.URL,
		Status:     status,
		DurationMS: in.DurationMS,
		Size:       len(in.Body),
		Body:       in.Body,
	}
	for key, value := range in.Headers {
		example.Headers = append(example.Headers, collection.KV{Name: key, Value: value, Enabled: true})
	}
	sort.Slice(example.Headers, func(i, j int) bool { return example.Headers[i].Name < example.Headers[j].Name })
	rel, err := explorer.SaveExample(ws.Dir, request.File, example)
	if err != nil {
		return "", err
	}
	return "已保存示例 " + rel, nil
}

// DeleteExample 删一份响应示例。
func (s *Service) DeleteExample(_ context.Context, in DeleteExampleArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	examples := explorer.Examples(ws.Dir, in.Path)
	if len(examples) == 0 {
		return "", fmt.Errorf("这条请求没有保存过示例")
	}
	name := strings.TrimSpace(in.Name)
	for _, example := range examples {
		if example.Name == name || example.File == name {
			if err := explorer.DeleteExample(ws.Dir, example.File); err != nil {
				return "", err
			}
			return "已删除示例 " + example.File, nil
		}
	}
	names := make([]string, 0, len(examples))
	for _, example := range examples {
		names = append(names, example.Name)
	}
	return "", fmt.Errorf("没有找到示例 %q，现有：%s", name, strings.Join(names, "、"))
}

// CreateEnv 新建环境（可带初始变量）。
func (s *Service) CreateEnv(_ context.Context, in CreateEnvArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	if err := explorer.CreateEnv(ws.Dir, name); err != nil {
		return "", err
	}
	if len(in.Vars) > 0 {
		names := make([]string, 0, len(in.Vars))
		for key := range in.Vars {
			names = append(names, key)
		}
		sort.Strings(names)
		vars := make([]collection.EnvVar, 0, len(names))
		for _, key := range names {
			vars = append(vars, collection.EnvVar{Name: key, Value: in.Vars[key]})
		}
		if err := explorer.SaveEnvAll(ws.Dir, name, vars); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("已新建环境 %s（%d 个变量）", name, len(in.Vars)), nil
}

// RenameEnv 给环境改名。
func (s *Service) RenameEnv(_ context.Context, in RenameEnvArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	oldName, newName := strings.TrimSpace(in.Old), strings.TrimSpace(in.New)
	if oldName == "" || newName == "" {
		return "", fmt.Errorf("old 与 new 都不能为空")
	}
	if err := explorer.RenameEnv(ws.Dir, oldName, newName); err != nil {
		return "", err
	}
	return "已把环境 " + oldName + " 改名为 " + newName, nil
}

// DeleteEnv 删一个环境。
func (s *Service) DeleteEnv(_ context.Context, in DeleteEnvArgs) (string, error) {
	ws, err := s.pick(in.Workspace)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	if err := explorer.DeleteEnv(ws.Dir, name); err != nil {
		return "", err
	}
	return "已删除环境 " + name, nil
}

// findFolder 按相对路径找分组；路径对不上再按显示名找一次。
//
// 为什么两种都认：分组名存在 folder.yml 里，改名后目录名不变——
// 调用方刚被告诉「已改名为 交易」，下一个请求就该能用「交易」定位。
func findFolder(dir, rel string) *explorer.Folder {
	col, err := explorer.Load(dir)
	if err != nil {
		return nil
	}
	want := strings.Trim(strings.TrimSpace(rel), "/")
	for _, folder := range col.Folders {
		if strings.Trim(folder.Dir, "/") == want {
			return folder
		}
	}
	for _, folder := range col.Folders {
		if strings.TrimSpace(folder.Name) == want {
			return folder
		}
	}
	return nil
}

// findDoc 按相对路径找文档条目。
func findDoc(dir, rel string) *explorer.Entry {
	col, err := explorer.Load(dir)
	if err != nil {
		return nil
	}
	want := strings.Trim(strings.TrimSpace(rel), "/")
	for _, entry := range col.Entries {
		if entry.IsDoc() && strings.Trim(entry.File, "/") == want {
			return entry
		}
	}
	return nil
}
