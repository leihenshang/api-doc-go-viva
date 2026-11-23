package explorer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	return moveToTrash(collectionDir, full)
}

// ArrangeEntries 按 files 的顺序把同一目录里的条目 seq 重排为 1..N。
// 只动 seq：请求保留其余字段与未知字段，文档只替换 frontmatter 里的 seq 行。
func ArrangeEntries(collectionDir string, files []string) error {
	for index, rel := range files {
		if err := setEntrySeq(collectionDir, rel, index+1); err != nil {
			return err
		}
	}
	return nil
}

// setEntrySeq 改写一个条目的排序号；已经是目标值时一个字节都不写。
func setEntrySeq(collectionDir, rel string, seq int) error {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", rel, err)
	}
	if isDocFile(filepath.Base(rel)) {
		text := string(data)
		front, _, body := splitFrontmatter(text)
		next := setFrontSeq(front, seq)
		if next == front {
			return nil
		}
		return writeAtomic(full, []byte(next+body))
	}
	next, changed, err := replaceRequestSeq(data, seq)
	if err != nil {
		return fmt.Errorf("解析 %s: %w", rel, err)
	}
	if !changed {
		return nil
	}
	return writeAtomic(full, next)
}

// replaceRequestSeq 只改 info.seq 那一行的值：文件其余字节、缩进、注释与字段顺序都不动
// （整体重新编码会把 2 空格缩进改成 4 空格，diff 会淹没真正的改动）。
func replaceRequestSeq(data []byte, seq int) ([]byte, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("顶层不是映射")
	}
	root := doc.Content[0]
	var info *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "info" {
			info = root.Content[i+1]
			break
		}
	}
	if info == nil || info.Kind != yaml.MappingNode || len(info.Content) == 0 {
		return nil, false, fmt.Errorf("没有 info 段")
	}
	want := fmt.Sprintf("seq: %d", seq)
	lines := strings.SplitAfter(string(data), "\n")
	for i := 0; i+1 < len(info.Content); i += 2 {
		if info.Content[i].Value != "seq" {
			continue
		}
		value := info.Content[i+1]
		if value.Value == fmt.Sprint(seq) {
			return data, false, nil
		}
		line := value.Line - 1
		if line < 0 || line >= len(lines) {
			break
		}
		text := lines[line]
		at := strings.Index(text, "seq:")
		if at < 0 {
			break
		}
		tail := ""
		if strings.HasSuffix(text, "\n") {
			tail = "\n"
		}
		lines[line] = text[:at] + want + tail
		return []byte(strings.Join(lines, "")), true, nil
	}
	// 没有 seq 键：插在 info 段最后一行后面，缩进照抄第一个子键。
	indent := strings.Repeat(" ", info.Content[0].Column-1)
	last := 0
	for i := 0; i+1 < len(info.Content); i += 2 {
		if info.Content[i+1].Line > last {
			last = info.Content[i+1].Line
		}
	}
	if last <= 0 || last > len(lines) {
		return nil, false, fmt.Errorf("找不到 info 段的结尾")
	}
	insert := indent + want + "\n"
	lines = append(lines[:last], append([]string{insert}, lines[last:]...)...)
	return []byte(strings.Join(lines, "")), true, nil
}

// setFrontSeq 只在 frontmatter 里替换/插入 seq 行，其余行原样保留。
func setFrontSeq(front string, seq int) string {
	line := fmt.Sprintf("seq: %d\n", seq)
	if front == "" {
		return "---\n" + line + "---\n"
	}
	var out strings.Builder
	replaced := false
	for _, text := range strings.SplitAfter(front, "\n") {
		if !replaced && strings.HasPrefix(strings.TrimSpace(text), "seq:") {
			out.WriteString(line)
			replaced = true
			continue
		}
		out.WriteString(text)
	}
	text := out.String()
	if replaced {
		return text
	}
	// front 一定以 "---\n" 开头：插在它后面即可留在 frontmatter 内。
	if strings.HasPrefix(text, "---\n") {
		return "---\n" + line + text[4:]
	}
	return "---\n" + line + "---\n" + text
}

// CreateFolder 在 parent（相对集合根，空=根）下新建分组目录，返回相对路径。
// 重名自动补 -2、-3；folder.yml 带 name / type / seq 与一个新的 uid。
func CreateFolder(collectionDir, parent, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !collection.ValidEntryName(name) {
		return "", fmt.Errorf("名称含非法字符或为空: %q", name)
	}
	dir, err := resolveFolder(collectionDir, parent)
	if err != nil {
		return "", err
	}
	base := collection.SanitizeFileName(name)
	for i := 1; i <= 999; i++ {
		fileName := base
		if i > 1 {
			fileName = fmt.Sprintf("%s-%d", base, i)
		}
		full := filepath.Join(dir, fileName)
		if mkErr := os.Mkdir(full, 0o755); mkErr != nil {
			if errors.Is(mkErr, fs.ErrExist) {
				continue
			}
			return "", fmt.Errorf("创建分组: %w", mkErr)
		}
		body := fmt.Sprintf("info:\n  name: %s\n  type: folder\n  seq: %d\nmeta:\n  uid: %s\n",
			yamlScalar(name), nextFolderSeq(dir), newUID())
		if err := os.WriteFile(filepath.Join(full, "folder.yml"), []byte(body), 0o644); err != nil {
			return "", fmt.Errorf("写入分组描述: %w", err)
		}
		rel, relErr := filepath.Rel(collectionDir, full)
		if relErr != nil {
			return "", relErr
		}
		return filepath.ToSlash(rel), nil
	}
	return "", fmt.Errorf("同名分组过多: %s", base)
}

// nextFolderSeq 取目录里分组的下一个 seq（现有 folder.yml 的最大值 +1）。
func nextFolderSeq(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	maxSeq := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		folder, readErr := readFolder(filepath.Join(dir, entry.Name()))
		if readErr == nil && folder.Info.Seq > maxSeq {
			maxSeq = folder.Info.Seq
		}
	}
	return maxSeq + 1
}

// RenameFolder 只改 folder.yml 里的 info.name，目录名与其余行都不动。
func RenameFolder(collectionDir string, folder *Folder, name string) error {
	name = strings.TrimSpace(name)
	if !collection.ValidEntryName(name) {
		return fmt.Errorf("名称含非法字符或为空: %q", name)
	}
	if folder == nil {
		return fmt.Errorf("没有要重命名的分组")
	}
	dir, err := resolveFolder(collectionDir, folder.Dir)
	if err != nil {
		return err
	}
	full := filepath.Join(dir, "folder.yml")
	data, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		body := fmt.Sprintf("info:\n  name: %s\n  type: folder\n  seq: 1\nmeta:\n  uid: %s\n",
			yamlScalar(name), newUID())
		return writeAtomic(full, []byte(body))
	}
	if err != nil {
		return fmt.Errorf("读取 %s/folder.yml: %w", folder.Dir, err)
	}
	next, changed, err := replaceInfoValue(data, "name", yamlScalar(name))
	if err != nil {
		return fmt.Errorf("解析 %s/folder.yml: %w", folder.Dir, err)
	}
	if !changed {
		return nil
	}
	return writeAtomic(full, next)
}

// replaceInfoValue 只替换 info.<key> 那一行的值，文件其余字节不动；key 缺省时插在 info 段末尾。
func replaceInfoValue(data []byte, key, value string) ([]byte, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("顶层不是映射")
	}
	root := doc.Content[0]
	var info *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "info" {
			info = root.Content[i+1]
			break
		}
	}
	if info == nil || info.Kind != yaml.MappingNode || len(info.Content) == 0 {
		return nil, false, fmt.Errorf("没有 info 段")
	}
	want := key + ": " + value
	lines := strings.SplitAfter(string(data), "\n")
	for i := 0; i+1 < len(info.Content); i += 2 {
		if info.Content[i].Value != key {
			continue
		}
		valueNode := info.Content[i+1]
		if valueNode.Value == strings.Trim(value, "'\"") {
			return data, false, nil
		}
		line := valueNode.Line - 1
		if line < 0 || line >= len(lines) {
			break
		}
		text := lines[line]
		at := strings.Index(text, key+":")
		if at < 0 {
			break
		}
		tail := ""
		if strings.HasSuffix(text, "\n") {
			tail = "\n"
		}
		lines[line] = text[:at] + want + tail
		return []byte(strings.Join(lines, "")), true, nil
	}
	indent := strings.Repeat(" ", info.Content[0].Column-1)
	last := 0
	for i := 0; i+1 < len(info.Content); i += 2 {
		if info.Content[i+1].Line > last {
			last = info.Content[i+1].Line
		}
	}
	if last <= 0 || last > len(lines) {
		return nil, false, fmt.Errorf("找不到 info 段的结尾")
	}
	lines = append(lines[:last], append([]string{indent + want + "\n"}, lines[last:]...)...)
	return []byte(strings.Join(lines, "")), true, nil
}

// ArrangeFolders 按 dirs 的顺序把同一父目录下的分组 seq 重排为 1..N；
// 没有 folder.yml 的分组补一个只有 name 与 seq 的最小文件，其余文件原样保留。
func ArrangeFolders(collectionDir string, dirs []string) error {
	for index, rel := range dirs {
		if err := setFolderSeq(collectionDir, rel, index+1); err != nil {
			return err
		}
	}
	return nil
}

// setFolderSeq 改写某个分组 folder.yml 里的 info.seq。
func setFolderSeq(collectionDir, rel string, seq int) error {
	folderDir, err := resolveFolder(collectionDir, rel)
	if err != nil {
		return err
	}
	full := filepath.Join(folderDir, "folder.yml")
	data, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		name := yamlScalar(filepath.Base(filepath.FromSlash(rel)))
		body := fmt.Sprintf("info:\n  name: %s\n  seq: %d\n", name, seq)
		return writeAtomic(full, []byte(body))
	}
	if err != nil {
		return fmt.Errorf("读取 %s/folder.yml: %w", rel, err)
	}
	next, changed, err := replaceRequestSeq(data, seq)
	if err != nil {
		return fmt.Errorf("解析 %s/folder.yml: %w", rel, err)
	}
	if !changed {
		return nil
	}
	return writeAtomic(full, next)
}

// TrashFolder 把整个分组目录移进 .trash（与客户端 DeleteFolderTree 同一语义）。
func TrashFolder(collectionDir string, folder *Folder) error {
	if folder == nil {
		return fmt.Errorf("没有要删除的分组")
	}
	dir, err := resolveFolder(collectionDir, folder.Dir)
	if err != nil {
		return err
	}
	return moveToTrash(collectionDir, dir)
}

// CloneFolder 深拷贝一个分组：显示名加「副本N」，目录名唯一化；
// 顶层与新分组里的 folder.yml / 请求 mid / 文档 uid 都换新，其余文件按字节复制。
func CloneFolder(collectionDir string, folder *Folder) (string, error) {
	if folder == nil {
		return "", fmt.Errorf("没有要克隆的分组")
	}
	src, err := resolveFolder(collectionDir, folder.Dir)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(src)
	name := nextCloneName(parent, folder.Name)
	baseDir := collection.SanitizeFileName(name)
	dst := filepath.Join(parent, baseDir)
	for i := 2; ; i++ {
		if _, statErr := os.Stat(dst); errors.Is(statErr, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(parent, fmt.Sprintf("%s-%d", baseDir, i))
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return "", fmt.Errorf("创建副本目录: %w", err)
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(dst)
		}
	}()

	// 顶层分组用自己的显示名与新的 seq / uid。
	if err := writeFolderYML(dst, name, nextFolderSeq(parent), newUID()); err != nil {
		return "", err
	}
	err = filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			if err := os.Mkdir(target, 0o755); err != nil {
				return err
			}
			display, seq := filepath.Base(path), 0
			if ff, readErr := readFolder(path); readErr == nil {
				display, seq = ff.Info.Name, ff.Info.Seq
				if display == "" {
					display = filepath.Base(path)
				}
			}
			return writeFolderYML(target, display, seq, newUID())
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		switch {
		case filepath.Base(path) == "folder.yml":
			return nil // 目录自己的 folder.yml 由上面重写
		case isRequestFile(filepath.Base(path)):
			if next, changed, replaceErr := replaceMappingValue(data, "meta", "uid", newUID()); replaceErr == nil && changed {
				data = next
			}
		case isDocFile(filepath.Base(path)):
			if next, changed := replaceDocUID(string(data), newUID()); changed {
				data = []byte(next)
			}
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return "", err
	}
	failed = false
	rel, err := filepath.Rel(collectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// nextCloneName 取「名字 副本N」里第一个未被同层占用的显示名。
func nextCloneName(parent string, base string) string {
	names := map[string]bool{}
	entries, err := os.ReadDir(parent)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if folder, readErr := readFolder(filepath.Join(parent, name)); readErr == nil && folder.Info.Name != "" {
				name = folder.Info.Name
			}
			names[name] = true
		}
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s 副本%d", base, i)
		if !names[candidate] {
			return candidate
		}
	}
}

// writeFolderYML 写一个最小分组描述。
func writeFolderYML(dir, name string, seq int, uid string) error {
	body := fmt.Sprintf("info:\n  name: %s\n  type: folder\n  seq: %d\nmeta:\n  uid: %s\n",
		yamlScalar(name), seq, uid)
	if err := os.WriteFile(filepath.Join(dir, "folder.yml"), []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入分组描述: %w", err)
	}
	return nil
}

// replaceDocUID 只替换 frontmatter 里的顶层 uid 行；没有 uid 就原样返回
// （无 uid 的文档按路径哈希派生，副本路径不同，本来就不会撞）。
func replaceDocUID(text, uid string) (string, bool) {
	front, _, body := splitFrontmatter(text)
	if front == "" {
		return text, false
	}
	lines := strings.SplitAfter(front, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "uid:") {
			continue
		}
		at := strings.Index(line, "uid:")
		tail := ""
		if strings.HasSuffix(line, "\n") {
			tail = "\n"
		}
		lines[i] = line[:at] + "uid: " + uid + tail
		return strings.Join(lines, "") + body, true
	}
	return text, false
}

// replaceMappingValue 替换 <section>.<key> 那一行的值；缺这一项就原样返回。
func replaceMappingValue(data []byte, section, key, value string) ([]byte, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("顶层不是映射")
	}
	root := doc.Content[0]
	var block *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == section {
			block = root.Content[i+1]
			break
		}
	}
	if block == nil || block.Kind != yaml.MappingNode {
		return nil, false, nil
	}
	for i := 0; i+1 < len(block.Content); i += 2 {
		if block.Content[i].Value != key {
			continue
		}
		node := block.Content[i+1]
		if node.Value == value {
			return data, false, nil
		}
		lines := strings.SplitAfter(string(data), "\n")
		line := node.Line - 1
		if line < 0 || line >= len(lines) {
			return nil, false, fmt.Errorf("行号越界")
		}
		text := lines[line]
		at := strings.Index(text, key+":")
		if at < 0 {
			return nil, false, fmt.Errorf("找不到 %s 行", key)
		}
		tail := ""
		if strings.HasSuffix(text, "\n") {
			tail = "\n"
		}
		lines[line] = text[:at] + key + ": " + value + tail
		return []byte(strings.Join(lines, "")), true, nil
	}
	return nil, false, nil
}

// MoveEntryTo 跨集合移动一条内容：源目录与目标集合目录不同，文件名目标撞车时补 -2。
func MoveEntryTo(srcCollectionDir, dstCollectionDir string, entry *Entry, folder string) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("没有要移动的条目")
	}
	src, err := safeJoin(srcCollectionDir, entry.File)
	if err != nil {
		return "", err
	}
	dir, err := resolveFolder(dstCollectionDir, folder)
	if err != nil {
		return "", err
	}
	base := filepath.Base(src)
	dst := filepath.Join(dir, base)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		if _, statErr := os.Stat(dst); errors.Is(statErr, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("移动条目: %w", err)
	}
	rel, err := filepath.Rel(dstCollectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// MoveFolderTo 跨集合移动一个分组目录。
func MoveFolderTo(srcCollectionDir, dstCollectionDir string, folder *Folder, destParent string) (string, error) {
	if folder == nil {
		return "", fmt.Errorf("没有要移动的分组")
	}
	src, err := resolveFolder(srcCollectionDir, folder.Dir)
	if err != nil {
		return "", err
	}
	parent, err := resolveFolder(dstCollectionDir, destParent)
	if err != nil {
		return "", err
	}
	base := filepath.Base(src)
	dst := filepath.Join(parent, base)
	// 与客户端一致：目标下已有同名分组就报错，不悄悄改目录名（否则显示名会重复）。
	if _, statErr := os.Stat(dst); statErr == nil {
		return "", fmt.Errorf("目标分组下已存在同名分组: %s", base)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("移动分组: %w", err)
	}
	rel, err := filepath.Rel(dstCollectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// MoveFolder 把分组移到新的父分组（空串表示集合根），返回新的相对路径。
// 目标已有同名目录时补 -2/-3；不能移进自己或自己的子孙。
func MoveFolder(collectionDir string, folder *Folder, destParent string) (string, error) {
	if folder == nil {
		return "", fmt.Errorf("没有要移动的分组")
	}
	src, err := resolveFolder(collectionDir, folder.Dir)
	if err != nil {
		return "", err
	}
	parent, err := resolveFolder(collectionDir, destParent)
	if err != nil {
		return "", err
	}
	if samePath(parent, filepath.Dir(src)) {
		return folder.Dir, nil
	}
	if samePath(parent, src) || strings.HasPrefix(parent, src+string(os.PathSeparator)) {
		return "", fmt.Errorf("不能把分组移进它自己: %s", folder.Dir)
	}
	base := filepath.Base(src)
	dst := filepath.Join(parent, base)
	if _, statErr := os.Stat(dst); statErr == nil {
		return "", fmt.Errorf("目标分组下已存在同名分组: %s", base)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("移动分组: %w", err)
	}
	rel, err := filepath.Rel(collectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// MoveEntry 把条目移进目标分组（空串表示集合根），返回新的相对路径。
// 目标已有同名文件时补 -2/-3，不覆盖；源与目标相同则原样返回。
func MoveEntry(collectionDir string, entry *Entry, folder string) (string, error) {
	if entry == nil {
		return "", fmt.Errorf("没有要移动的条目")
	}
	src, err := safeJoin(collectionDir, entry.File)
	if err != nil {
		return "", err
	}
	dir, err := resolveFolder(collectionDir, folder)
	if err != nil && folder != "" {
		return "", err
	}
	if folder == "" {
		dir = collectionDir
	}
	base := filepath.Base(src)
	dst := filepath.Join(dir, base)
	if samePath(dst, src) {
		return entry.File, nil
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		if _, statErr := os.Stat(dst); errors.Is(statErr, fs.ErrNotExist) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("移动条目: %w", err)
	}
	rel, err := filepath.Rel(collectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// TrashItem 是回收站里的一个文件：File 是 .trash 下的名字，Name 是原文件名。
type TrashItem struct {
	File string
	Name string
}

// ListTrash 列出回收站文件，按时间戳倒序（文件名前缀就是时间）。
func ListTrash(collectionDir string) ([]TrashItem, error) {
	trash := filepath.Join(collectionDir, trashDirName)
	entries, err := os.ReadDir(trash)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取回收站: %w", err)
	}
	items := make([]TrashItem, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "index.") {
			continue
		}
		name := entry.Name()
		original := name
		if dot := strings.IndexByte(name, '.'); dot >= 0 && dot < len(name)-1 {
			original = name[dot+1:]
		}
		items = append(items, TrashItem{File: name, Name: original})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].File > items[j].File })
	return items, nil
}

// RestoreTrash 把回收站文件移回集合根；目标已存在时补 -恢复 后缀，不覆盖。
func RestoreTrash(collectionDir, file string) (string, error) {
	if file == "" || strings.ContainsAny(file, "/\\") || file == "." || file == ".." {
		return "", fmt.Errorf("非法的回收站文件: %q", file)
	}
	trash := filepath.Join(collectionDir, trashDirName)
	src := filepath.Join(trash, file)
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("回收站里没有 %q", file)
	}
	name := file
	if dot := strings.IndexByte(name, '.'); dot >= 0 && dot < len(name)-1 {
		name = name[dot+1:]
	}
	dst := filepath.Join(collectionDir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(dst); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		dst = filepath.Join(collectionDir, fmt.Sprintf("%s-恢复%d%s", base, i, ext))
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("恢复文件: %w", err)
	}
	rel, err := filepath.Rel(collectionDir, dst)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// PurgeTrash 彻底删除回收站里的指定文件（目录也一并删掉），不可恢复。
// 返回真正删掉的条数与第一条错误；单个失败不影响其余项。
func PurgeTrash(collectionDir string, files []string) (int, error) {
	trash := filepath.Join(collectionDir, trashDirName)
	removed := 0
	var firstErr error
	for _, file := range files {
		if file == "" || strings.ContainsAny(file, "/\\") || file == "." || file == ".." {
			continue
		}
		target := filepath.Join(trash, file)
		if err := os.RemoveAll(target); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("删除 %s: %w", file, err)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

// moveToTrash 把一个集合内的文件移进 .trash（时间戳前缀，重名再补序号）。
func moveToTrash(collectionDir, full string) error {
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

// ErrConflict 表示磁盘上的文件在打开之后被别人改过。
var ErrConflict = errors.New("文件已被其它程序修改")

// HashRequest 算一条请求文件的内容散列（用来发现外部改动）。
// 文件不存在时返回空串与 nil：新建的请求没有基线。
func HashRequest(collectionDir, rel string) (string, error) {
	full, err := safeJoin(collectionDir, rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// SaveRequestChecked 与 SaveRequest 一样写回，但先核对基线散列。
//
// baseline 为空表示没有基线（新建 / 首次保存），直接写。不一致时返回 ErrConflict，
// 由界面决定是「重新加载丢掉本地改动」还是「强行覆盖」——这里不替用户做决定，
// 也不偷偷覆盖别人刚写进去的内容。
func SaveRequestChecked(collectionDir, rel string, file *collection.RequestFile, baseline string) error {
	if baseline != "" {
		current, err := HashRequest(collectionDir, rel)
		if err != nil {
			return err
		}
		if current != "" && current != baseline {
			return ErrConflict
		}
	}
	return SaveRequest(collectionDir, rel, file)
}
