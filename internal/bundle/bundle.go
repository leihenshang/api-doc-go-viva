// Package bundle 是集合包（.apidocgo.json）：把整个集合打成一份 JSON，另一台机器导入即可用。
//
// 刻意做成「文件原文内嵌」而不是把请求解析成结构体再序列化：YAML 里的未知字段、
// 注释外的字节顺序都能一字不差地带回来，导入结果与手工拷贝目录等价。
package bundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// Format 是本包的文件标识；Version 用于将来做兼容分支。
const (
	Format  = "api-doc-go/collection"
	Version = 1
)

// File 是集合里的一个文件：路径相对集合根，内容原样。
type File struct {
	Path string `json:"path"`
	Body string `json:"yaml"`
}

// Bundle 是一份集合包。
type Bundle struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Name    string `json:"name"`
	Files   []File `json:"files"`
}

// Encode 把一个已打开的集合打成集合包。
//
// 只收集合树里的条目（请求与文档）以及清单；environments / assets / examples /
// .trash 不打包——前两个是每台机器各自的运行状态，后两个是回收站与示例。
func Encode(col *explorer.Collection) ([]byte, error) {
	if col == nil || col.Dir == "" {
		return nil, fmt.Errorf("没有可导出的集合")
	}
	pack := Bundle{Format: Format, Version: Version, Name: col.Name}
	paths := make([]string, 0, len(col.Entries))
	for _, entry := range col.Entries {
		if entry == nil || entry.File == "" || strings.HasPrefix(entry.File, ".") {
			continue
		}
		paths = append(paths, entry.File)
	}
	paths = append(paths, "opencollection.yml")
	sort.Strings(paths)

	seen := map[string]bool{}
	for _, rel := range paths {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		data, err := os.ReadFile(filepath.Join(col.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", rel, err)
		}
		pack.Files = append(pack.Files, File{Path: rel, Body: string(data)})
	}
	return json.MarshalIndent(pack, "", "  ")
}

// Decode 解析一份集合包；格式不对给出人话错误。
func Decode(data []byte) (*Bundle, error) {
	var pack Bundle
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("解析集合包: %w", err)
	}
	if pack.Format != Format {
		return nil, fmt.Errorf("不是集合包（format=%q）", pack.Format)
	}
	if len(pack.Files) == 0 {
		return nil, fmt.Errorf("集合包里没有文件")
	}
	return &pack, nil
}

// Write 把集合包写进目标目录：已存在的文件改名成 `-2`，绝不覆盖。
// 返回写入的文件数与改名数。
func Write(dir string, pack *Bundle) (written, renamed int, err error) {
	if pack == nil {
		return 0, 0, fmt.Errorf("没有可导入的集合包")
	}
	manifest := false
	for _, file := range pack.Files {
		if strings.TrimPrefix(filepath.ToSlash(file.Path), "./") == "opencollection.yml" {
			manifest = true
		}
	}
	if !manifest {
		return 0, 0, fmt.Errorf("集合包里缺 opencollection.yml")
	}
	for _, file := range pack.Files {
		rel := filepath.Clean(filepath.FromSlash(file.Path))
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue // 防路径穿越：包里写 ../../x 也不落盘
		}
		full := filepath.Join(dir, rel)
		if _, statErr := os.Stat(full); statErr == nil {
			full = uniquePath(full)
			renamed++
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return written, renamed, fmt.Errorf("建目录: %w", err)
		}
		if err := os.WriteFile(full, []byte(file.Body), 0o644); err != nil {
			return written, renamed, fmt.Errorf("写入 %s: %w", rel, err)
		}
		written++
	}
	return written, renamed, nil
}

// uniquePath 给重名文件找一个「-2 / -3 …」的后缀，扩展名保持在最后。
func uniquePath(full string) string {
	dir, name := filepath.Split(full)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
