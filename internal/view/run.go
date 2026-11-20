package view

import (
	"context"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/runner"
)

// runRow 是运行器列表里的一行：请求的身份与勾选状态（勾选存在 map 里，随弹窗走）。
type runRow struct {
	File     string
	Name     string
	Method   string
	URL      string
	Selected bool
}

// runState 是运行器的界面状态。
type runState struct {
	Rows       []runRow
	Selected   map[string]bool
	Results    []runner.Result
	StopOnFail bool
	Running    bool
	Note       string
}

// openRunner 打开运行器：每次打开都把当前集合的 http 请求抄成一份列表，默认全选。
func (m *Model) openRunner() {
	m.ui.runOpen = true
	rows := make([]runRow, 0, len(m.col.Entries))
	for _, entry := range m.col.Entries {
		if entry.Kind != explorer.KindHTTP || entry.Payload == nil {
			continue
		}
		rows = append(rows, runRow{
			File: entry.File, Name: entry.Name, Method: entry.Method, URL: entry.URL, Selected: true,
		})
	}
	m.run = runState{Rows: rows}
	m.invalidator.Invalidate()
}

// selectAllRuns 全选 / 全不选。
func (m *Model) selectAllRuns(on bool) {
	for index := range m.run.Rows {
		m.run.Rows[index].Selected = on
	}
	m.invalidator.Invalidate()
}

// startRun 把勾选的请求顺序跑起来：界面线程只做快照，结果经通道交回。
func (m *Model) startRun() {
	if m.run.Running {
		return
	}
	rows := make([]runRow, 0, len(m.run.Rows))
	for _, row := range m.run.Rows {
		if row.Selected {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		m.run.Note = "没有勾选任何请求"
		m.invalidator.Invalidate()
		return
	}
	cases := make([]runner.Case, 0, len(rows))
	for _, row := range rows {
		entry := m.entryByFile(row.File)
		if entry == nil || entry.Payload == nil {
			continue
		}
		cases = append(cases, runner.Case{
			Name: entry.Name, File: entry.File, Plan: m.planFor(entry.Payload),
		})
	}
	if len(cases) == 0 {
		m.run.Note = "勾选的请求都不在当前集合里了"
		m.invalidator.Invalidate()
		return
	}
	options := runner.Options{StopOnFailure: m.run.StopOnFail}
	ctx, cancel := context.WithCancel(context.Background())
	channel := make(chan runner.Result, 4)
	m.runCancel = cancel
	m.runCh = channel
	m.run.Running = true
	m.run.Results = m.run.Results[:0]
	m.run.Note = ""
	go func() {
		defer close(channel)
		runner.Run(ctx, cases, options, func(result runner.Result) {
			channel <- result
			m.invalidator.Invalidate()
		})
	}()
}

// stopRun 停下正在跑的集合（已发出的那条会跑完）。
func (m *Model) stopRun() {
	if m.runCancel != nil {
		m.runCancel()
		m.runCancel = nil
	}
	m.run.Note = "已停止"
	m.invalidator.Invalidate()
}

// drainRun 把后台跑完的结果搬进界面（只在界面线程调用，与 drainMCP 同一时机）。
func (m *Model) drainRun() {
	if m.runCh == nil {
		return
	}
	for {
		select {
		case result, ok := <-m.runCh:
			if !ok {
				m.runCh = nil
				m.run.Running = false
				m.runCancel = nil
				m.run.Note = m.runSummary()
				m.invalidator.Invalidate()
				return
			}
			m.run.Results = append(m.run.Results, result)
			m.invalidator.Invalidate()
		default:
			return
		}
	}
}

// runSummary 是跑完以后的一行小结。
func (m *Model) runSummary() string {
	if len(m.run.Results) == 0 {
		return "没有结果"
	}
	failed := 0
	for _, result := range m.run.Results {
		if result.Failed {
			failed++
		}
	}
	if failed == 0 {
		return "全部通过（" + strconv.Itoa(len(m.run.Results)) + " 条）"
	}
	return "失败 " + strconv.Itoa(failed) + " 条 / 共 " + strconv.Itoa(len(m.run.Results)) + " 条"
}

// runMethodText 是列表里显示的方法（空值当 GET，与发送口径一致）。
func runMethodText(method string) string {
	upper := strings.ToUpper(strings.TrimSpace(method))
	if upper == "" {
		return "GET"
	}
	return upper
}
