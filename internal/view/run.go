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
		rules, err := runner.RulesOf(entry.Payload)
		if err != nil {
			m.run.Note = "读取「" + entry.Name + "」的运行规则失败：" + err.Error()
		}
		cases = append(cases, runner.Case{
			Name: entry.Name, File: entry.File, Plan: m.planFor(entry.Payload), Rules: rules,
		})
	}
	if len(cases) == 0 {
		m.run.Note = "勾选的请求都不在当前集合里了"
		m.invalidator.Invalidate()
		return
	}
	// 变量袋从当前环境起步，跑的过程中被提取结果覆盖。
	bag := make(map[string]string, len(m.vars))
	for name, value := range m.vars {
		bag[name] = value
	}
	options := runner.Options{StopOnFailure: m.run.StopOnFail, Vars: bag}
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

// openRunRules 打开某条请求的运行规则弹窗（提取 / 断言）。
func (m *Model) openRunRules(file string) {
	entry := m.entryByFile(file)
	if entry == nil || entry.Payload == nil {
		return
	}
	rules, err := runner.RulesOf(entry.Payload)
	m.ui.rulesOpen = true
	m.ui.rulesFile = file
	m.ui.rulesName = entry.Name
	m.ui.rulesNote = ""
	m.ui.ruleExtracts = append([]runner.Extract(nil), rules.Extract...)
	m.ui.ruleAsserts = append([]runner.Assert(nil), rules.Assert...)
	if err != nil {
		m.ui.rulesNote = "原来的规则读不出来：" + err.Error()
	}
	m.invalidator.Invalidate()
}

// saveRunRules 把弹窗里的规则写回请求文件的 run: 键，然后重扫让树上的草稿保持一致。
func (m *Model) saveRunRules() {
	entry := m.entryByFile(m.ui.rulesFile)
	if entry == nil || entry.Payload == nil {
		m.ui.rulesNote = "这条请求已经不在集合里了"
		m.invalidator.Invalidate()
		return
	}
	rules := runner.Rules{
		Extract: append([]runner.Extract(nil), m.ui.ruleExtracts...),
		Assert:  append([]runner.Assert(nil), m.ui.ruleAsserts...),
	}
	runner.Apply(entry.Payload, rules)
	if err := explorer.SaveRequest(m.col.Dir, entry.File, entry.Payload); err != nil {
		m.ui.rulesNote = "保存失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.ui.rulesNote = "已保存到 " + entry.File
	m.reloadCollection()
	m.invalidator.Invalidate()
}

// runMethodText 是列表里显示的方法（空值当 GET，与发送口径一致）。
func runMethodText(method string) string {
	upper := strings.ToUpper(strings.TrimSpace(method))
	if upper == "" {
		return "GET"
	}
	return upper
}
