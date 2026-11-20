package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// writeFile 在集合里落一个文件。
func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("写 %s: %v", rel, err)
	}
}

const manifest = `opencollection: 1.0.0
info:
  name: 打包集合
meta:
  uid: bundle-1
`

const requestYAML = `info:
  name: 列表
  type: http
  seq: 1
meta:
  uid: bundle-req-1
http:
  method: GET
  url: "{{host}}/api/users"
  headers: []
  body:
    type: none
`

// TestBundleRoundTrip 导出的集合包写回空目录后，结构与内容一致。
func TestBundleRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeFile(t, src, "opencollection.yml", manifest)
	writeFile(t, src, "用户/列表.yml", requestYAML)
	writeFile(t, src, "docs/说明.md", "# 说明\n")
	writeFile(t, src, "environments/dev.yml", "name: dev\n")

	col, err := explorer.Load(src)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := Encode(col)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	pack, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if pack.Name != "打包集合" || len(pack.Files) != 3 {
		t.Fatalf("包内容不对：name=%q files=%d", pack.Name, len(pack.Files))
	}

	dst := t.TempDir()
	written, renamed, err := Write(dst, pack)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written != 3 || renamed != 0 {
		t.Fatalf("写入统计不对：written=%d renamed=%d", written, renamed)
	}
	back, err := explorer.Load(dst)
	if err != nil {
		t.Fatalf("导入后 Load: %v", err)
	}
	if back.Name != col.Name || len(back.Entries) != len(col.Entries) {
		t.Fatalf("导入结果不一致：%q/%d vs %q/%d", back.Name, len(back.Entries), col.Name, len(col.Entries))
	}
	raw, err := os.ReadFile(filepath.Join(dst, "用户", "列表.yml"))
	if err != nil {
		t.Fatalf("读回请求: %v", err)
	}
	if string(raw) != requestYAML {
		t.Fatalf("请求文件没原样写回：\n%s", raw)
	}

	// 再导一次进同一目录：不覆盖，改名成 -2。
	_, renamed, err = Write(dst, pack)
	if err != nil {
		t.Fatalf("二次 Write: %v", err)
	}
	if renamed == 0 {
		t.Fatal("同名文件应改名而不是覆盖")
	}
}

// TestDecodeRejects 不是集合包 / 空包都要报错。
func TestDecodeRejects(t *testing.T) {
	if _, err := Decode([]byte(`{"format":"other"}`)); err == nil {
		t.Fatal("别的 format 应拒绝")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("坏 JSON 应拒绝")
	}
}
