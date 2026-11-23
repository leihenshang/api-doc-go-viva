package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/drafts"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// requestFile 读回集合里某条请求的磁盘内容（顺带返回原文，用来断言 settings 段的存在与否）。
func requestFile(t *testing.T, dir, rel string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读 %s: %v", rel, err)
	}
	return string(raw), rel
}

// TestReqSettingsRoundTrip 请求级设置：填了就写进 settings 段，清空就整段删掉。
func TestReqSettingsRoundTrip(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openNamed(t, model, "健康检查")

	model.openReqSettings()
	if !model.ui.reqSetOpen {
		t.Fatal("应打开请求设置")
	}
	model.ui.reqSetTimeout = "7"
	model.ui.reqSetMaxRedirect = "3"
	model.ui.reqSetFollow = triOff
	model.ui.reqSetInsecure = triOn
	model.ui.reqSetEncode = triDefault
	model.saveReqSettings()
	if model.ui.reqSetErr != "" {
		t.Fatalf("保存报错：%s", model.ui.reqSetErr)
	}

	reloaded, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	var settings *explorer.Entry
	for _, entry := range reloaded.Entries {
		if strings.Contains(entry.Name, "健康检查") {
			settings = entry
		}
	}
	if settings == nil || settings.Payload == nil || settings.Payload.Settings == nil {
		t.Fatalf("settings 没落盘：%+v", settings)
	}
	got := settings.Payload.Settings
	if got.TimeoutSec != 7 || got.MaxRedirects != 3 {
		t.Fatalf("数值不对：%+v", got)
	}
	if got.FollowRedirects == nil || *got.FollowRedirects {
		t.Fatalf("跟随重定向应显式关掉：%+v", got.FollowRedirects)
	}
	if got.InsecureSSL == nil || !*got.InsecureSSL {
		t.Fatalf("跳过校验应显式打开：%+v", got.InsecureSSL)
	}
	if got.EncodeURL != nil {
		t.Fatalf("「跟随全局」不该写字段：%+v", got.EncodeURL)
	}

	// 清空所有项 → 整段 settings 消失，别在文件里留空壳。
	openNamed(t, model, "健康检查")
	model.openReqSettings()
	if model.ui.reqSetTimeout != "7" || model.ui.reqSetFollow != triOff {
		t.Fatalf("重新打开应回填：%+v", model.ui)
	}
	model.ui.reqSetTimeout = ""
	model.ui.reqSetMaxRedirect = ""
	model.ui.reqSetFollow = triDefault
	model.ui.reqSetInsecure = triDefault
	model.saveReqSettings()
	raw, _ := requestFile(t, dir, settings.File)
	if strings.Contains(raw, "settings:") {
		t.Fatalf("全默认时应删掉 settings 段：\n%s", raw)
	}
}

// TestReqSettingsRejectsBadInput 超时填错要报错且不落盘。
func TestReqSettingsRejectsBadInput(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openNamed(t, model, "健康检查")
	model.openReqSettings()
	model.ui.reqSetTimeout = "9999"
	model.saveReqSettings()
	if model.ui.reqSetErr == "" || !strings.Contains(model.ui.reqSetErr, "超时") {
		t.Fatalf("应提示超时越界：%q", model.ui.reqSetErr)
	}
	if !model.ui.reqSetOpen {
		t.Fatal("出错时不该关弹窗")
	}
	raw, _ := requestFile(t, dir, "健康检查.yml")
	if strings.Contains(raw, "settings:") {
		t.Fatalf("出错不该落盘：\n%s", raw)
	}
}

// TestAutoSaveWritesFile 自动保存开着：停顿到点后直接写回集合文件。
func TestAutoSaveWritesFile(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	openNamed(t, model, "健康检查")

	model.req.url = "http://127.0.0.1:1/changed"
	model.markEdited()
	if !model.autoSavePending {
		t.Fatal("编辑后应挂上待保存标记")
	}
	// 把到点时间拨到过去，模拟「停手超过间隔」。
	model.autoSaveAt = time.Now().Add(-time.Second)
	model.pumpAutoSave()

	raw, _ := requestFile(t, dir, "健康检查.yml")
	if !strings.Contains(raw, "/changed") {
		t.Fatalf("自动保存没写盘：\n%s", raw)
	}
	if model.req.dirty {
		t.Fatal("写盘后不该还是脏的")
	}
	if !strings.Contains(model.req.note, "已自动保存") {
		t.Fatalf("应提示已自动保存：%q", model.req.note)
	}
}

// TestDraftWhenAutoSaveOff 自动保存关掉：文件不动，改动进草稿；重开能捡回来。
func TestDraftWhenAutoSaveOff(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.settings.NoAutoSave = true
	t.Cleanup(func() { model.settings.NoAutoSave = false })
	openNamed(t, model, "健康检查")

	model.req.url = "http://127.0.0.1:1/draft-only"
	model.markEdited()
	model.autoSaveAt = time.Now().Add(-time.Second)
	model.pumpAutoSave()

	raw, _ := requestFile(t, dir, "健康检查.yml")
	if strings.Contains(raw, "/draft-only") {
		t.Fatalf("关掉自动保存后不该写集合文件：\n%s", raw)
	}
	if _, ok := drafts.Load(model.draftDir(), drafts.Request, dir, "健康检查.yml"); !ok {
		t.Fatal("应写出草稿")
	}
	if !strings.Contains(model.req.note, "草稿") {
		t.Fatalf("应提示改动存为草稿：%q", model.req.note)
	}

	// 换一个模型重新打开（模拟重启）：草稿要盖回编辑器。
	fresh, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	reopened := New(fresh, "", nil)
	openNamed(t, reopened, "健康检查")
	if reopened.req.url != "http://127.0.0.1:1/draft-only" {
		t.Fatalf("草稿没恢复：%q", reopened.req.url)
	}
	if !reopened.req.dirty || !strings.Contains(reopened.req.note, "恢复") {
		t.Fatalf("恢复后应标记为未保存：dirty=%v note=%q", reopened.req.dirty, reopened.req.note)
	}

	// 手动保存成功 → 草稿删掉，下次打开就是磁盘内容。
	reopened.saveRequest()
	if _, ok := drafts.Load(reopened.draftDir(), drafts.Request, dir, "健康检查.yml"); ok {
		t.Fatal("保存成功后草稿应被删除")
	}
}

// TestAutoSaveDocs 文档编辑：停顿到点后直接写回 .md。
func TestAutoSaveDocs(t *testing.T) {
	dir := writeCollection(t, "http://127.0.0.1:1")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatalf("建 docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "指南.md"), []byte("---\nname: 指南\n---\n# 指南\n"), 0o644); err != nil {
		t.Fatalf("准备文档: %v", err)
	}
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	var doc *explorer.Entry
	for _, entry := range col.Entries {
		if entry.IsDoc() {
			doc = entry
		}
	}
	if doc == nil {
		t.Fatal("测试集合里没有文档")
	}
	model.openEntry(doc)
	model.tab = tabEdit
	model.editor.text = "# 指南\n\n自动保存写进来的。\n"
	model.autoSaveDocAt = time.Now().Add(-time.Second)
	model.pumpAutoSave()

	raw, err := os.ReadFile(filepath.Join(dir, "docs", "指南.md"))
	if err != nil {
		t.Fatalf("读文档: %v", err)
	}
	if !strings.Contains(string(raw), "自动保存写进来的") {
		t.Fatalf("文档没自动保存：\n%s", raw)
	}
	if model.docDirty() {
		t.Fatal("写盘后不该还是脏的")
	}
}

// TestSettingsPersistAutoSave 两个新设置项要落进偏好。
func TestSettingsPersistAutoSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	col, err := explorer.Load(writeCollection(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	model.openSettings()
	model.ui.autoSave = false
	model.ui.autoSaveSec = "5"
	model.applySettingsEdits()

	saved := settings.Load()
	if saved.AutoSave() {
		t.Fatal("关掉自动保存没落盘")
	}
	if saved.AutoSaveInterval() != 5*time.Second {
		t.Fatalf("间隔没落盘：%v", saved.AutoSaveInterval())
	}
}
