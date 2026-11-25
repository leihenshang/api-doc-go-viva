package view

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/vivaui/viva/gfx"
)

// writeDocCollection 造一个只有一篇文档的集合，文档放在 docs/ 下。
func writeDocCollection(t *testing.T, content string) (dir string, entry *explorer.Entry) {
	t.Helper()
	dir = t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 图片集合\n")
	writeFile(t, filepath.Join(dir, "docs", "笔记.md"), content)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, item := range col.Entries {
		if item.IsDoc() {
			return dir, item
		}
	}
	t.Fatal("集合里没有文档")
	return "", nil
}

// testPNG 是一张纯色 PNG：宽高决定它在预览里占多大地方。
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码 PNG: %v", err)
	}
	return buf.Bytes()
}

// TestDocPasteImage 粘贴位图：内容寻址落盘、正文插一行引用、同一张图只落一个文件。
func TestDocPasteImage(t *testing.T) {
	dir, entry := writeDocCollection(t, "# 笔记\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })
	model.openEntry(entry)
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	png := testPNG(t, 120, 80)
	sum := sha256.Sum256(png)
	name := hex.EncodeToString(sum[:4]) + ".png"

	model.pasteDocPNG(png)
	pump(t, driver)

	saved := filepath.Join(dir, "assets", name)
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("图片没有落盘：%v", err)
	}
	// 文档在 docs/ 下，引用要回到集合根的 assets/。
	want := "![](../assets/" + name + ")"
	if !strings.Contains(model.editor.text, want) {
		t.Fatalf("正文里应插入 %q，实际 %q", want, model.editor.text)
	}
	if !strings.Contains(model.editor.status, name) {
		t.Fatalf("状态栏应提示插入了哪张图，实际 %q", model.editor.status)
	}

	// 同一张图再贴一次：只追加引用，不再落第二个文件。
	model.pasteDocPNG(png)
	pump(t, driver)
	files, err := os.ReadDir(filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatalf("读 assets: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("同一张图应只落一个文件，实际 %d 个", len(files))
	}
	if count := strings.Count(model.editor.text, want); count != 2 {
		t.Fatalf("第二次粘贴应再追加一行引用，实际 %d 处", count)
	}
}

// TestDocImageResolve 预览按文档目录解析相对路径，越界与外链一律拒绝。
func TestDocImageResolve(t *testing.T) {
	dir, entry := writeDocCollection(t, "# 笔记\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	writeFile(t, filepath.Join(dir, "assets", "shot.png"), string(testPNG(t, 8, 6)))
	model := New(col, "", nil)
	model.openEntry(entry)

	base := docBase(entry.File)
	if bitmap, ok := model.docImage(base, "../assets/shot.png"); !ok {
		t.Fatal("集合内的相对路径应该解得出来")
	} else if bitmap.Width != 8 || bitmap.Height != 6 || len(bitmap.Pix) != 8*6*4 {
		t.Fatalf("解码结果尺寸不对：%dx%d pix=%d", bitmap.Width, bitmap.Height, len(bitmap.Pix))
	}
	// 同一张图第二次问：走缓存，ID 不变。
	first, _ := model.docImage(base, "../assets/shot.png")
	second, _ := model.docImage(base, "../assets/shot.png")
	if first.ID == 0 || first.ID != second.ID {
		t.Fatalf("纹理 ID 应稳定：%d / %d", first.ID, second.ID)
	}

	for _, bad := range []string{
		"../../outside.png",         // 跳出集合
		"/etc/passwd",               // 绝对路径
		"https://example.com/a.png", // 外链
		"../assets/missing.png",     // 不存在
	} {
		if _, ok := model.docImage(base, bad); ok {
			t.Fatalf("%q 不该解析成图片", bad)
		}
	}

	// 文件删掉后：缓存失效，回到「没有图」。
	if err := os.Remove(filepath.Join(dir, "assets", "shot.png")); err != nil {
		t.Fatalf("删图: %v", err)
	}
	if _, ok := model.docImage(base, "../assets/shot.png"); ok {
		t.Fatal("文件删掉后应返回缺失")
	}
}

// TestDocImageInPreview 预览确实按图片排版：删掉文件后重建排版，高度退回 alt 文本。
func TestDocImageInPreview(t *testing.T) {
	png := testPNG(t, 200, 120)
	content := "# 截图\n\n![示意](../assets/shot.png)\n"
	dir, entry := writeDocCollection(t, content)
	writeFile(t, filepath.Join(dir, "assets", "shot.png"), string(png))

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })
	model.openEntry(entry)
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	withImage := model.docs.height
	if withImage <= 0 {
		t.Fatalf("预览没有完成排版：height=%0.f", withImage)
	}

	if err := os.Remove(filepath.Join(dir, "assets", "shot.png")); err != nil {
		t.Fatalf("删图: %v", err)
	}
	model.docs.key = "" // 强制重排，让 Image 钩子重新问一次
	model.invalidator.Invalidate()
	pump(t, driver)

	without := model.docs.height
	if without <= 0 {
		t.Fatalf("重排后高度异常：%0.f", without)
	}
	if without >= withImage {
		t.Fatalf("图片缺失后应退回 alt 文本、高度变小：有图 %.0f → 无图 %.0f", withImage, without)
	}
}

// TestDocPasteAutoSave 自动保存开着时，粘贴的图与正文引用都要落到集合里。
func TestDocPasteAutoSave(t *testing.T) {
	dir, entry := writeDocCollection(t, "# 笔记\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.settings.NoAutoSave = false
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })
	model.openEntry(entry)
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	png := testPNG(t, 60, 40)
	model.pasteDocPNG(png)
	pump(t, driver)
	if !model.docDirty() {
		t.Fatal("粘贴后文档应处于未保存状态")
	}

	// 假装静默期已过：只是让自动保存的判定条件成立，不真的等 2 秒。
	model.autoSaveDocAt = time.Now().Add(-time.Second)
	model.invalidator.Invalidate()
	pump(t, driver)

	sum := sha256.Sum256(png)
	name := hex.EncodeToString(sum[:4]) + ".png"
	if _, err := os.Stat(filepath.Join(dir, "assets", name)); err != nil {
		t.Fatalf("图片文件不在：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs", "笔记.md"))
	if err != nil {
		t.Fatalf("读文档: %v", err)
	}
	if !strings.Contains(string(data), "![](../assets/"+name+")") {
		t.Fatalf("自动保存没有把图片引用写回文档：%q", string(data))
	}
	if model.docDirty() {
		t.Fatal("自动保存之后不该还是未保存状态")
	}
}

// TestDocPasteShortcutImage 源码区聚焦时按 Ctrl+V：剪贴板里是图片就存进 assets 并插入引用。
// 这条走的是应用自己的粘贴通道（平台只把「粘贴」交给文本系统，应用看不到图片格式）。
func TestDocPasteShortcutImage(t *testing.T) {
	dir, entry := writeDocCollection(t, "# 笔记\n")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	t.Cleanup(func() { model.setDocSplit(false) })
	model.openEntry(entry)
	model.setDocSplit(true)
	model.invalidator.Invalidate()
	pump(t, driver)

	// 点进源码区，让它拿到焦点。
	field := hitBoundsOf(t, driver, "文档内容")
	if err := driver.Click(field.X+40, field.Y+30); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatalf("Pump: %v", err)
	}

	png := testPNG(t, 64, 48)
	if err := driver.SetClipboardImage(png); err != nil {
		t.Fatalf("设置剪贴板图片: %v", err)
	}
	if err := driver.Chord(gfx.ModifierControl, "v"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	pump(t, driver)

	sum := sha256.Sum256(png)
	name := hex.EncodeToString(sum[:4]) + ".png"
	if _, err := os.Stat(filepath.Join(dir, "assets", name)); err != nil {
		t.Fatalf("Ctrl+V 没把图片存进 assets：%v", err)
	}
	if !strings.Contains(model.editor.text, "![](../assets/"+name+")") {
		t.Fatalf("正文里没有插入引用：%q", model.editor.text)
	}

	// 剪贴板里是文本时照常插入：接管 Ctrl+V 不能把普通粘贴吞掉。
	driver.SetClipboard("补一段文字")
	if err := driver.Chord(gfx.ModifierControl, "v"); err != nil {
		t.Fatalf("Chord: %v", err)
	}
	pump(t, driver)
	pump(t, driver)
	if !strings.Contains(model.editor.text, "补一段文字") {
		t.Fatalf("文本粘贴被吞了：%q", model.editor.text)
	}
}
