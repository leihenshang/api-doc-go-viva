// Package view 是 Viva 验证版的界面：左侧集合树，右侧按条目类型显示请求 / 响应 / 文档 / 编辑 / 预览。
//
// 模型只持有「已载入的集合 + 界面状态 + 每次状态变化时算好的文案/表格」，
// 不碰磁盘格式（explorer），不碰网络（send）。所有派生数据都在变化时算一次：
// 页面每帧重跑，声明期不做重复计算与分配。
package view

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

// detailTab 是右侧的页签：请求 / 响应 / 文档（请求条目）与 编辑 / 预览（文档条目）。
type detailTab uint8

const (
	tabRequest detailTab = iota
	tabResponse
	tabDocs
	tabEdit
	tabPreview
)

// 两个页签集合是包级切片：Tabs 每帧都要它，放这里避免每帧分配。
var (
	httpTabs = []detailTab{tabRequest, tabResponse, tabDocs}
	docTabs  = []detailTab{tabEdit, tabPreview}
)

func tabLabel(tab detailTab) string {
	switch tab {
	case tabRequest:
		return "请求"
	case tabResponse:
		return "响应"
	case tabDocs:
		return "文档"
	case tabEdit:
		return "编辑"
	default:
		return "预览"
	}
}

// docState 是 Markdown 排版缓存（请求的 Docs 与文档预览共用）。
// Layout 是昂贵的一半：只在源文本或可用宽度变化时重排；Paint 每帧都跑，只画不算。
type docState struct {
	key    string
	source string
	width  float32
	height float32
	doc    markdown.Document
	view   components.MarkdownView
	look   components.MarkdownLook
}

// docEditor 是一篇文档的编辑状态：正文可改，frontmatter 原样保留。
type docEditor struct {
	file       string
	text       string
	saved      string
	front      string
	metaLine   string
	previewKey string
	status     string
}

// 布局常量：分隔条厚度与详情页内边距，用于推算 Markdown 的可用宽度。
const (
	splitterThickness = 8
	detailPadding     = 20
)

// newDocName 是「新建文档」的默认名，与客户端一样靠 -2 后缀去重。
const newDocName = "新建文档"

// sendResult 是后台发送交回界面线程的结果，带代号以丢弃过期响应。
type sendResult struct {
	gen int
	out send.Outcome
}

// requestTables 是「当前选中请求」派生出来的表格与文案，选中项变化时重建一次。
type requestTables struct {
	metaLine     string
	docsKey      string
	authLine     string
	bodyText     string
	grpcEndpoint string
	grpcMeta     string
	grpcMessage  string
	params       ui.TableRows
	headers      ui.TableRows
	fields       ui.TableRows
}

// workspaceUI 是弹窗与设置面板的输入状态（不参与集合语义，只是界面状态）。
type workspaceUI struct {
	newReqOpen bool
	newName    string
	newMethod  string
	newURL     string
	newErr     string

	renameOpen bool
	renameName string
	renameFile string
	renameErr  string

	settingsOpen   bool
	themePick      int
	imeDetail      string
	dirLine        string
	envLine        string
	collectionName string
}

// Model 一次运行的界面状态。
type Model struct {
	col     *explorer.Collection
	envName string
	vars    map[string]string

	theme ui.Theme
	split ui.SplitterState
	list  ui.ListState
	tab   detailTab

	filter    string
	shown     []*explorer.Entry
	shownFor  string
	shownOnce bool
	countText string
	subtitle  string

	// entryFile 是当前右侧展示的条目；pendingSelect 是重扫后要选中的路径。
	entryFile     string
	pendingSelect string
	createErr     string

	tables      requestTables
	tableStates [3]ui.TableState

	docs     docState
	editor   docEditor
	drafts   map[string]string
	settings settings.Settings
	ui       workspaceUI

	resp     send.Outcome
	hasResp  bool
	sending  bool
	status   string
	reason   string
	elapsed  string
	size     string
	bodyText string
	missing  string
	failText string
	statusOK bool

	// gen 是「当前选中请求」的代号：换请求后，旧请求在途的响应作废。
	gen         int
	results     chan sendResult
	invalidator app.Invalidator
}

// New 建模型：列表行名直接取自集合，过滤后仍按「方法 + 名称」朗读。
func New(col *explorer.Collection, envName string, vars map[string]string) *Model {
	m := &Model{
		col:      col,
		envName:  envName,
		vars:     vars,
		theme:    ui.DarkTheme(),
		results:  make(chan sendResult, 1),
		drafts:   map[string]string{},
		settings: settings.Load(),
	}
	m.ui.newMethod = "GET"
	m.applyTheme()
	m.ui.imeDetail = ime.Detect().Detail
	m.split.Pos = 320
	m.list.Label = func(i int) string { return m.shown[i].Name }
	m.list.Name = func(i int) string { return m.shown[i].Title() }
	m.refreshShown()
	return m
}

// config 是窗口与页面的接线：Run 与无头测试共用，免得两处配置漂移。
func (m *Model) config() app.Config {
	page := ui.NewPage(ui.PageConfig{View: m.Page, Theme: &m.theme})
	return page.Config(app.Config{
		Title:       "API-DOC · Viva 验证版",
		Class:       "api-doc-viva",
		Width:       1180,
		Height:      760,
		MinWidth:    900,
		MinHeight:   560,
		Invalidator: &m.invalidator,
		OnWake:      m.collect,
	})
}

// Run 打开窗口并阻塞到关闭；后台发送完成后由 Invalidator 唤醒重绘。
func (m *Model) Run() error {
	var canvas gfx.Canvas
	return app.RunCanvas(m.config(), &canvas)
}

// refreshShown 只在过滤词变化时重建可见列表，顺带更新计数文案。
func (m *Model) refreshShown() {
	if m.shownOnce && m.shownFor == m.filter {
		return
	}
	m.shown = filterEntries(m.col.Entries, m.filter)
	m.shownFor = m.filter
	m.shownOnce = true
	m.countText = countLabel(len(m.shown), len(m.col.Entries), m.filter)
	m.subtitle = m.countText + " · 环境 " + envLabel(m.envName)
}

// filterEntries 按名称 / 分组 / URL / 概述做大小写不敏感匹配。
func filterEntries(all []*explorer.Entry, filter string) []*explorer.Entry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if needle == "" {
		return all
	}
	out := make([]*explorer.Entry, 0, len(all))
	for _, entry := range all {
		haystack := strings.ToLower(entry.Name + " " + entry.Group + " " + entry.URL + " " + entry.Summary)
		if strings.Contains(haystack, needle) {
			out = append(out, entry)
		}
	}
	return out
}

// current 取列表当前选中项；还没选过时落在第一行。
func (m *Model) current() *explorer.Entry {
	if len(m.shown) == 0 {
		return nil
	}
	index := m.list.Selected()
	if index < 0 || index >= len(m.shown) {
		index = 0
	}
	return m.shown[index]
}

// indexOfFile 在可见列表里找一条路径；找不到返回 -1。
func (m *Model) indexOfFile(file string) int {
	for i, entry := range m.shown {
		if entry.File == file {
			return i
		}
	}
	return -1
}

// applyPendingSelect 在重扫后把选中项落回关心的路径。
// ListState.Select 会按上一帧的行数截断，新行要等下一帧才选得上，所以必要时再要一帧。
func (m *Model) applyPendingSelect() {
	if m.pendingSelect == "" {
		return
	}
	index := m.indexOfFile(m.pendingSelect)
	if index < 0 {
		m.pendingSelect = ""
		return
	}
	m.list.Select(index)
	if m.list.Selected() != index {
		m.invalidator.Invalidate()
		return
	}
	m.pendingSelect = ""
}

// entryByFile 在完整集合里按路径找条目（保存后同步正文用）。
func (m *Model) entryByFile(file string) *explorer.Entry {
	for _, entry := range m.col.Entries {
		if entry.File == file {
			return entry
		}
	}
	return nil
}

// ensureEntry 在选中项变化时重建派生状态：请求建表格、文档装编辑器。
func (m *Model) ensureEntry(entry *explorer.Entry) {
	if m.entryFile == entry.File {
		return
	}
	m.entryFile = entry.File
	if entry.IsDoc() {
		m.tab = tabEdit
		m.ensureEditor(entry)
		return
	}
	if m.tab == tabEdit || m.tab == tabPreview {
		m.tab = tabRequest
	}
	// 换了一条请求：上一次的响应不再属于它，在途结果也作废。
	m.gen++
	m.sending = false
	m.resetResponse()
	m.tables = requestTables{
		metaLine: entry.File + " · " + orDash(entry.Group),
		docsKey:  entry.File + "#docs",
	}
	if entry.Kind != explorer.KindHTTP {
		m.fillGRPC(entry)
		return
	}
	block := entry.Payload.HTTP
	m.tables.params = kvRows(block.Params)
	m.tables.headers = kvRows(block.Headers)
	m.tables.fields = fieldRows(block.Body.Fields)
	if auth := block.Auth; auth != nil && auth.Type != "" {
		m.tables.authLine = "认证：" + auth.Type
	}
	if block.Body.Type != "" && block.Body.Type != "none" {
		m.tables.bodyText = clip(bodyRaw(block.Body), 2000)
	}
}

// fillGRPC 把 gRPC 段的元数据拼成固定文案，绘制期不再格式化。
func (m *Model) fillGRPC(entry *explorer.Entry) {
	grpc := entry.Payload.GRPC
	if grpc == nil {
		return
	}
	m.tables.grpcEndpoint = grpc.Target + " / " + grpc.Service + "." + grpc.Method
	meta := "流式：" + orDash(grpc.Stream)
	if grpc.Proto != "" {
		meta = "proto：" + grpc.Proto + " · " + meta
	}
	m.tables.grpcMeta = meta
	m.tables.grpcMessage = clip(grpc.Message, 2000)
}

// ensureEditor 装载一篇文档：有草稿优先用草稿，离开上一篇时把改动留下。
func (m *Model) ensureEditor(entry *explorer.Entry) {
	if m.editor.file == entry.File {
		return
	}
	if m.editor.file != "" && m.editor.text != m.editor.saved {
		m.drafts[m.editor.file] = m.editor.text
	}
	text := entry.Content
	if draft, ok := m.drafts[entry.File]; ok {
		text = draft
	}
	m.editor = docEditor{
		file:       entry.File,
		text:       text,
		saved:      entry.Content,
		front:      entry.Front,
		metaLine:   entry.File + " · " + orDash(entry.Group),
		previewKey: entry.File + "#preview",
	}
}

// docDirty 报告文档有没有未保存的改动。
func (m *Model) docDirty() bool { return m.editor.text != m.editor.saved }

// createDoc 新建一篇 Markdown（客户端同款 docs/ 默认位与 -2 去重），随后重扫并选中它。
// 由 c.After 调用：它会改变正在声明的列表。
func (m *Model) createDoc() {
	path, err := explorer.CreateDoc(m.col.Dir, newDocName)
	if err == nil {
		m.col, err = explorer.Load(m.col.Dir)
	}
	if err != nil {
		m.createErr = err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.createErr = ""
	m.filter = ""
	m.shownOnce = false
	m.entryFile = ""
	m.tab = tabEdit
	m.pendingSelect = path
	m.invalidator.Invalidate()
}

// saveDoc 把编辑器里的正文写回文档，frontmatter 原样保留。
func (m *Model) saveDoc() {
	if !m.docDirty() {
		return
	}
	if err := explorer.SaveDoc(m.col.Dir, m.editor.file, m.editor.front, m.editor.text); err != nil {
		m.editor.status = "保存失败：" + err.Error()
		return
	}
	m.editor.saved = m.editor.text
	m.editor.status = "已保存"
	delete(m.drafts, m.editor.file)
	if entry := m.entryByFile(m.editor.file); entry != nil {
		entry.Content = m.editor.text
	}
	m.docs.key = "" // 预览按新正文重排
}

// start 把发送放到后台：界面线程只做一次快照，结果经通道交回 OnWake。
func (m *Model) start(entry *explorer.Entry) {
	if m.sending || entry.Payload == nil {
		return
	}
	m.resetResponse()
	m.sending = true
	plan := send.Plan{File: entry.Payload, Vars: m.vars, MaxBody: 1 << 20}
	gen := m.gen
	go func() {
		m.results <- sendResult{gen: gen, out: send.Do(context.Background(), plan)}
		m.invalidator.Invalidate()
	}()
}

// resetResponse 清掉上一次的结果，避免发送中看到旧状态。
func (m *Model) resetResponse() {
	m.resp = send.Outcome{}
	m.hasResp = false
	m.status, m.reason, m.elapsed, m.size = "", "", "", ""
	m.bodyText, m.missing, m.failText, m.statusOK = "", "", "", false
}

// collect 是 OnWake：在重绘前把后台结果搬成界面文案（这里算一次，绘制期只读）。
func (m *Model) collect() {
	var result sendResult
	select {
	case result = <-m.results:
	default:
		return
	}
	if result.gen != m.gen {
		return // 结果属于已经换掉的那条请求
	}
	out := result.out
	m.resp = out
	m.hasResp, m.sending = true, false
	m.tab = tabResponse
	if out.Err != "" {
		m.failText = out.Err
		return
	}
	m.status = strconv.Itoa(out.Status)
	m.reason = out.Reason
	m.elapsed = strconv.FormatInt(out.Duration.Milliseconds(), 10) + " ms"
	m.size = humanBytes(out.Size)
	m.statusOK = out.Status < 400
	m.bodyText = clip(out.Body, 4000)
	if len(out.Missing) > 0 {
		m.missing = "未解析变量：" + strings.Join(out.Missing, ", ")
	}
}

// previewHeight 返回一段 Markdown 在 width 下的高度；源文本或宽度变了才重新排版。
func (m *Model) previewHeight(c ui.C, key, source string, width float32) float32 {
	width = max(width, 80)
	if m.docs.key == key && m.docs.source == source && m.docs.width == width {
		return m.docs.height
	}
	m.docs.key, m.docs.source, m.docs.width = key, source, width
	m.docs.doc.Parse(source)
	m.docs.height = m.docs.view.Layout(c.Canvas(), components.MarkdownStyle{
		Size: m.theme.Metrics.FontSizeM - 1,
	}, &m.docs.doc, width)
	return m.docs.height
}

// contentWidth 是详情页可用的文字宽度：整窗宽减去侧栏、分隔条与两侧内边距。
func (m *Model) contentWidth(c ui.C) float32 {
	return c.Bounds().Width - m.split.Pos - splitterThickness - 2*detailPadding
}

// humanBytes 把字节数写成 B / KB / MB，避免响应区出现七位数。
func humanBytes(n int) string {
	switch {
	case n < 1024:
		return strconv.Itoa(n) + " B"
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
