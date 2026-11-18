// Package explorer 把磁盘上的集合目录读成界面直接可用的条目清单。
//
// 格式定义一律复用 api-doc-go-share/collection（与 Wails 客户端、Flutter 端同一份），
// 本包不定义请求/分组的 YAML 结构体；只有文档的 frontmatter 因为共享库没有对应类型，
// 在 doc.go 里按客户端的同口径解析（uid/name/seq + 正文）。
package explorer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// ErrNeedsInit 表示目录里没有 opencollection.yml。
// 与 Wails 客户端同一条规则：缺清单一律不打开，由界面确认后再初始化，绝不静默生成。
var ErrNeedsInit = errors.New("目录不是集合：缺少 opencollection.yml")

const manifestName = "opencollection.yml"

// 条目种类：请求、gRPC 请求、Markdown 文档。
const (
	KindHTTP = "http"
	KindGRPC = "grpc"
	KindDoc  = "doc"
)

// skipDirs 不是条目的分组目录（与客户端一致：环境与图片资源各自管理）。
var skipDirs = map[string]bool{
	"environments": true,
	"assets":       true,
	"trash":        true,
	"node_modules": true,
}

// Entry 集合树里的一个条目：一条 http/gRPC 请求，或一篇 Markdown 文档。
type Entry struct {
	Name  string // 请求取 info.name，文档取 frontmatter name 或文件名
	Group string // 分组显示名（根目录为空）
	File  string // 相对集合根的路径（文档即 .md 路径）
	Kind  string // http | grpc | doc
	UID   string // 文档的 uid（frontmatter 或路径哈希）；请求为空

	Seq     int
	Method  string
	URL     string
	Summary string

	// Payload 是 http/gRPC 请求的解析结果，文档为 nil。
	Payload *collection.RequestFile
	// Content 是文档正文（不含 frontmatter），Front 是原始 frontmatter（含首尾 ---）。
	Content string
	Front   string
}

// Title 是列表行与无障碍朗读用的名字：带方法/类型，避免同名条目分不清。
func (e *Entry) Title() string {
	if e.Kind == KindDoc {
		return "MD " + e.Name
	}
	if e.Method != "" {
		return e.Method + " " + e.Name
	}
	return e.Name
}

// IsDoc 报告条目是不是 Markdown 文档。
func (e *Entry) IsDoc() bool { return e.Kind == KindDoc }

// Collection 一个已打开的集合根。
type Collection struct {
	Dir     string
	Name    string
	Entries []*Entry
	// Warnings 是读不动的文件（坏 YAML、权限等）：跳过但不让整个集合打不开。
	Warnings []string
}

// Load 读取集合清单并扫描条目。缺清单返回 ErrNeedsInit。
func Load(dir string) (*Collection, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析集合路径: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("集合目录不可用: %s", abs)
	}
	data, err := os.ReadFile(filepath.Join(abs, manifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", abs, ErrNeedsInit)
	}
	if err != nil {
		return nil, fmt.Errorf("读取集合清单: %w", err)
	}
	manifest, err := collection.DecodeManifest(data)
	if err != nil {
		return nil, fmt.Errorf("解析集合清单: %w", err)
	}
	c := &Collection{Dir: abs, Name: manifest.Info.Name}
	if c.Name == "" {
		c.Name = filepath.Base(abs)
	}
	if err := c.scan(abs, ""); err != nil {
		return nil, err
	}
	return c, nil
}

// scan 递归一个目录：先分组、再本目录的条目，与 collection.SortNodes 的方向一致。
func (c *Collection) scan(dir, group string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取目录 %s: %w", dir, err)
	}
	root := samePath(dir, c.Dir)
	var folders, files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		switch {
		case entry.IsDir():
			if !skipDirs[name] {
				folders = append(folders, name)
			}
		case isDocFile(name):
			// 子目录里的 README.md 不作为文档条目显示，与客户端同一条规则。
			if root || !strings.EqualFold(name, "README.md") {
				files = append(files, name)
			}
		case isRequestFile(name):
			files = append(files, name)
		}
	}
	for _, folder := range c.folderEntries(dir, folders) {
		if err := c.scan(folder.dir, joinGroup(group, folder.name)); err != nil {
			return err
		}
	}
	c.appendEntries(dir, group, files)
	return nil
}

// folderEntry 是一个分组的目录与显示信息：folder.yml 只读一次。
type folderEntry struct {
	dir  string
	name string
	seq  int
}

// folderEntries 读每个子目录的 folder.yml 并排序；缺省名是目录名、seq 为 0。
func (c *Collection) folderEntries(parent string, names []string) []folderEntry {
	out := make([]folderEntry, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(parent, name)
		entry := folderEntry{dir: dir, name: name}
		if folder, err := readFolder(dir); err == nil {
			entry.seq = folder.Info.Seq
			if folder.Info.Name != "" {
				entry.name = folder.Info.Name
			}
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq < out[j].seq
		}
		return out[i].name < out[j].name
	})
	return out
}

// appendEntries 解析本目录的条目，按 seq 再按名称排序后追加（文档与请求混排）。
func (c *Collection) appendEntries(dir, group string, files []string) {
	local := make([]*Entry, 0, len(files))
	for _, name := range files {
		entry, err := c.readEntry(filepath.Join(dir, name), group)
		if err != nil {
			c.Warnings = append(c.Warnings, err.Error())
			continue
		}
		if entry != nil {
			local = append(local, entry)
		}
	}
	sort.SliceStable(local, func(i, j int) bool {
		if local[i].Seq != local[j].Seq {
			return local[i].Seq < local[j].Seq
		}
		return local[i].Name < local[j].Name
	})
	c.Entries = append(c.Entries, local...)
}

// readEntry 按扩展名分派：.md 走文档，其余按请求文件解析。
func (c *Collection) readEntry(path, group string) (*Entry, error) {
	if isDocFile(path) {
		return c.readDocEntry(path, group)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", path, err)
	}
	file, err := collection.DecodeRequest(data)
	if err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	kind := file.Info.Type
	if kind == "" {
		kind = KindHTTP
	}
	if kind != KindHTTP && kind != KindGRPC {
		return nil, nil
	}
	rel, relErr := filepath.Rel(c.Dir, path)
	if relErr != nil {
		rel = path
	}
	name := file.Info.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	entry := &Entry{
		Name:    name,
		Group:   group,
		File:    filepath.ToSlash(rel),
		Kind:    kind,
		Seq:     file.Info.Seq,
		Summary: file.Summary,
		Payload: file,
	}
	if kind == KindHTTP {
		entry.Method = strings.ToUpper(file.HTTP.Method)
		entry.URL = file.HTTP.URL
	}
	return entry, nil
}

// isRequestFile 只认请求/分组 YAML；清单、环境密文都不算。
func isRequestFile(name string) bool {
	if strings.HasSuffix(name, ".secrets.yml") {
		return false
	}
	if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
		return false
	}
	switch name {
	case manifestName, "folder.yml":
		return false
	}
	return true
}

// isDocFile 判断是不是 Markdown 文档（不区分大小写扩展名）。
func isDocFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".md")
}

// samePath 比较两个目录是否同一个（都已绝对化）。
func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func readFolder(dir string) (*collection.FolderFile, error) {
	data, err := os.ReadFile(filepath.Join(dir, "folder.yml"))
	if err != nil {
		return nil, err
	}
	return collection.DecodeFolder(data)
}

func joinGroup(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + " / " + name
}

// LoadEnv 读取 environments/<name>.yml 与同名 .secrets.yml 的启用变量。
// name 为空表示不用环境；名字先过 collection.ValidEnvName，挡住路径穿越。
func LoadEnv(dir, name string) (map[string]string, error) {
	vars := map[string]string{}
	if name == "" {
		return vars, nil
	}
	if !collection.ValidEnvName(name) {
		return nil, fmt.Errorf("环境名 %q 不合法", name)
	}
	base := filepath.Join(dir, "environments", name)
	data, err := os.ReadFile(base + ".yml")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("环境 %q 不存在（缺 environments/%s.yml）", name, name)
		}
		return nil, fmt.Errorf("读取环境 %s: %w", name, err)
	}
	env, err := collection.DecodeEnv(data)
	if err != nil {
		return nil, fmt.Errorf("解析环境 %s: %w", name, err)
	}
	for _, v := range env.Vars {
		if v.Enabled {
			vars[v.Name] = v.Value
		}
	}
	if secrets, err := os.ReadFile(base + ".secrets.yml"); err == nil {
		values, decodeErr := collection.DecodeSecrets(secrets)
		if decodeErr != nil {
			return nil, fmt.Errorf("解析环境密文 %s: %w", name, decodeErr)
		}
		for key, value := range values {
			vars[key] = value
		}
	}
	return vars, nil
}
