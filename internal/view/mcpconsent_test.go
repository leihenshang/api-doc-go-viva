package view

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
)

// TestMCPConsentAnswers 写操作确认：允许通过、拒绝报错、关掉开关直接放行。
func TestMCPConsentAnswers(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name  string
		allow bool
		off   bool
		want  bool // 是否应通过
	}{
		{"允许", true, false, true},
		{"拒绝", false, false, false},
		{"关掉确认", false, true, true},
	}
	for _, one := range cases {
		model := New(col, "", nil)
		model.settings.MCPConsentDisabled = one.off
		go func() {
			// 等界面「点」一下：关掉确认时不会有弹窗，直接退出。
			time.Sleep(30 * time.Millisecond)
			if !one.off {
				model.answerMCPConsent(one.allow)
			}
		}()
		err := model.confirmMCPWrite(context.Background(), "save_doc", map[string]string{"name": "x"})
		if (err == nil) != one.want {
			t.Fatalf("%s：err=%v want 通过=%v", one.name, err, one.want)
		}
	}
}

// TestMCPConsentOneAtATime 同一时刻只允许一个待确认：第二个写操作直接拒绝。
func TestMCPConsentOneAtATime(t *testing.T) {
	col, err := explorer.Load(filepath.Join("..", "explorer", "testdata", "docs"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	done := make(chan error, 1)
	go func() {
		done <- model.confirmMCPWrite(context.Background(), "first", nil)
	}()
	// 等第一个进入队列。
	deadline := time.Now().Add(time.Second)
	for model.mcpConsent.take() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := model.confirmMCPWrite(context.Background(), "second", nil); err == nil {
		t.Fatal("已有待确认时，第二个写操作应被拒绝")
	}
	model.answerMCPConsent(false)
	<-done
}
