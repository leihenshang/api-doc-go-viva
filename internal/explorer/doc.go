package explorer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/leihenshang/api-doc-go-share/collection"
	"gopkg.in/yaml.v3"
)

// DefaultDocFolder 是新建文档的默认目录，与客户端的 CreateDoc 默认位一致。
const DefaultDocFolder = "docs"

// docFront 文档 frontmatter：与客户端 docFront 同口径（只认这三项，其余原样保留在 Front 里）。
type docFront struct {
	UID  string `yaml:"uid,omitempty"`
	Name string `yaml:"name,omitempty"`
	Seq  int    `yaml:"seq,omitempty"`
}

// readDocEntry 把一篇 .md 读成文档条目。
//
// 口径与客户端 parseDocFile 一致：无 frontmatter（或 frontmatter 解析失败）时全文即正文，
// name 取文件名、uid 由相对路径哈希得到；无论哪种情况，正文与原始 frontmatter 都保留，
// 保存时按原样写回，避免改写客户端维护的 uid/name/seq。
func (c *Collection) readDocEntry(path, group string) (*Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", path, err)
	}
	rel, relErr := filepath.Rel(c.Dir, path)
	if relErr != nil {
		rel = path
	}
	relSlash := filepath.ToSlash(rel)
	text := string(data)
	front, yamlPart, body := splitFrontmatter(text)
	meta := docFront{}
	if front != "" {
		if err := yaml.Unmarshal([]byte(yamlPart), &meta); err != nil {
			// 分隔线不一定是 frontmatter（README 里常见）：当作没有，全文都是正文。
			front, body = "", text
		}
	}
	name := meta.Name
	if name == "" {
		base := filepath.Base(relSlash)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	uid := meta.UID
	if uid == "" {
		sum := sha256.Sum256([]byte(relSlash))
		uid = "md-" + hex.EncodeToString(sum[:])[:16]
	}
	return &Entry{
		Name:    name,
		Group:   group,
		File:    relSlash,
		Kind:    KindDoc,
		UID:     uid,
		Seq:     meta.Seq,
		Content: body,
		Front:   front,
	}, nil
}

// splitFrontmatter 拆出 frontmatter：raw 含首尾 `---\n`（保存时原样写回），
// yamlPart 是中间那段 YAML，body 是正文。
func splitFrontmatter(text string) (raw, yamlPart, body string) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", text
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return "", "", text
	}
	cut := 4 + end + 5
	return text[:cut], text[4 : 4+end], text[cut:]
}

// CreateDoc 在集合的 docs/ 下新建一篇 Markdown，返回相对集合根的路径。
// 与客户端 CreateDoc 一致：重名自动加 -2、-3，绝不覆盖已有文件。
func CreateDoc(collectionDir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !collection.ValidEntryName(name) {
		return "", fmt.Errorf("文档名不合法: %q", name)
	}
	dir := filepath.Join(collectionDir, DefaultDocFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s 目录: %w", DefaultDocFolder, err)
	}
	base := collection.SanitizeFileName(name)
	for i := 1; i <= 999; i++ {
		fileName := base + ".md"
		if i > 1 {
			fileName = fmt.Sprintf("%s-%d.md", base, i)
		}
		full := filepath.Join(dir, fileName)
		file, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("创建文档: %w", err)
		}
		_, writeErr := file.WriteString("# " + name + "\n\n")
		closeErr := file.Close()
		if writeErr != nil {
			return "", fmt.Errorf("写入文档: %w", writeErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("关闭文档: %w", closeErr)
		}
		rel, relErr := filepath.Rel(collectionDir, full)
		if relErr != nil {
			return "", relErr
		}
		return filepath.ToSlash(rel), nil
	}
	return "", fmt.Errorf("同名文档过多: %s", base)
}

// SaveDoc 写回一篇文档：frontmatter 原样保留（没有就不写），正文换成 body。
// 先写同目录临时文件再改名，崩溃时不会留下半篇文档。
func SaveDoc(collectionDir, rel, front, body string) error {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return err
	}
	return writeAtomic(full, []byte(front+body))
}

// safeJoin 把集合内的相对路径拼成绝对路径，并拒绝越界（..、绝对路径、符号环）。
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径越界: %q", rel)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(rootAbs, clean)
	if !strings.HasPrefix(full, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径越界: %q", rel)
	}
	return full, nil
}

// writeAtomic 同目录临时文件 + rename：崩溃时要么旧文件、要么新文件。
// 权限沿用已有文件（新文件 0644），不让 0600 的临时文件把集合变成别人读不了。
func writeAtomic(full string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, statErr := os.Stat(full); statErr == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".md-*")
	if err != nil {
		return fmt.Errorf("写临时文件: %w", err)
	}
	name := tmp.Name()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("设置权限: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("写临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("落盘: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(name, full); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("替换文档: %w", err)
	}
	return nil
}
