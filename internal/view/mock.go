package view

import (
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	appmock "github.com/leihenshang/api-doc-go-viva/internal/mock"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
)

// mockRuntime 是桩服务的界面状态（界面线程只读，不参与每帧计算）。
type mockRuntime struct {
	running bool
	addr    string
	err     string
	routes  []appmock.Route
}

// mockDirs 是当前打开的全部集合根：桩把它们里面的请求都注册成路由。
func (m *Model) mockDirs() []string {
	dirs := make([]string, 0, len(m.roots))
	for _, root := range m.roots {
		if root.col == nil || root.col.Dir == "" {
			continue
		}
		dirs = append(dirs, root.col.Dir)
	}
	return dirs
}

// startMock 按当前偏好启动（或重启）桩服务；未启用时只清掉运行状态。
func (m *Model) startMock() {
	m.stopMock()
	if !m.settings.MockEnabled {
		return
	}
	server, err := appmock.Start(m.settings.MockAddr, m.mockDirs(), nil)
	if err != nil {
		m.mock = mockRuntime{err: err.Error()}
		return
	}
	m.mockServer = server
	m.mock = mockRuntime{running: true, addr: server.Addr(), routes: server.Routes()}
}

// stopMock 关掉桩服务（没启动过也安全）。
func (m *Model) stopMock() {
	if m.mockServer != nil {
		_ = m.mockServer.Close()
		m.mockServer = nil
	}
	m.mock = mockRuntime{}
}

// restartMock 在设置变化后重开服务（开关 / 地址改了都走这里）。
func (m *Model) restartMock() {
	m.startMock()
	m.invalidator.Invalidate()
}

// refreshMock 在集合变化后重建路由表；没启动就是空操作。
func (m *Model) refreshMock() {
	if m.mockServer == nil {
		return
	}
	m.mockServer.Reload(m.mockDirs())
	m.mock.routes = m.mockServer.Routes()
}

// applyMockEdits 把弹窗里的开关与地址落到偏好，需要时重启服务。
func (m *Model) applyMockEdits() {
	addr := strings.TrimSpace(m.ui.mockAddrText)
	if addr == "" {
		addr = appmock.DefaultAddr
	}
	changed := false
	if addr != m.settings.MockAddr {
		m.settings.MockAddr = addr
		changed = true
	}
	if m.ui.mockOn != m.settings.MockEnabled {
		m.settings.MockEnabled = m.ui.mockOn
		changed = true
	}
	if changed {
		_ = settings.Save(m.settings)
	}
	m.restartMock()
}

// mockLabel 是底栏入口的状态文案与提示。
func (m *Model) mockLabel() (label, hint string) {
	switch {
	case m.mock.running:
		return "Mock 运行中", "桩服务运行中：http://" + m.mock.addr + "/__mock/routes 看全部路由"
	case m.mock.err != "":
		return "Mock 出错", "桩服务启动失败：" + m.mock.err
	default:
		return "Mock 未启用", "本地桩服务未启用（在 Mock 弹窗里打开）"
	}
}

// openMockRule 打开某条请求的桩规则弹窗：状态码 / 延迟 / 覆盖响应体。
func (m *Model) openMockRule(file string) {
	entry := m.entryByFile(file)
	if entry == nil || entry.Payload == nil {
		return
	}
	rule := appmock.RuleOf(entry.Payload)
	m.ui.mockRuleOpen = true
	m.ui.mockRuleFile = file
	m.ui.mockRuleName = entry.Name
	m.ui.mockRuleNote = ""
	m.ui.mockRuleStatus = ""
	if rule.Status > 0 {
		m.ui.mockRuleStatus = strconv.Itoa(rule.Status)
	}
	m.ui.mockRuleDelay = ""
	if rule.DelayMS > 0 {
		m.ui.mockRuleDelay = strconv.Itoa(rule.DelayMS)
	}
	m.ui.mockRuleBody = rule.Body
	m.invalidator.Invalidate()
}

// saveMockRule 把规则写回请求文件的 mock: 键，并让正在跑的桩服务重建路由。
func (m *Model) saveMockRule() {
	entry := m.entryByFile(m.ui.mockRuleFile)
	if entry == nil || entry.Payload == nil {
		m.ui.mockRuleNote = "这条请求已经不在集合里了"
		m.invalidator.Invalidate()
		return
	}
	status, _ := strconv.Atoi(strings.TrimSpace(m.ui.mockRuleStatus))
	delay, _ := strconv.Atoi(strings.TrimSpace(m.ui.mockRuleDelay))
	if status != 0 && (status < 100 || status > 599) {
		m.ui.mockRuleNote = "状态码要在 100–599 之间"
		m.invalidator.Invalidate()
		return
	}
	if delay < 0 {
		delay = 0
	}
	rule := appmock.Rule{Status: status, DelayMS: delay, Body: m.ui.mockRuleBody}
	appmock.ApplyRule(entry.Payload, rule)
	if err := explorer.SaveRequest(m.col.Dir, entry.File, entry.Payload); err != nil {
		m.ui.mockRuleNote = "保存失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.reloadCollection()
	m.refreshMock()
	m.ui.mockRuleNote = "已保存到 " + entry.File
	m.invalidator.Invalidate()
}

// openMock 打开 Mock 弹窗：每次打开都把当前偏好抄进表单。
func (m *Model) openMock() {
	m.ui.mockOpen = true
	m.ui.mockOn = m.settings.MockEnabled
	m.ui.mockAddrText = m.settings.MockAddr
	m.invalidator.Invalidate()
}
