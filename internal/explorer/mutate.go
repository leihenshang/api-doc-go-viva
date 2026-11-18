package explorer

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"gopkg.in/yaml.v3"
)

// trashDirName 是集合里的回收站目录（与客户端一致，隐藏目录不参与扫描）。
const trashDirName = ".trash"

// CreateRequest 在 folder（相对集合根，空=根）下新建一条 http 请求，返回相对路径。
// 默认值照抄客户端的 CreateRequest：Content-Type: application/json + 一行空参数 + 无请求体，
// 重名自动加 -2，绝不覆盖已有文件。
func CreateRequest(collectionDir, folder, name, method, url string) (string, error) {
	name = strings.TrimSpace(name)
	if !collection.ValidEntryName(name) {
		return "", fmt.Errorf("名称含非法字符或为空: %q", name)
	}
	dir, err := resolveFolder(collectionDir, folder)
	if err != nil {
		return "", err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	file := &collection.RequestFile{
		Info: collection.FileInfo{Name: name, Type: KindHTTP, Seq: nextSeq(dir)},
		Meta: collection.FileMeta{UID: newUID()},
		HTTP: collection.HTTPBlock{
			Method:  method,
			URL:     strings.TrimSpace(url),
			Params:  []collection.KV{{Enabled: true}},
			Headers: []collection.KV{{Name: "Content-Type", Value: "application/json", Enabled: true}},
			Body:    collection.Body{Type: "none"},
		},
	}
	data, err := file.Encode()
	if err != nil {
		return "", fmt.Errorf("编码请求: %w", err)
	}
	base := collection.SanitizeFileName(name)
	for i := 1; i <= 999; i++ {
		fileName := base + ".yml"
		if i > 1 {
			fileName = fmt.Sprintf("%s-%d.yml", base, i)
		}
		full := filepath.Join(dir, fileName)
		handle, createErr := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(createErr, fs.ErrExist) {
			continue
		}
		if createErr != nil {
			return "", fmt.Errorf("创建请求: %w", createErr)
		}
		_, writeErr := handle.Write(data)
		closeErr := handle.Close()
		if writeErr != nil {
			return "", fmt.Errorf("写入请求: %w", writeErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("关闭请求: %w", closeErr)
		}
		rel, relErr := filepath.Rel(collectionDir, full)
		if relErr != nil {
			return "", relErr
		}
		return filepath.ToSlash(rel), nil
	}
	return "", fmt.Errorf("同名请求过多: %s", base)
}

// RenameEntry 改显示名：请求改 info.name，文档改 frontmatter 的 name；文件名都不动。
func RenameEntry(collectionDir string, entry *Entry, name string) error {
	name = strings.TrimSpace(name)
	if !collection.ValidEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空: %q", name)
	}
	if entry.IsDoc() {
		front := setFrontName(entry.Front, entry.UID, name)
		return SaveDoc(collectionDir, entry.File, front, entry.Content)
	}
	return renameRequest(collectionDir, entry, name)
}

// renameRequest 只改请求文件的 info.name，其余字段与未知字段原样带回。
func renameRequest(collectionDir string, entry *Entry, name string) error {
	full, err := safeJoin(collectionDir, entry.File)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", entry.File, err)
	}
	file, err := collection.DecodeRequest(data)
	if err != nil {
		return fmt.Errorf("解析 %s: %w", entry.File, err)
	}
	file.Info.Name = name
	encoded, err := file.Encode()
	if err != nil {
		return fmt.Errorf("编码 %s: %w", entry.File, err)
	}
	return writeAtomic(full, encoded)
}

// SaveRequest 把编辑后的请求写回原文件（Meta/Extra/Settings 由调用方原样带上）。
func SaveRequest(collectionDir, rel string, file *collection.RequestFile) error {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return err
	}
	data, err := file.Encode()
	if err != nil {
		return fmt.Errorf("编码 %s: %w", rel, err)
	}
	return writeAtomic(full, data)
}

// TrashEntry 把条目文件移到 <集合>/.trash/<时间戳>.<文件名>，与客户端同一个回收站。
func TrashEntry(collectionDir string, entry *Entry) error {
	full, err := safeJoin(collectionDir, entry.File)
	if err != nil {
		return err
	}
	trash := filepath.Join(collectionDir, trashDirName)
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return fmt.Errorf("创建回收站: %w", err)
	}
	dst := filepath.Join(trash, collection.TrashName(filepath.Base(full), time.Now()))
	for i := 2; ; i++ {
		if _, statErr := os.Stat(dst); errors.Is(statErr, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(trash, collection.TrashName(filepath.Base(full), time.Now())+fmt.Sprintf(".%d", i))
	}
	if err := os.Rename(full, dst); err != nil {
		return fmt.Errorf("移入回收站: %w", err)
	}
	return nil
}

// resolveFolder 校验并返回要写入的目录；只允许集合内已存在的分组目录。
func resolveFolder(collectionDir, folder string) (string, error) {
	if strings.TrimSpace(folder) == "" {
		return collectionDir, nil
	}
	clean := filepath.Clean(filepath.FromSlash(folder))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("非法的分组路径: %q", folder)
	}
	dir := filepath.Join(collectionDir, clean)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("分组目录不存在: %s", folder)
	}
	return dir, nil
}

// nextSeq 取目录里下一个排序号（与客户端 nextSeq 同语义：现有最大值 +1）。
func nextSeq(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	maxSeq := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".yml") || name == "folder.yml" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		file, decodeErr := collection.DecodeRequest(data)
		if decodeErr != nil {
			continue
		}
		if file.Info.Seq > maxSeq {
			maxSeq = file.Info.Seq
		}
	}
	return maxSeq + 1
}

// setFrontName 只在 frontmatter 里替换/插入 name，其余行一个字节都不动
// （客户端可能在里面放 icon/seq/扩展字段，重写整个 frontmatter 会丢信息）。
func setFrontName(front, uid, name string) string {
	quoted := yamlScalar(name)
	if front == "" {
		return "---\nuid: " + yamlScalar(uid) + "\nname: " + quoted + "\n---\n"
	}
	lines := strings.SplitAfter(front, "\n")
	var out strings.Builder
	replaced, inserted := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !replaced && strings.HasPrefix(trimmed, "name:") {
			out.WriteString("name: " + quoted + "\n")
			replaced = true
			continue
		}
		out.WriteString(line)
		if !replaced && !inserted && strings.HasPrefix(trimmed, "uid:") {
			out.WriteString("name: " + quoted + "\n")
			inserted = true
		}
	}
	text := out.String()
	if replaced || inserted {
		return text
	}
	// 既没有 uid 也没有 name：插在开头那条 --- 之后。
	if strings.HasPrefix(text, "---\n") {
		return "---\nname: " + quoted + "\n" + text[4:]
	}
	return "---\nname: " + quoted + "\n---\n" + text
}

// yamlScalar 把文本编码成 YAML 标量（需要时自动加引号）。
func yamlScalar(value string) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		return value
	}
	return strings.TrimSuffix(string(data), "\n")
}

// newUID 生成 v4 UUID（不引第三方依赖），与客户端新建条目时的 uid 形态一致。
func newUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
