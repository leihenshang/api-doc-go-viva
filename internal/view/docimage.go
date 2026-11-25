package view

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// 文档图片（N21）：粘贴位图落盘成 <集合>/assets/<哈希前 8 位>.png，预览按文档所在目录
// 解析相对路径。Markdown 视图自己不读文件，唯一的入口是 MarkdownStyle.Image。
//
// 三条守则：
//   - 路径只认集合内的相对路径：绝对路径、带协议的、靠 .. 跳出集合的一律拒绝；
//   - 只解码 png / jpeg / gif 首帧，单张有字节与像素上限；
//   - 缓存按「路径 + mtime + 大小」，文件没了就先失效再返回缺失，预览退回 alt 文本。
const (
	docImageMaxBytes  = 16 << 20 // 单张图的文件上限
	docImageMaxPixels = 40 << 20 // 解码后的像素上限，挡解压炸弹
)

// docImageEntry 是一张已解码的图：pixels 只在 mtime 或大小变化时重解。
type docImageEntry struct {
	mod    time.Time
	size   int64
	bitmap gfx.Bitmap
}

// pasteDocClipboard 是源码区的 Ctrl/Cmd+V：剪贴板里是图就存图，否则按文本插入。
//
// 为什么不走平台的粘贴：TextPasteNoText（「粘贴但剪贴板里没有文本」）只发给
// TextInput.OnEvent，而交出 OnEvent 就得放弃 Editor，代价见 docode.go。
// 用页面快捷键在文本系统之前截住，既保住编辑器原有的输入通路，又能看剪贴板格式。
func (m *Model) pasteDocClipboard() {
	if m.docText == nil {
		return
	}
	if png, err := app.ClipboardImagePNG(); err == nil && len(png) > 0 {
		m.pasteDocPNG(png)
		return
	}
	text, err := app.ClipboardText()
	if err != nil || text == "" {
		return
	}
	m.docText.Type(text)
	m.docs.key = ""
	m.invalidator.Invalidate()
}

// pasteDocPNG 是粘贴的落盘与正文插入（与剪贴板解耦，方便无头用例直接喂字节）。
func (m *Model) pasteDocPNG(png []byte) {
	if m.col == nil || m.col.Dir == "" || m.editor.file == "" || m.docText == nil {
		m.editor.status = "粘贴图片失败：当前没有打开的文档"
		m.invalidator.Invalidate()
		return
	}
	link, err := m.storeDocImage(png, filepath.Dir(m.editor.file))
	if err != nil {
		m.editor.status = "粘贴图片失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	// 插到光标处；正文末尾不是换行就先补一个，免得图片与上一行黏在一起。
	insert := "![](" + link + ")"
	if current := m.docText.String(); current != "" && !strings.HasSuffix(current, "\n") {
		insert = "\n" + insert
	}
	m.docText.Type(insert + "\n")
	m.editor.status = "已插入图片 " + link + "（Ctrl+S 保存）"
	m.docs.key = "" // 预览按新正文重排
	m.invalidator.Invalidate()
}

// storeDocImage 按内容寻址落盘，返回相对文档目录的引用路径。
// 同一张图贴两次不会再写一个文件（哈希一样）。
func (m *Model) storeDocImage(data []byte, base string) (string, error) {
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:4]) + ".png" // 前 8 个十六进制字符
	directory := filepath.Join(m.col.Dir, "assets")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(directory, name)
	if _, err := os.Stat(path); err != nil {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
	}
	// 文档在子目录里（如 docs/）时，引用要写成 ../assets/xxx.png。
	from := filepath.Join(m.col.Dir, base)
	relative, err := filepath.Rel(from, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

// docImage 是 MarkdownStyle.Image：把 ![alt](destination) 的目标解成位图。
// 目标解析不了、文件没了、解不出来，都返回 false，让视图按 alt 文本排版。
func (m *Model) docImage(base, destination string) (gfx.Bitmap, bool) {
	path, ok := m.docImagePath(base, destination)
	if !ok {
		return gfx.Bitmap{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > docImageMaxBytes {
		delete(m.docImages, path)
		return gfx.Bitmap{}, false
	}
	if entry, ok := m.docImages[path]; ok && entry.size == info.Size() && entry.mod.Equal(info.ModTime()) {
		return entry.bitmap, true
	}
	bitmap, ok := decodeDocImage(path, info)
	if !ok {
		delete(m.docImages, path)
		return gfx.Bitmap{}, false
	}
	if m.docImages == nil {
		m.docImages = make(map[string]docImageEntry, 4)
	}
	m.docImages[path] = docImageEntry{mod: info.ModTime(), size: info.Size(), bitmap: bitmap}
	return bitmap, true
}

// docImagePath 把 Markdown 里的相对路径解析成集合内的绝对路径。
func (m *Model) docImagePath(base, destination string) (string, bool) {
	if m.col == nil || m.col.Dir == "" {
		return "", false
	}
	// 只认相对路径：外链、绝对路径、file:// 都不碰。
	if destination == "" || strings.Contains(destination, "://") || strings.HasPrefix(destination, "/") ||
		filepath.IsAbs(destination) {
		return "", false
	}
	// Markdown 允许 `![alt](path "title")`：标题不参与寻路。
	if index := strings.IndexAny(destination, " \t"); index > 0 {
		destination = destination[:index]
	}
	root, err := filepath.Abs(m.col.Dir)
	if err != nil {
		return "", false
	}
	full := filepath.Clean(filepath.Join(root, base, filepath.FromSlash(destination)))
	// 校验没有跳出集合根（Join 已经吃掉 ..，这里只判断结果在哪）。
	if full == root || !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

// decodeDocImage 解码首帧并转成画家要的 4 字节 RGBA。
func decodeDocImage(path string, info os.FileInfo) (gfx.Bitmap, bool) {
	file, err := os.Open(path)
	if err != nil {
		return gfx.Bitmap{}, false
	}
	defer file.Close()
	source, _, err := image.Decode(file) // gif 只取首帧
	if err != nil {
		return gfx.Bitmap{}, false
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 || width*height > docImageMaxPixels {
		return gfx.Bitmap{}, false
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), source, bounds.Min, draw.Src)
	return gfx.Bitmap{
		Pix: rgba.Pix, Width: width, Height: height, Stride: rgba.Stride,
		// ID 认路径、Version 认 mtime：画家据此决定是否重传像素。
		ID: docBitmapID(path), Version: uint32(info.ModTime().UnixNano()),
	}, true
}

// docBitmapID 是同一张图跨帧稳定的纹理名字。
func docBitmapID(path string) uint64 {
	sum := sha256.Sum256([]byte(path))
	return binary.LittleEndian.Uint64(sum[:8])
}

// docBase 是文档文件所在目录（相对集合根），根目录返回空串。
func docBase(file string) string {
	if file == "" {
		return ""
	}
	if directory := filepath.Dir(file); directory != "." {
		return directory
	}
	return ""
}
