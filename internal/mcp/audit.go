package mcp

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 调用审计：每次工具调用追加一行 jsonl（时间 / 工具 / 是否写 / 耗时 / 结果 / 参数摘要）。
//
// 为什么落盘而不是只留内存：AI 会连续调很多次，出问题时要能翻「它到底改了什么」；
// 参数只留**摘要**（长度上限 + 敏感键打码），避免把 token 之类的东西抄进去。

// AuditMax 是审计文件保留的条目数：超出后由调用方按需裁剪（这里只在 Append 后截断）。
const AuditMax = 2000

// AuditEntry 是一条调用记录。
type AuditEntry struct {
	Time   time.Time `json:"time"`
	Tool   string    `json:"tool"`
	Write  bool      `json:"write,omitempty"`
	OK     bool      `json:"ok"`
	Millis int64     `json:"ms"`
	Args   string    `json:"args,omitempty"` // 摘要，已截断与打码
	Err    string    `json:"err,omitempty"`
}

// Audit 是审计日志的落盘点；Path 为空时只记内存（测试与「关掉审计」时）。
type Audit struct {
	mu      sync.Mutex
	path    string
	entries []AuditEntry
}

// NewAudit 建一个审计器，path 为空表示只留内存。
func NewAudit(path string) *Audit { return &Audit{path: path} }

// Record 记一次调用。
func (a *Audit) Record(tool string, write bool, args any, started time.Time, err error) {
	if a == nil {
		return
	}
	entry := AuditEntry{
		Time:   time.Now(),
		Tool:   tool,
		Write:  write,
		Millis: time.Since(started).Milliseconds(),
		Args:   summarizeArgs(args),
		OK:     err == nil,
	}
	if err != nil {
		entry.Err = err.Error()
	}
	a.mu.Lock()
	a.entries = append(a.entries, entry)
	if len(a.entries) > AuditMax {
		a.entries = a.entries[len(a.entries)-AuditMax:]
	}
	path := a.path
	a.mu.Unlock()
	if path == "" {
		return
	}
	line, marshalErr := json.Marshal(entry)
	if marshalErr != nil {
		return
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return
	}
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	_, _ = file.Write(append(line, '\n'))
	_ = file.Close()
}

// Load 读回落盘的记录（最新的在前）；文件不存在就是空。
func (a *Audit) Load() []AuditEntry {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	path := a.path
	memory := append([]AuditEntry(nil), a.entries...)
	a.mu.Unlock()
	if path == "" {
		return reverse(memory)
	}
	file, err := os.Open(path)
	if err != nil {
		return reverse(memory)
	}
	defer func() { _ = file.Close() }()
	out := make([]AuditEntry, 0, len(memory))
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var entry AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		out = append(out, entry)
	}
	return reverse(out)
}

// Export 把审计导出到另一个文件（界面上的「导出」）。
func (a *Audit) Export(target string) (int, error) {
	if a == nil {
		return 0, nil
	}
	entries := a.Load()
	file, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	written := 0
	for _, entry := range reverse(entries) { // 导出按时间正序，好读
		line, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// Clear 清空内存与文件。
func (a *Audit) Clear() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	a.entries = nil
	path := a.path
	a.mu.Unlock()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// summarizeArgs 把入参压成一行摘要：截断 + 敏感键打码。
func summarizeArgs(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	text := string(data)
	if text == "{}" || text == "null" {
		return ""
	}
	text = maskArgSecrets(text)
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return text
}

// sensitiveKeys 是入参里不该落盘明文的键（小写匹配）。
var sensitiveKeys = []string{"token", "password", "secret", "authorization", "apikey", "api_key"}

// maskArgSecrets 做两件事：
//  1. 把 `"token":"xxx"` 这类**键**直接打码；
//  2. 入参里出现 `"name":"token"` 这种「变量名本身敏感」时，连它的 `"value"` 一起打码
//     （set_env_var 的入参就是 name + value 两个字段）。
func maskArgSecrets(text string) string {
	lower := strings.ToLower(text)
	for _, key := range sensitiveKeys {
		if strings.Contains(lower, `"name":"`+key+`"`) {
			text = maskField(text, "value")
			lower = strings.ToLower(text)
			break
		}
	}
	return maskFields(text)
}

// maskFields 逐个敏感键名打码。
func maskFields(text string) string {
	for _, key := range sensitiveKeys {
		text = maskField(text, key)
	}
	return text
}

// maskField 把一个键的值换成掩码（多次出现也全换）。
func maskField(text, key string) string {
	lower := strings.ToLower(text)
	{
		needle := `"` + key + `":"`
		index := 0
		for {
			at := strings.Index(lower[index:], needle)
			if at < 0 {
				break
			}
			start := index + at + len(needle)
			end := strings.IndexByte(text[start:], '"')
			if end < 0 {
				break
			}
			masked := text[:start] + "••••" + text[start+end:]
			lower = strings.ToLower(masked)
			text = masked
			index = start + len("••••")
		}
	}
	return text
}

// reverse 返回倒序副本（不改原切片）。
func reverse(in []AuditEntry) []AuditEntry {
	out := make([]AuditEntry, len(in))
	for i, entry := range in {
		out[len(in)-1-i] = entry
	}
	return out
}
