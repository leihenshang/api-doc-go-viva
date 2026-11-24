package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRunCollection 造一个两段的集合：先登录取 token，再带着 token 取数。
func writeRunCollection(t *testing.T, base string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("写文件: %v", err)
		}
	}
	write("opencollection.yml", "opencollection: 1.0.0\ninfo:\n  name: 运行集合\n")
	write("用户/登录.yml", fmt.Sprintf(`info:
  name: 登录
  type: http
  seq: 1
http:
  method: POST
  url: %s/login
  headers: []
  body:
    type: none
run:
  extract:
    - name: token
      from: json
      path: token
`, base))
	write("用户/取数.yml", fmt.Sprintf(`info:
  name: 取数
  type: http
  seq: 2
http:
  method: GET
  url: %s/data
  headers:
    - name: Authorization
      value: Bearer {{token}}
      enabled: true
  body:
    type: none
run:
  assert:
    - kind: status
      op: equals
      want: "200"
    - kind: json
      path: ok
      op: exists
`, base))
	write("订单/下单.yml", fmt.Sprintf(`info:
  name: 下单
  type: http
  seq: 1
http:
  method: GET
  url: %s/login
  headers: []
  body:
    type: none
`, base))
	return dir
}

// TestRunCollectionChainsVariables 顺序跑两条：先提取 token，后一条用它并断言通过。
func TestRunCollectionChainsVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"token":"secret-token"}`))
		case "/data":
			if r.Header.Get("Authorization") != "Bearer secret-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := writeRunCollection(t, srv.URL)
	svc := &Service{Roots: func() []string { return []string{dir} }}

	text, err := svc.RunCollection(context.Background(), RunCollectionArgs{})
	if err != nil {
		t.Fatalf("RunCollection: %v", err)
	}
	if !strings.Contains(text, "共 3 条") {
		t.Fatalf("没说明条数：%s", text)
	}
	if strings.Count(text, "ok") < 3 {
		t.Fatalf("三条都该通过：%s", text)
	}
	if !strings.Contains(text, "3 通过") {
		t.Fatalf("汇总不对：%s", text)
	}

	// 只跑「订单」分组：只应有一条。
	text, err = svc.RunCollection(context.Background(), RunCollectionArgs{Folder: "订单"})
	if err != nil {
		t.Fatalf("按分组跑: %v", err)
	}
	if !strings.Contains(text, "共 1 条") || !strings.Contains(text, "1 通过") {
		t.Fatalf("分组过滤没生效：%s", text)
	}

	if _, err := svc.RunCollection(context.Background(), RunCollectionArgs{Folder: "不存在"}); err == nil {
		t.Fatal("空分组应该报错")
	}
}
