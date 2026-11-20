// Package history 记录每次真实发送的请求：时间、方法、地址、状态、耗时、体积。
// 只落本机偏好目录（history.jsonl），不写进集合——集合是纯文本真相源，
// 历史属于「我在这台机器上做过什么」。
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// Max 是保留的条数：超出后丢最早的，文件不会无限长。
const Max = 300

// Entry 是一条发送记录。
type Entry struct {
	Time     time.Time `json:"time"`
	Method   string    `json:"method"`
	URL      string    `json:"url"`
	Status   int       `json:"status,omitempty"`
	Duration int64     `json:"durationMs,omitempty"`
	Size     int       `json:"size,omitempty"`
	Err      string    `json:"err,omitempty"`
	// Name / File / Dir 用来跳回那条请求：临时请求与没落盘的草稿只有 URL。
	Name string `json:"name,omitempty"`
	File string `json:"file,omitempty"` // 集合根下的相对路径
	Dir  string `json:"dir,omitempty"`  // 集合根，换集合后也认得出
	Env  string `json:"env,omitempty"`
}

// Path 是历史文件的位置（与偏好同目录）。
func Path() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.jsonl"), nil
}

// Load 读全部记录，最新的在前；文件不存在就是空列表。
func Load() []Entry {
	path, err := Path()
	if err != nil {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	entries := make([]Entry, 0, 32)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var entry Entry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue // 坏行跳过：历史不该挡住界面
		}
		entries = append(entries, entry)
	}
	// 文件是追加写的（旧在前），读出来翻成新的在前。
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries
}

// Append 追加一条记录，并把文件裁到 Max 条（先写临时文件再换，写坏了也不会留半个文件）。
func Append(entry Entry) error {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	path, err := Path()
	if err != nil {
		return err
	}
	entries := Load() // 最新在前
	entries = append([]Entry{entry}, entries...)
	if len(entries) > Max {
		entries = entries[:Max]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("建配置目录: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "history-*.jsonl")
	if err != nil {
		return fmt.Errorf("写历史: %w", err)
	}
	writer := bufio.NewWriter(temp)
	for index := len(entries) - 1; index >= 0; index-- { // 倒序存：旧在前
		line, err := json.Marshal(entries[index])
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return fmt.Errorf("写历史: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return fmt.Errorf("写历史: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("写历史: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("写历史: %w", err)
	}
	return nil
}

// Clear 清空历史。
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
