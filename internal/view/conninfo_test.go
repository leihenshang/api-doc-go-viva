package view

import (
	"strings"
	"testing"

	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// TestConnInfoChips 宽栏四项齐全；窄栏只留协议与 TLS（地址最长，先舍）。
func TestConnInfoChips(t *testing.T) {
	model := &Model{resp: send.Outcome{
		Proto: "HTTP/2.0", TLS: "TLS 1.3", Redirects: 2, Remote: "api.example.com",
	}}
	wide := model.connInfo(false)
	for _, want := range []string{"HTTP/2.0", "TLS 1.3", "2 跳", "api.example.com"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("宽栏缺 %q：%q", want, wide)
		}
	}
	narrow := model.connInfo(true)
	if strings.Contains(narrow, "api.example.com") || strings.Contains(narrow, "跳") {
		t.Fatalf("窄栏该舍掉地址与跳数：%q", narrow)
	}
	if !strings.Contains(narrow, "TLS 1.3") {
		t.Fatalf("窄栏应保留 TLS：%q", narrow)
	}
}

// TestConnInfoPlaintext 明文响应写「明文」，没有 TLS 时不留空槽。
func TestConnInfoPlaintext(t *testing.T) {
	model := &Model{resp: send.Outcome{Proto: "HTTP/1.1", Remote: "127.0.0.1:8080"}}
	got := model.connInfo(false)
	if !strings.Contains(got, "明文") {
		t.Fatalf("应标出明文：%q", got)
	}
	if strings.Contains(got, " ·  · ") {
		t.Fatalf("不该留空的项：%q", got)
	}
	if !strings.Contains(got, "127.0.0.1:8080") {
		t.Fatalf("缺地址：%q", got)
	}
}

// TestConnInfoEmpty 还没响应时是空串（那一行不画任何东西）。
func TestConnInfoEmpty(t *testing.T) {
	model := &Model{}
	if got := model.connInfo(false); got != "" {
		t.Fatalf("没有响应时应为空：%q", got)
	}
}
