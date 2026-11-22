package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCollection 造一个最小集合：清单 + 两条请求（一条成功、一条状态码不对）。
func writeCollection(t *testing.T, url string, failURL string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"opencollection.yml": "opencollection: 1.0.0\ninfo:\n  name: CLI 集合\n",
		"健康.yml": fmt.Sprintf(`info:
  name: 健康检查
  type: http
  seq: 1
meta:
  uid: cli-1
http:
  method: GET
  url: %s/ok
  body:
    type: none
`, url),
		"故障.yml": fmt.Sprintf(`info:
  name: 故障
  type: http
  seq: 2
meta:
  uid: cli-2
http:
  method: GET
  url: %s/fail
  body:
    type: none
`, failURL),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("写 %s: %v", name, err)
		}
	}
	return dir
}

// TestRunTextReport 终端报告：一条通过一条失败，退出码 1。
func TestRunTextReport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "--collection", dir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("有失败应返回 1，实际 %d（stderr=%s）", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "健康检查") || !strings.Contains(out, "故障") {
		t.Fatalf("报告里缺条目：%s", out)
	}
	if !strings.Contains(out, "2 条：1 通过，1 失败") {
		t.Fatalf("汇总不对：%s", out)
	}
}

// TestRunJUnitToFile 报告写文件：XML 里有 testcase。
func TestRunJUnitToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	dir := writeCollection(t, srv.URL, srv.URL)

	out := filepath.Join(t.TempDir(), "report.xml")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"run", "--collection", dir, "--filter", "健康",
		"--reporter", "junit", "--output", out,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("全通过应返回 0，实际 %d（stderr=%s）", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读报告: %v", err)
	}
	if !strings.Contains(string(data), "<testcase") || !strings.Contains(string(data), "健康检查") {
		t.Fatalf("报告内容不对：%s", data)
	}
}

// TestRunRejectsMissingCollection 用法错误返回 2。
func TestRunRejectsMissingCollection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"run"}, &stdout, &stderr); code != 2 {
		t.Fatalf("缺 --collection 应返回 2，实际 %d", code)
	}
	if code := Run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("无参数应返回 2，实际 %d", code)
	}
	if code := Run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help 应返回 0，实际 %d", code)
	}
}
