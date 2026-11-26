package view

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vivaui/viva/ui"
)

// mcpConsentTimeout 是一次写操作等待用户确认的上限：超时按拒绝处理，
// 免得 AI 卡在半路、界面也一直挂着弹窗。
const mcpConsentTimeout = 60 * time.Second

// mcpConsentRequest 是一次待确认的写操作。
type mcpConsentRequest struct {
	tool  string
	args  string
	reply chan bool
}

// mcpConsent 是「服务端 goroutine 提问、界面线程回答」的桥。
// 同一时刻只允许一个待确认：多个写操作同时排队只会让人更慌。
type mcpConsent struct {
	mu      sync.Mutex
	pending *mcpConsentRequest
}

// ask 把请求放进队列并由界面弹出确认框；已有待确认时直接拒绝。
func (c *mcpConsent) ask(req mcpConsentRequest) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil {
		return false
	}
	c.pending = &req
	return true
}

// take 取走待确认（界面每帧调用，取到就画弹窗）。
func (c *mcpConsent) take() *mcpConsentRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := c.pending
	return req
}

// answer 回写用户的选择并清空队列。
func (c *mcpConsent) answer(allow bool) {
	c.mu.Lock()
	req := c.pending
	c.pending = nil
	c.mu.Unlock()
	if req == nil {
		return
	}
	select {
	case req.reply <- allow:
	default:
	}
}

// drop 在超时或 ctx 取消时清场（回答丢弃，不阻塞调用方）。
func (c *mcpConsent) drop(req *mcpConsentRequest) {
	c.mu.Lock()
	if c.pending == req {
		c.pending = nil
	}
	c.mu.Unlock()
}

// confirmMCPWrite 是交给服务端的 Confirm 回调：阻塞等用户点「允许 / 拒绝」。
func (m *Model) confirmMCPWrite(ctx context.Context, tool string, in any) error {
	// 零值即「需要确认」：只有用户显式关掉才放行。
	if m.settings.MCPConsentDisabled {
		return nil
	}
	req := mcpConsentRequest{tool: tool, args: consentArgsText(in), reply: make(chan bool, 1)}
	if !m.mcpConsent.ask(req) {
		return errors.New("上一个写操作还没确认，这次已拒绝")
	}
	m.invalidator.Invalidate()
	select {
	case allow := <-req.reply:
		if !allow {
			return errors.New("用户拒绝了这次写操作")
		}
		return nil
	case <-ctx.Done():
		m.mcpConsent.drop(&req)
		return ctx.Err()
	case <-time.After(mcpConsentTimeout):
		m.mcpConsent.drop(&req)
		return errors.New("等待确认超时，已拒绝")
	}
}

// consentArgsText 把工具入参排成一行 JSON：确认框要让用户看清「AI 到底要改什么」，
// 直接 %v 打结构体会变成 {  E2E-允许 GET ... map[] } 这种读不懂的样子。
func consentArgsText(in any) string {
	if raw, err := json.Marshal(in); err == nil {
		return string(raw)
	}
	return fmt.Sprintf("%v", in)
}

// mcpConsentDialog 画写操作确认框：每帧检查一次待确认队列。
func (m *Model) mcpConsentDialog(c ui.C) {
	req := m.mcpConsent.take()
	if req == nil {
		// 队列空了（用户答过 / 超时丢弃）：把弹窗一起收掉，别留一个点不动的确认框。
		m.ui.mcpConsentOpen = false
		return
	}
	// 有待确认才开：Modal 的开关一直是 false 的话，这个框根本画不出来
	// （写操作会一路等到 60 秒超时被拒，用户在界面上看不到任何提示）。
	m.ui.mcpConsentOpen = true
	ui.Modal(c, &m.ui.mcpConsentOpen, "MCP 写操作确认", ui.ModalSpec{Width: px(520), Height: px(240)}, func() {
		ui.Column(c).FillWidth().Gap(px(10)).Children(func() {
			ui.Text(c, "AI 想执行写操作："+req.tool).FontSize(fs(13)).Bold()
			ui.Text(c, "参数："+req.args).FontSize(fs(11)).Mono().Muted().MaxLines(3)
			ui.Text(c, "允许后它会直接改动磁盘上的集合文件。").
				FontSize(fs(11)).Color(c.Theme().Danger)
		})
	}, func() {
		ui.Row(c).FillWidth().Gap(px(8)).Justify(ui.End).Children(func() {
			if ui.Button(c, "拒绝").Outline().Clicked() {
				c.After(func() { m.answerMCPConsent(false) })
			}
			if ui.Button(c, "允许").Clicked() {
				c.After(func() { m.answerMCPConsent(true) })
			}
		})
	})
}

// answerMCPConsent 回写选择并让弹窗收起。
func (m *Model) answerMCPConsent(allow bool) {
	m.mcpConsent.answer(allow)
	m.ui.mcpConsentOpen = false
	m.invalidator.Invalidate()
}
