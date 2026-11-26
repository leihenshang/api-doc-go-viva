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
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/drafts"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/history"
	"github.com/leihenshang/api-doc-go-viva/internal/ime"
	appmcp "github.com/leihenshang/api-doc-go-viva/internal/mcp"
	appmock "github.com/leihenshang/api-doc-go-viva/internal/mock"
	"github.com/leihenshang/api-doc-go-viva/internal/runner"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
	"github.com/leihenshang/api-doc-go-viva/internal/settings"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"github.com/vivaui/viva/ui/codeeditor"
)

// detailTab 是右侧的页签：请求 / 响应 / 文档（请求条目）与 编辑 / 预览（文档条目）。
type detailTab uint8

const (
	// http 请求的四个分段（顺序对齐 Wails：参数 / 请求体 / 请求头 / 认证），外加文档。
	tabParams detailTab = iota
	tabBody
	tabHeaders
	tabAuth
	// gRPC 仍用「请求 / 响应 / 文档」三页签。
	tabRequest
	tabResponse
	tabDocs
	tabEdit
	tabPreview
)

// 页签集合是包级切片：Tabs 每帧都要它，放这里避免每帧分配。
var (
	httpSegments = []detailTab{tabParams, tabBody, tabHeaders, tabAuth, tabDocs}
	docTabs      = []detailTab{tabEdit, tabPreview}
)

func tabLabel(tab detailTab) string {
	switch tab {
	case tabParams:
		return "参数"
	case tabBody:
		return "请求体"
	case tabHeaders:
		return "请求头"
	case tabAuth:
		return "认证"
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
	base   string // 源文本所在目录（相对集合根），图片按它解析相对路径
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

// 布局尺寸：随界面缩放重算（见 scale.go 的 applyUIScale）。
var (
	splitterThickness float32 = 8
	detailPadding     float32 = 20
	// panePadding 是请求栏与响应区两张卡片的内部边距；两边同值才对齐。
	panePadding float32 = 12
)

// newDocName 是「新建文档」的默认名，与客户端一样靠 -2 后缀去重。
const newDocName = "新建文档"

// mockRuntime 与 mockServer 是本地桩服务的状态与句柄（见 view/mock.go）。

type mcpRuntime struct {
	running bool
	addr    string
	err     string
	// audit 是本次运行的调用审计（弹窗里导出 / 清空用）；未运行时为 nil。
	audit *appmcp.Audit
}

// sendResult 是后台发送交回界面线程的结果，带标签代号与请求代号：
// 关掉的标签、换掉的请求都直接丢弃，不会把旧响应贴到别处。
type sendResult struct {
	tab int
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
	respHeaders  []send.Header
	json         []jsonLine
	jsonFolds    map[int]int
	jsonFolded   map[int]bool
}

// palette 是 Wails 客户端的面板色令牌：浅色 sidebar #f5f5f5 / panel #ffffff，
// 暗色 #17181a / #1e1f22，让集合树与请求页有明确的层次，而不是同一块白底。
type palette struct {
	bg         ui.Color
	sidebar    ui.Color
	panel      ui.Color
	border     ui.Color
	active     ui.Color // 树选中行：参考图是信息蓝浅底
	hover      ui.Color // 树行悬停
	accent     ui.Color // 主色：分段按钮选中项的图标
	accentTint ui.Color // 主色浅底：分段按钮选中项的背景（--app-accent-tint）
	findTint   ui.Color // 查找命中：非当前的命中底色
	findActive ui.Color // 查找命中：当前这一处（琥珀底，和选区区分开）
}

func paletteFor(light bool) palette {
	if light {
		return palette{
			bg:         ui.RGB(0xf5, 0xf5, 0xf5),
			sidebar:    ui.RGB(0xf5, 0xf5, 0xf5),
			panel:      ui.RGB(0xff, 0xff, 0xff),
			border:     ui.RGB(0xe3, 0xe3, 0xe3),
			active:     ui.RGB(0xe8, 0xf0, 0xfe),
			hover:      ui.RGB(0xec, 0xec, 0xec),
			accent:     ui.RGB(0x18, 0xa0, 0x58), // #18a058
			accentTint: ui.RGB(0xe8, 0xf5, 0xee), // #e8f5ee
			findTint:   ui.RGB(0xe8, 0xf5, 0xee),
			findActive: ui.RGB(0xfd, 0xe0, 0x8a),
		}
	}
	return palette{
		bg:         ui.RGB(0x17, 0x18, 0x1a),
		sidebar:    ui.RGB(0x17, 0x18, 0x1a),
		panel:      ui.RGB(0x1e, 0x1f, 0x22),
		border:     ui.RGB(0x2b, 0x2d, 0x31),
		active:     ui.RGB(0x16, 0x28, 0x3d),
		hover:      ui.RGB(0x26, 0x28, 0x2c),
		accent:     ui.RGB(0x36, 0xad, 0x6a), // #36ad6a
		accentTint: ui.RGB(0x1b, 0x2b, 0x22), // #1b2b22
	}
}

// rootState 是一个已打开的集合根，以及它自己的过滤词、环境与选中项。
type rootState struct {
	col    *explorer.Collection
	env    string
	filter string
	file   string

	// 标签按集合根各存一份：切到别的根再切回来，标签与当前标签都还在
	// （m.tabs 永远只是「活动根的那一份」，切换就是整份换掉）。
	tabs     []*tabState
	curTab   int
	overview bool
	tabSeq   int
	// tabScroll 是页签条的横向偏移，换根回来时接着上次的位置。
	tabScroll float32
	// tabFollowed 是上一次自动跟随的标签下标：只在换标签时把条滚过去，
	// 用户自己横扫 / 点箭头之后不再每帧把他拽回来。
	tabFollowed int
	// tabStrip 是页签条的滚动状态；tabOverflow 表示它装不下（决定左右箭头是否出现）。
	tabStrip     *ui.ScrollState
	tabOverflows bool
}

// kvRow 是一行可编辑的键值（查询参数 / 请求头 / 环境变量）：指针存进切片，
// 文本输入绑定 &row.name 时指针稳定，删行/加行不会重置其它行的编辑器状态。
type kvRow struct {
	enabled bool
	name    string
	value   string
	desc    string
	typ     string // 仅 multipart 表单使用：text（默认）| file
	secret  bool   // 仅环境变量：值写进 *.secrets.yml
}

// fieldRow 是请求体字段表的一行（body.fields 是文档生成的真源）。
type fieldRow struct {
	name     string
	typ      string
	required bool
	example  string
	desc     string
}

// requestDraft 是一条 http 请求的编辑草稿；保存时写回集合文件。
type requestDraft struct {
	file   string
	method string
	url    string

	params  []*kvRow
	headers []*kvRow
	form    []*kvRow
	fields  []*fieldRow

	bodyType  string
	bodyRaw   string
	bodyField string // raw | data：保留 Bruno 的 data 字段形态

	authType  string
	authUser  string
	authPass  string
	authToken string
	authKey   string
	authValue string
	authIn    string

	summary string
	docs    string

	// base 是打开时磁盘内容的散列：保存前用它发现「别人也改了这个文件」。
	base string
	// conflict 为真表示上次保存撞上了外部改动，界面要给出「重新加载 / 覆盖」。
	conflict bool

	dirty bool
	err   string
	note  string
}

// workspaceUI 是弹窗与设置面板的输入状态（不参与集合语义，只是界面状态）。
type workspaceUI struct {
	newReqOpen bool
	newName    string
	newMethod  string
	newURL     string

	// 集合公共参数弹窗：默认请求头与默认认证的表单。
	defaultsOpen bool
	defHeaders   []*kvRow
	defAuthType  string
	defAuthUser  string
	defAuthPass  string
	defAuthToken string
	defAuthKey   string
	defAuthValue string
	defAuthIn    string
	defNote      string

	// Cookie Jar 弹窗：打开时从共享 jar 读一次快照。
	cookieOpen bool
	cookieRows []send.Cookie
	cookieNote string

	// 请求历史弹窗：打开时从磁盘读一次，之后只读缓存。
	historyOpen   bool
	historyQuery  string
	historyRows   []history.Entry
	historyNote   string
	historyDetail time.Time // 展开详情的那条（按时间认，零值表示都没展开）

	// 导出弹窗：格式、勾选范围与结果提示。
	exportOpen   bool
	exportFormat string
	exportRows   []exportRow
	exportNote   string

	// 环境变量表的列表状态：用 ui.List 才能拖拽重排。
	envList ui.ListState

	// 回收站：勾选、以及「彻底删除」是否已经点过一次（二次确认）。
	trashSel   map[string]bool
	trashArmed bool

	// 集合运行器弹窗。
	runOpen bool

	// 运行规则弹窗：某条请求的提取 / 断言（存进请求文件的 run: 键）。
	rulesOpen    bool
	rulesFile    string
	rulesName    string
	rulesNote    string
	ruleExtracts []runner.Extract
	ruleAsserts  []runner.Assert

	// Mock 弹窗：开关与监听地址。
	mockOpen     bool
	mockOn       bool
	mockAddrText string

	// Mock 单条规则弹窗：状态码 / 延迟 / 覆盖响应体。
	mockRuleOpen   bool
	mockRuleFile   string
	mockRuleName   string
	mockRuleStatus string
	mockRuleDelay  string
	mockRuleBody   string
	mockRuleNote   string

	// 导入弹窗：0 = Postman 集合 JSON，1 = 另一个 OpenCollection 集合目录。
	importOpen    bool
	importMode    int
	importText    string
	importDir     string
	importBase    string
	importFolder  string
	importFolders []string
	importNote    string
	importErr     string
	newErr        string
	newFolder     string // 从某个分组右键新建时指定落点
	// savingScratch 表示这次弹窗是「临时请求另存为」，保存成功后关掉临时标签。
	savingScratch bool

	renameOpen   bool
	renameName   string
	renameFile   string
	renameFolder string // 非空表示这次重命名的是分组目录（相对集合根）
	renameErr    string

	folderOpen   bool
	folderName   string
	folderParent string // 新分组落在哪个目录下（空=集合根）
	folderErr    string

	folderDeleteOpen bool
	folderDeleteDir  string
	folderDeleteName string

	settingsOpen    bool
	settingsSection int
	paletteOpen     bool
	trashOpen       bool
	curlOpen        bool
	curlText        string
	curlErr         string
	curlFolder      string   // 导入到哪个分组（"(集合根)" 表示根）
	curlName        string   // 可选：导入后的显示名
	curlFolders     []string // 打开弹窗时算好的分组下拉选项
	trashRows       []explorer.TrashItem
	trashMsg        string
	paletteQuery    string
	themePick       int
	imeDetail       string
	dirLine         string
	envLine         string
	collectionName  string
	timeoutText     string
	proxyText       string
	clientCertText  string
	clientKeyText   string
	follow          bool
	insecure        bool
	persistCookies  bool
	closeToTray     bool
	sealSecrets     bool
	// 检查更新：结果文本与（有新版本时的）发布页地址。
	// 请求级设置弹窗的表单值（三态项 0=默认 1=开 2=关）。
	reqSetOpen        bool
	reqSetTimeout     string
	reqSetMaxRedirect string
	reqSetFollow      int
	reqSetInsecure    int
	reqSetEncode      int
	reqSetErr         string
	reqSetNote        string

	// 自动保存（外观分区）：开关与停顿秒数。
	autoSave    bool
	autoSaveSec string
	// shortcutsOpen 是 Ctrl+/ 的快捷键卡。
	shortcutsOpen bool

	updateNote string
	updateURL  string
	updateBusy bool
	updateRepo string
	splitSide  int // 0 = 上下，1 = 左右
	// 文档页视图：默认全宽预览；docSplitPick 是设置表单里的 0/1。
	docSplit     bool
	docSplitPick int
	scalePick    int // 界面缩放在 scaleOptions 里的下标
	envs         []string
	envPick      string

	// MCP 服务的设置（独立弹窗，与软件设置分开）：开关 / 监听地址 / 令牌 / 工作区授权。
	mcpOpen bool
	// mcpOrigins 是弹窗里的来源白名单文本（逗号分隔），落盘前才拆成数组。
	mcpOrigins string
	// mcpAuditRows 是面板里展示的最近调用（打开面板时载入，避免每帧读文件）。
	mcpAuditRows []appmcp.AuditEntry
	// mcpConsentOpen 是写操作确认框的开关。
	mcpConsentOpen bool
	// mcpConsentOff 是弹窗里「写操作不确认」的镜像（零值 = 需要确认）。
	mcpConsentOff bool
	mcpEnabled    bool
	mcpReadOnly   bool
	// mcpGroupOn 按 appmcp.Groups 的顺序记「哪一组开着」。
	mcpGroupOn []bool
	mcpAddr    string
	mcpToken   string
	mcpAllow   []mcpAllowRow

	pendingDir string
	initOpen   bool
	pathOpen   bool
	pathText   string
	openErr    string

	envRenameOpen bool
	envRenameName string
	envDeleteOpen bool

	envOpen        bool
	envNameEdit    string
	envSel         string
	envRows        []*kvRow
	envSecretCount int
	envSecretLine  string
	envErr         string
	newEnvOpen     bool
	newEnvName     string
	newEnvErr      string
}

// Model 一次运行的界面状态。
type Model struct {
	// 多个集合根：roots 按打开顺序排，col 始终是 active 指向的那个（界面只读 col）。
	roots  []*rootState
	active int
	col    *explorer.Collection

	envName string
	vars    map[string]string

	// 多个标签：tabs 按打开顺序排，curTab 是当前展示的那个（-1 表示一个都没开）。
	tabs      []*tabState
	curTab    int
	tabSeq    int
	tabActive int
	// 页签条横向滚动的两个位置：tabScrollX 是每帧从 ScrollState 抄回来的当前值，
	// tabScrollRestore 时用 tabScrollWant 再滚回去（换根或换集合后）。
	tabScrollX       float32
	tabScrollWant    float32
	tabScrollRestore bool
	// tabFollowed 是上一次自动跟随的标签下标：只在换标签时把条滚过去；
	// 用户自己横扫或点箭头之后，不再每帧把他拽回当前标签。
	tabFollowed int
	// splitDrag 是请求 / 响应之间那条透明分隔条的拖动状态。
	splitDrag splitDrag
	dragSplit ui.DragState
	// tabStrip 是页签条的滚动状态；tabOverflow 表示它装不下（决定左右箭头是否出现）。
	tabStrip     *ui.ScrollState
	tabOverflows bool
	// tabMenuOpen 是右侧「⋯」的展开状态。
	tabMenuOpen bool
	// 自动保存：autoSaveAt 是「停手后第几秒落盘」，pending 表示还欠一次写。
	autoSaveAt      time.Time
	autoSavePending bool
	autoSaveDocAt   time.Time

	theme  ui.Theme
	pal    palette
	split  ui.SplitterState
	rsplit ui.SplitterState
	hsplit ui.SplitterState
	// tree 是集合树：行由 rebuildTree 摊平，展开状态、过滤词、集合根本身变化时重建。
	tree      ui.TreeState
	rows      []treeRow
	expanded  map[string]bool
	treeDirty bool
	sideC     ui.C // 只在本帧声明期有效：treeContent 需要上下文

	// reqMenuOpen 是地址栏「更多操作」菜单；codeOpen 是代码生成弹窗。
	reqMenuOpen bool
	codeOpen    bool
	codeLang    int
	codeText    string

	// 拖拽悬停展开：指针停在折叠分组上超过 dragExpandDelay，就把该分组展开。
	// 记录的是树行键与起始时间，真正展开在下一帧的 flushHoverExpand 里做。
	dragHoverKey   string
	dragHoverSince time.Time
	dragExpandKey  string

	// 选中项：行的数量要等 Tree 画完才更新，新行第一帧选不上，留一次重试。
	selectFile    string
	selectPending bool
	// selKey 是当前高亮的行（Tree 的选中由应用决定，组件只按 TreeNode.Selected 上色）。
	selKey             string
	menuRoot           int
	menuEntry          string
	envMenuOpen        bool
	collectionMenuOpen bool
	menuOpen           bool
	tab                detailTab

	filter    string
	shown     []*explorer.Entry
	shownFor  string
	shownOnce bool
	countText string
	subtitle  string

	// entryFile 是当前右侧展示的条目；pendingSelect 是重扫后要选中的路径，
	// pendingKey 是同一件事的树行键（分组排序后选回分组用）。
	entryFile     string
	pendingSelect string
	pendingKey    string
	createErr     string

	tables  requestTables
	respTab int
	rawResp bool
	// jsonSel 是响应正文（JSON / 原始）的鼠标选区：Viva 的文字没有原生选区，自绘。
	jsonSel selView
	// docsEditing 是请求「文档」分段是否处于编辑态：默认预览，点一下才编辑（按标签记忆）。
	docsEditing bool

	docs docState
	// 文档预览用到的图片：按路径缓存解码结果（见 docimage.go）。
	docImages map[string]docImageEntry
	// 文档源码区的控件身份：Ctrl/Cmd+V 只在它拿着焦点时接管（见 docimage.go）。
	docCodeID gfx.ID
	editor    docEditor
	// 文档源码区的代码编辑器（行号 + 当前行高亮）；正文真源仍是 m.editor.text。
	docCode     codeeditor.Editor
	docText     *text.Editor
	docCodeFile string
	docMirror   string
	// docHandlers 是源码区自己声明的控件（按键、指针、身份）；不用
	// ui/codeeditor.View 的 wrapper，原因见 docSourcePane。
	docHandlers ui.Handlers
	// docCodeRect 是源码区上一帧的实拍矩形（平台文本层按它裁剪，见 docCodeInput）。
	docCodeRect gfx.Rect
	drafts      map[string]string
	req         requestDraft
	scratch     bool // 当前标签是不是未落盘的临时请求
	overview    bool // 正在看集合概览（固定的「集合」页）
	settings    settings.Settings
	// 树的排序（磁盘顺序 / 名称升降），跟着偏好走。
	sortMode     string
	sortMenuOpen bool

	// 页签拖拽重排：tabDrag 是这一次手势，tabSlotIDs 记录本帧每个槽位的点击身份
	// （拖拽回调只给身份，要靠它反查槽位）。
	tabDrag    ui.DragState
	tabSlotIDs []gfx.ID       // 槽位 → 主点击身份（测试与调试用）
	tabSlots   map[gfx.ID]int // 任何声明过的点击身份 → 槽位（标题、背景各一份）

	// 托盘与窗口：托盘挂上之后才有 window/app 的用法，未挂上也各自有 nil 判断。
	app         *app.Application
	window      trayWindow
	trayItem    statusItem
	trayEnabled bool
	trayNote    string

	ui workspaceUI
	// mcpConsent 是服务端与界面之间传递「写操作确认」的桥。
	mcpConsent mcpConsent
	// mcp 是内嵌 MCP 服务的运行状态（底栏状态点 / 设置面板）；服务器本体在 mcp.go。
	mcp        mcpRuntime
	mcpHost    mcpHost
	mock       mockRuntime
	mockServer *appmock.Server
	run        runState
	runCh      chan runner.Result
	runCancel  context.CancelFunc

	resp     send.Outcome
	hasResp  bool
	sending  bool
	status   string
	copyNote string
	reason   string
	elapsed  string
	size     string
	bodyText string
	missing  string
	failText string
	statusOK bool

	// 响应示例：当前请求存下来的快照（examples/*.yml），与最近一次真实响应分开存。
	// exSel 为空表示正在看实时响应；看示例时 live 不被覆盖，切回来用它还原。
	exRows []explorer.Example
	exSel  string
	// respMenuOpen 是右上角「保存响应」下拉的展开状态。
	respMenuOpen bool
	live         send.Outcome

	// 集合级公共参数（清单 defaults 段）：默认请求头 + inherit 认证的来源。
	defaults collection.Defaults

	// gRPC 请求的执行状态（见 grpcrun.go）。
	grpcMetaRows []*kvRow
	grpcMessage  string
	grpcStatus   string
	grpcBody     string
	grpcNote     string
	grpcTime     time.Duration
	grpcSending  bool
	grpcDirty    bool
	grpcResults  chan grpcOutcome

	// 响应正文的查找条状态（见 find.go）。
	find findState

	// 指针位置与含变量的输入框区域（见 varhover.go）。
	hover varHover

	// 撤销 / 重做：整份草稿的快照栈（见 undo.go）。
	undoStack []requestDraft
	redoStack []requestDraft
	editSeq   int
	snapSeq   int
	lastSnap  time.Time

	// gen 是「当前选中请求」的代号：换请求后，旧请求在途的响应作废。
	gen         int
	results     chan sendResult
	invalidator app.Invalidator
}

// New 建模型：列表行名直接取自集合，过滤后仍按「方法 + 名称」朗读。
func New(col *explorer.Collection, envName string, vars map[string]string) *Model {
	if col == nil {
		// 空状态：窗口照常打开，首屏只提供「打开集合」。
		col = &explorer.Collection{Name: "未打开集合"}
	}
	m := &Model{
		col:      col,
		envName:  envName,
		vars:     vars,
		theme:    ui.DarkTheme(),
		results:  make(chan sendResult, 1),
		drafts:   map[string]string{},
		settings: settings.Load(),
	}
	m.mcpHost.errs = make(chan string, 1)
	m.grpcResults = make(chan grpcOutcome, 1)
	if col.Dir != "" {
		m.defaults = explorer.LoadDefaults(col.Dir)
	}
	m.roots = []*rootState{{col: col}}
	m.active = 0
	m.col = col
	m.sortMode = m.settings.SortMode
	m.curTab = -1
	m.tabActive = -1
	m.ui.newMethod = "GET"
	m.find.jump = -1
	m.ui.newEnvName = "dev"
	// 没显式指定环境时沿用该集合上次的选择（偏好按集合根记）。
	if m.envName == "" && m.col.Dir != "" {
		m.envName = m.settings.Envs[m.col.Dir]
		if m.envName != "" {
			if loaded, err := explorer.LoadEnv(m.col.Dir, m.envName); err == nil {
				m.vars = loaded
			}
		}
	}
	// 缩放要在建主题与布局之前定下来：主题度量与布局常量都由它算。
	applyUIScale(m.settings.UIScalePercent())
	m.applyTheme()
	m.ui.imeDetail = ime.Detect().Detail
	m.split.Pos = px(260)
	m.rsplit.Pos = px(260)
	m.hsplit.Pos = px(460)
	m.overview = true
	m.menuRoot = -1
	m.expanded = map[string]bool{}
	m.tree.Node = m.treeNode
	m.tree.Label = m.treeLabel
	m.tree.Key = m.treeKey
	m.tree.CanDrop = m.canDrop
	m.tree.RowHeight = px(30)
	m.tree.Indent = px(16)
	m.refreshShown()
	m.refreshEnvs()
	m.ui.envPick = m.envName
	m.syncSettingsUI()
	// 上次启用过就随窗口一起起来（未启用时是空操作）。
	m.startMCP()
	m.startMock()
	return m
}

// config 是窗口与页面的接线：Run 与无头测试共用，免得两处配置漂移。
// pageConfig 是页面自己的配置（不含窗口参数）。
func (m *Model) pageConfig() ui.PageConfig {
	return ui.PageConfig{View: m.Page, Theme: &m.theme, Background: &m.pal.bg}
}

// config 把页面与窗口参数合成一个 app.Config（无头用例仍按这条路起窗口）。
func (m *Model) config() app.Config {
	return ui.Configure(ui.NewPage(m.pageConfig()), m.windowConfig())
}

// windowConfig 是窗口参数：标题、尺寸、无边框标题栏、托盘关闭拦截等。
func (m *Model) windowConfig() app.Config {
	return app.Config{
		Title:     "API-DOC-GO",
		Class:     "api-doc-viva",
		Width:     1180,
		Height:    760,
		MinWidth:  900,
		MinHeight: 560,
		// 顶栏融合：不要系统标题栏，工具条自己就是标题栏（拖动区在 toolbar 里声明）。
		// viva 在 Windows / Linux 上自绘最小化 / 最大化 / 关闭，并给无边框窗口补阴影、
		// 圆角与边缘拖拽缩放，所以不需要我们自己画这些。
		WindowMode:     app.WindowCustomTitlebar,
		TitlebarHeight: titlebarHeight,
		WindowControls: m.windowControls,
		Invalidator:    &m.invalidator,
		OnWake:         m.collect,
		// 变量悬停要知道指针在哪：输入框自己不会把移动事件交出来（见 varhover.go）。
		OnPointer: m.trackPointer,
		// 文档源码区的控件是 RoleGroup（见 docSourcePane），页面级 provider 才会被问到。
		TextInput: m.docTextService,
		// 任务栏 / Alt+Tab 的图标：与托盘同一张画，画在代码里，少一份资源文件。
		Icon: appIcon(256),
		// 关到托盘：不调 allow 就等于「窗口不关」；托盘没挂上时照常关。
		OnCloseRequest: func(allow func()) {
			if m.shouldHideOnClose() {
				m.hideWindow()
				return
			}
			allow()
		},
	}
}

// windowControls 是标题栏右端那组窗口按钮的配色：每帧调用，跟着主题走。
func (m *Model) windowControls() app.WindowControlsStyle {
	return app.WindowControlsStyle{
		Foreground:      m.theme.MutedForeground,
		Hover:           m.theme.Hover,
		Pressed:         m.theme.Border,
		Close:           m.theme.Danger,
		CloseForeground: m.theme.DangerForeground,
		Minimize:        "最小化",
		Maximize:        "最大化",
		Restore:         "还原",
		CloseName:       "关闭",
	}
}

// Run 打开窗口并阻塞到关闭；后台发送完成后由 Invalidator 唤醒重绘。
//
// 用 RunApplication 而不是 RunCanvas 是为了拿到 *app.Application：
// 托盘（AddStatusItem）只挂在应用对象上。
func (m *Model) Run() error {
	return app.RunApplication(func(application *app.Application) error {
		m.app = application
		window, err := ui.OpenWindow(application, m.windowConfig(), m.pageConfig())
		if err != nil {
			return err
		}
		m.window = window
		if m.ui.closeToTray {
			m.installTray(application)
		}
		return nil
	})
}

// dragExpandDelay 是拖拽时停在折叠分组上多久后自动展开。
const dragExpandDelay = 600 * time.Millisecond

// canDrop 接受：条目换上级、条目同级排序、分组换父级、分组同级排序。
// 集合根不能拖，跨集合根的拖放也不接。
func (m *Model) canDrop(from, to int, where ui.TreeDrop) bool {
	ok := m.dropAllowed(from, to, where)
	// Tree 会对同一行依次试 Inside / After / Before：只有「放进分组」这一个落点
	// 参与悬停展开，别的落点既不算命中也不该把计时清掉。
	if where == ui.TreeDropInside {
		if ok {
			m.noteDragHover(to)
		} else {
			m.dragHoverKey, m.dragHoverSince = "", time.Time{}
		}
	}
	return ok
}

// noteDragHover 记录指针停在折叠分组上的时长；到点后交给下一帧展开。
func (m *Model) noteDragHover(index int) {
	row := &m.rows[index]
	if !row.branch || row.expanded {
		m.dragHoverKey, m.dragHoverSince = "", time.Time{}
		return
	}
	now := time.Now()
	if m.dragHoverKey != row.key {
		m.dragHoverKey, m.dragHoverSince = row.key, now
		// 指针停住时没有新事件，稍晚一点唤醒：给到点判定留出余量，
		// 定时器只负责叫醒窗口，真正展开仍在界面线程做。
		time.AfterFunc(dragExpandDelay+100*time.Millisecond, m.invalidator.Invalidate)
		return
	}
	if now.Sub(m.dragHoverSince) < dragExpandDelay {
		return
	}
	m.dragExpandKey = row.key
	m.dragHoverKey, m.dragHoverSince = "", time.Time{}
	m.invalidator.Invalidate()
}

// flushHoverExpand 在声明树之前展开到点的分组（绘制期只读，不在回调里改状态）。
func (m *Model) flushHoverExpand() {
	if m.dragExpandKey != "" {
		key := m.dragExpandKey
		m.dragExpandKey = ""
		m.expanded[key] = true
		m.treeDirty = true
		m.syncTree()
	}
	if !m.tree.Dragging() {
		m.dragHoverKey, m.dragHoverSince = "", time.Time{}
	}
}

// dropAllowed 是 canDrop 的纯判定，不碰拖拽悬停状态。
// 同一集合根里可以换上级、排序；跨集合根只接「放进分组 / 集合根」，
// 因为另一棵树里的序号与父目录都不在当前集合里，排序没有意义。
func (m *Model) dropAllowed(from, to int, where ui.TreeDrop) bool {
	// 名称排序下禁用拖拽：拖完顺序还是按名字排，用户会以为「拖不动」，
	// 那不如一开始就不接——要排序就先切回磁盘顺序。
	if m.sortMode != "" {
		return false
	}
	if from < 0 || to < 0 || from >= len(m.rows) || to >= len(m.rows) || from == to {
		return false
	}
	source, target := m.rows[from], m.rows[to]
	if source.kind == nodeRoot {
		return false
	}
	if source.root < 0 || source.root >= len(m.roots) || target.root < 0 || target.root >= len(m.roots) {
		return false
	}
	same := source.root == target.root
	switch source.kind {
	case nodeEntry:
		switch where {
		case ui.TreeDropInside:
			folder, ok := m.folderTarget(to)
			if !ok {
				return false
			}
			return !same || folder != requestDir(source.entry.File)
		case ui.TreeDropBefore, ui.TreeDropAfter:
			return same && target.kind == nodeEntry &&
				requestDir(target.entry.File) == requestDir(source.entry.File)
		}
	case nodeFolder:
		switch where {
		case ui.TreeDropInside:
			var dest string
			switch target.kind {
			case nodeFolder:
				dest = target.folder.Dir
			case nodeRoot:
				dest = ""
			default:
				return false
			}
			if !same {
				return true
			}
			// 移进自己或自己的子孙会把整棵子树弄丢。
			if dest == source.folder.Dir || strings.HasPrefix(dest, source.folder.Dir+"/") {
				return false
			}
			return parentDir(source.folder.Dir) != dest
		case ui.TreeDropBefore, ui.TreeDropAfter:
			return same && target.kind == nodeFolder &&
				parentDir(target.folder.Dir) == parentDir(source.folder.Dir)
		}
	}
	return false
}

// folderTarget 把一行翻译成可放入的分组目录（集合根为空串）。
func (m *Model) folderTarget(index int) (string, bool) {
	if index < 0 || index >= len(m.rows) {
		return "", false
	}
	switch m.rows[index].kind {
	case nodeRoot:
		return "", true
	case nodeFolder:
		return m.rows[index].folder.Dir, true
	}
	return "", false
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
	m.treeDirty = true
}

// filterEntries 按名称 / 分组 / URL / 概述做大小写不敏感匹配。
func filterEntries(all []*explorer.Entry, filter string) []*explorer.Entry {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if needle == "" {
		return all
	}
	out := make([]*explorer.Entry, 0, len(all))
	for _, entry := range all {
		if entryMatches(entry, needle) {
			out = append(out, entry)
		}
	}
	return out
}

// entryMatches 是过滤词对单个条目的判定（名称 / 分组 / 地址 / 概述，大小写不敏感）。
func entryMatches(entry *explorer.Entry, needle string) bool {
	haystack := strings.ToLower(entry.Name + " " + entry.Group + " " + entry.URL + " " + entry.Summary)
	return strings.Contains(haystack, needle)
}

// applyPendingSelect 在重扫后把树的选中项落回关心的条目。
// Tree 的行数要等这一帧画完才更新，新行第一次选不上，所以必要时再要一帧。
func (m *Model) applyPendingSelect() {
	if m.pendingKey != "" {
		m.syncTree()
		index := m.rowOfKey(m.pendingKey)
		if index < 0 {
			m.pendingKey = ""
			return
		}
		m.tree.Select(index)
		if m.tree.Selected() != index {
			m.invalidator.Invalidate()
			return
		}
		m.pendingKey = ""
		return
	}
	if m.pendingSelect == "" {
		return
	}
	m.syncTree()
	index := m.rowOfEntry(m.pendingSelect)
	if index < 0 {
		m.pendingSelect = ""
		return
	}
	m.tree.Select(index)
	if m.tree.Selected() != index {
		m.invalidator.Invalidate()
		return
	}
	file := m.pendingSelect
	m.pendingSelect = ""
	if entry := m.entryByFile(file); entry != nil {
		m.openEntry(entry)
	}
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
		m.tab = tabParams
	}
	// 换了一条请求：上一次的响应与示例都不再属于它，在途结果也作废。
	m.gen++
	m.sending = false
	m.resetResponse()
	m.loadExamples(entry)
	m.tables = requestTables{
		metaLine: entry.File + " · " + orDash(entry.Group),
		docsKey:  entry.File + "#docs",
	}
	m.loadRequestDraft(entry)
	if entry.Kind == explorer.KindHTTP {
		// 上次没保存的改动优先：先装磁盘内容，再用草稿盖回去。
		m.restoreRequestDraft(entry)
	}
	if entry.Kind != explorer.KindHTTP {
		m.tab = tabRequest
		m.fillGRPC(entry)
		return
	}
	block := entry.Payload.HTTP
	m.tables.params = kvRows(block.Params)
	m.tables.headers = kvRows(block.Headers)

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
	m.loadGRPCFromEntry(entry)
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
	status := ""
	if draft, ok := m.drafts[entry.File]; ok {
		text = draft
	} else if m.col != nil {
		// 跨进程恢复：内存里没有，就看配置目录里的草稿。
		if m.restoreDocDraft(entry) {
			text, status = m.editor.text, "已恢复上次未保存的改动（Ctrl+S 保存）"
		}
	}
	m.editor = docEditor{
		file:       entry.File,
		text:       text,
		saved:      entry.Content,
		front:      entry.Front,
		metaLine:   entry.File + " · " + orDash(entry.Group),
		previewKey: entry.File + "#preview",
		status:     status,
	}
}

// docDirty 报告文档有没有未保存的改动。
func (m *Model) docDirty() bool { return m.editor.text != m.editor.saved }

// createDoc 新建一篇 Markdown（客户端同款 docs/ 默认位与 -2 去重），随后重扫并选中它。
// 由 c.After 调用：它会改变正在声明的列表。
func (m *Model) createDoc() {
	path, err := explorer.CreateDoc(m.col.Dir, newDocName)
	if err == nil {
		var reloaded *explorer.Collection
		reloaded, err = explorer.Load(m.col.Dir)
		if err == nil {
			m.installCol(reloaded)
		}
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
	// 存进集合了，这份草稿可以扔了。
	drafts.Delete(m.draftDir(), drafts.Doc, m.col.Dir, m.editor.file)
	if entry := m.entryByFile(m.editor.file); entry != nil {
		entry.Content = m.editor.text
	}
	m.docs.key = "" // 预览按新正文重排
}

// start 把发送放到后台：界面线程只做一次快照，结果经通道交回 OnWake。
func (m *Model) start(entry *explorer.Entry) {
	payload := entry.Payload
	if m.scratch {
		payload = m.scratchFile()
	}
	if m.sending || payload == nil {
		return
	}
	m.resetResponse()
	m.sending = true
	plan := m.planFor(payload)
	tab := m.activeTab()
	if tab == nil {
		m.sending = false
		return
	}
	tabID, gen := tab.id, m.gen
	// 历史记录在后台线程写好再交回结果：界面线程只管显示，不碰文件。
	method := strings.ToUpper(strings.TrimSpace(payload.HTTP.Method))
	if method == "" {
		method = "GET"
	}
	record := history.Entry{Method: method, Dir: m.col.Dir, Env: m.envName}
	if !m.scratch && entry != nil {
		record.Name, record.File = entry.Name, entry.File
	}
	go func() {
		out := send.Do(context.Background(), plan)
		record.URL = out.URL
		record.Status, record.Duration, record.Size, record.Err = out.Status, out.Duration.Milliseconds(), out.Size, out.Err
		// 请求快照：敏感头打码后再进历史文件。
		for _, header := range send.MaskHeaders(out.ReqHeaders) {
			record.Headers = append(record.Headers, history.Field{Name: header.Name, Value: header.Value})
		}
		for _, param := range out.ReqParams {
			record.Params = append(record.Params, history.Field{Name: param.Name, Value: param.Value})
		}
		record.Body, record.BodyTruncated = out.ReqBody, out.ReqBodyTruncated
		if record.URL != "" {
			_ = history.Append(record)
		}
		m.results <- sendResult{tab: tabID, gen: gen, out: out}
		m.invalidator.Invalidate()
	}()
}

// planFor 按当前偏好吃一次快照造发送计划：单条发送与集合运行器共用一份，
// 免得两边对超时 / 重定向 / 代理 / 证书的设置各写一遍。
func (m *Model) planFor(payload *collection.RequestFile) send.Plan {
	follow := m.settings.FollowRedirects()
	dir := ""
	if m.col != nil {
		dir = m.col.Dir
	}
	return send.Plan{
		File:            payload,
		Vars:            m.vars,
		Timeout:         time.Duration(m.settings.TimeoutSec) * time.Second,
		MaxBody:         1 << 20,
		Proxy:           m.settings.Proxy,
		ClientCert:      m.settings.ClientCert,
		ClientKey:       m.settings.ClientKey,
		Dir:             dir,
		Defaults:        m.defaults,
		FollowRedirects: &follow,
		InsecureSSL:     m.settings.InsecureSSL,
	}
}

// resetResponse 清掉上一次的结果，避免发送中看到旧状态。
func (m *Model) resetResponse() {
	m.resp = send.Outcome{}
	m.hasResp = false
	m.status, m.reason, m.elapsed, m.size = "", "", "", ""
	m.bodyText, m.missing, m.failText, m.statusOK = "", "", "", false
	m.copyNote = ""
}

// collect 是 OnWake：在重绘前把后台结果搬成界面文案（这里算一次，绘制期只读）。
// 结果属于哪个标签就填哪个标签：用户可能在等待时切走了。
func (m *Model) collect() {
	var result sendResult
	select {
	case result = <-m.results:
	default:
		return
	}
	index := m.findTabID(result.tab)
	if index < 0 {
		return // 标签已经关掉
	}
	tab := m.tabs[index]
	if tab.gen != result.gen {
		return // 结果属于已经换掉的那条请求
	}
	if index == m.curTab {
		m.fillResponse(result.out)
		m.storeTab()
		return
	}
	// 非活动标签：临时把它的状态装进来填结果，再切回当前标签。
	saved := &tabState{}
	m.captureInto(saved)
	m.applyFromTab(tab)
	m.fillResponse(result.out)
	m.captureInto(tab)
	m.applyFromTab(saved)
}

// fillResponse 把一次发送结果翻成状态行与正文文案。
// 同时留一份 live：切到示例再切回来时用它还原（示例不覆盖真实响应）。
func (m *Model) fillResponse(out send.Outcome) {
	m.live = out
	m.exSel = ""
	m.showOutcome(out)
}

// showOutcome 把一份结果（实时或示例）铺成界面文案。
func (m *Model) showOutcome(out send.Outcome) {
	m.resp = out
	m.hasResp, m.sending = true, false
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
	m.tables.respHeaders = out.Headers
	m.tables.json, m.tables.jsonFolds = parseJSON(out.Body)
	m.tables.jsonFolded = make(map[int]bool)
	if len(out.Missing) > 0 {
		// 说清楚后果：这些占位符在当前环境里没有定义，发送时按原文（{{名字}}）发出。
		m.missing = "未解析变量：" + strings.Join(out.Missing, ", ") +
			"（当前环境没有定义，发送时保留了原文）"
	}
}

// previewHeight 返回一段 Markdown 在 width 下的高度；源文本、目录或宽度变了才重新排版。
//
// base 是这段文本所在目录（相对集合根）：图片按它解析相对路径，所以它也是缓存键的一部分。
func (m *Model) previewHeight(c ui.C, key, source, base string, width float32) float32 {
	width = max(width, 80)
	if m.docs.key == key && m.docs.source == source && m.docs.base == base && m.docs.width == width {
		return m.docs.height
	}
	m.docs.key, m.docs.source, m.docs.base, m.docs.width = key, source, base, width
	m.docs.doc.Parse(source)
	m.docs.height = m.docs.view.Layout(c.Canvas(), components.MarkdownStyle{
		Size: m.theme.Metrics.FontSizeM - 1,
		// 图片由应用解析文件，视图自己从不读盘（见 docimage.go）。
		Image: func(destination string) (gfx.Bitmap, bool) {
			return m.docImage(base, destination)
		},
	}, &m.docs.doc, width)
	return m.docs.height
}

// contentWidth 是详情页可用的文字宽度：整窗宽减去侧栏、分隔条与两侧内边距。
func (m *Model) contentWidth(c ui.C) float32 {
	return c.Bounds().Width - m.split.Pos - splitterThickness - 2*detailPadding
}

// paneWidth 是请求栏卡片内部可用的文字宽度（Markdown 预览按它折行）：
// 左右分栏取左栏宽，上下分栏取整幅，再减去卡片内边距与 1px 描边。
func (m *Model) paneWidth(c ui.C) float32 {
	chrome := 2*panePadding + px(2)
	if m.settings.SplitSideBySide {
		return m.hsplit.Pos - chrome
	}
	return m.contentWidth(c) - chrome
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
