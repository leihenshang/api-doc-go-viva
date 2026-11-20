package history

import (
	"fmt"
	"testing"
	"time"
)

// TestAppendKeepsTheNewestFirst 追加是有序的：最新的在最前，并按 Max 裁掉最早的。
func TestAppendKeepsTheNewestFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := Append(Entry{Method: "GET", URL: "http://a/1", Status: 200}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(Entry{Method: "POST", URL: "http://a/2", Status: 500, Duration: 12, Size: 3}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries := Load()
	if len(entries) != 2 {
		t.Fatalf("应有两条记录：%+v", entries)
	}
	if entries[0].URL != "http://a/2" || entries[1].URL != "http://a/1" {
		t.Fatalf("最新的应排在最前：%+v", entries)
	}
	if entries[0].Status != 500 || entries[0].Duration != 12 {
		t.Fatalf("字段没写全：%+v", entries[0])
	}
	if entries[0].Time.IsZero() {
		t.Fatal("没补时间")
	}

	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if got := Load(); len(got) != 0 {
		t.Fatalf("清空后应为空：%+v", got)
	}
}

// TestAppendTrimsToMax 超过上限只留最新 Max 条。
func TestAppendTrimsToMax(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := time.Now()
	for i := 0; i < Max+5; i++ {
		entry := Entry{Method: "GET", URL: fmt.Sprintf("http://a/%d", i), Time: base.Add(time.Duration(i) * time.Millisecond)}
		if err := Append(entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	entries := Load()
	if len(entries) != Max {
		t.Fatalf("应裁到 %d 条，实际 %d", Max, len(entries))
	}
	if entries[0].URL != fmt.Sprintf("http://a/%d", Max+4) {
		t.Fatalf("留下的应是最新的：%q", entries[0].URL)
	}
}

// TestEntryKeepsRequestSnapshot 快照字段能落盘再读回（含打码后的头与截断标记）。
func TestEntryKeepsRequestSnapshot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	entry := Entry{
		Method: "POST", URL: "http://x/api", Status: 200,
		Headers:       []Field{{Name: "Authorization", Value: "••••（已打码）"}, {Name: "X-Team", Value: "core"}},
		Params:        []Field{{Name: "page", Value: "2"}},
		Body:          `{"a":1}`,
		BodyTruncated: true,
	}
	if err := Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rows := Load()
	if len(rows) != 1 {
		t.Fatalf("应有 1 条：%+v", rows)
	}
	got := rows[0]
	if len(got.Headers) != 2 || got.Headers[0].Value != "••••（已打码）" {
		t.Fatalf("请求头没读回：%+v", got.Headers)
	}
	if len(got.Params) != 1 || got.Params[0].Name != "page" {
		t.Fatalf("参数没读回：%+v", got.Params)
	}
	if got.Body != `{"a":1}` || !got.BodyTruncated {
		t.Fatalf("正文或截断标记不对：%q %v", got.Body, got.BodyTruncated)
	}
}
