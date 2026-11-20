package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-share/collection"
)

// TestDefaultsRoundTrip 公共参数写回清单：headers / auth 都在，未知顶层字段与 info 不丢。
func TestDefaultsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("写入 %s: %v", name, err)
		}
	}
	write("opencollection.yml", `opencollection: 1.0.0
info:
  name: 公共参数集合
meta:
  uid: defaults-1
自定义字段: 保留我
`)

	def := collection.Defaults{
		Headers: []collection.KV{
			{Name: "X-Team", Value: "{{team}}", Enabled: true},
			{Name: "X-Debug", Value: "1", Enabled: false},
		},
		Auth: &collection.Auth{Type: "bearer", Token: "{{token}}"},
	}
	if err := SaveDefaults(dir, def); err != nil {
		t.Fatalf("SaveDefaults: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "opencollection.yml"))
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if !strings.Contains(string(raw), "defaults:") || !strings.Contains(string(raw), "X-Team") {
		t.Fatalf("没写出 defaults 段：\n%s", raw)
	}
	if !strings.Contains(string(raw), "自定义字段") {
		t.Fatalf("未知顶层字段被删掉了：\n%s", raw)
	}

	back := LoadDefaults(dir)
	if len(back.Headers) != 2 || back.Headers[1].Enabled {
		t.Fatalf("读回不对：%+v", back.Headers)
	}
	if back.Auth == nil || back.Auth.Type != "bearer" || back.Auth.Token != "{{token}}" {
		t.Fatalf("认证读回不对：%+v", back.Auth)
	}

	// 清空即删除该段。
	if err := SaveDefaults(dir, collection.Defaults{}); err != nil {
		t.Fatalf("SaveDefaults(空): %v", err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "opencollection.yml"))
	if strings.Contains(string(raw), "defaults:") {
		t.Fatalf("清空后不该留 defaults 段：\n%s", raw)
	}
	if strings.Contains(string(raw), "自定义字段") == false {
		t.Fatalf("清空也不该丢未知字段：\n%s", raw)
	}
}

// TestLoadDefaultsMissing 没有清单 / 没有该段都是零值，不报错。
func TestLoadDefaultsMissing(t *testing.T) {
	if def := LoadDefaults(t.TempDir()); len(def.Headers) != 0 || def.Auth != nil {
		t.Fatalf("空目录应返回零值：%+v", def)
	}
}
