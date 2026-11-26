package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerVersion 是 MCP 服务自报的版本（与客户端同样单独维护）。
const ServerVersion = "0.1.0"

// DefaultAddr 是没给地址时的监听地址：只绑回环，漏传地址不该把集合暴露到局域网。
const DefaultAddr = "127.0.0.1:2026"

// Config 是内嵌服务的启动配置。
type Config struct {
	// Addr 监听地址：空值用 DefaultAddr（只本机可连）。
	Addr string
	// Path 挂载路径，留空 = /mcp。
	Path string
	// Token 非空时要求 `Authorization: Bearer <token>`。
	Token string
	// AllowOrigins 允许的浏览器来源（DNS rebinding 防护）；空 = 拒绝一切带 Origin 的请求。
	AllowOrigins []string
	Logger       *log.Logger
	// Service 是工具的业务实现（必填）。
	Service *Service
	// ReadOnly 只注册读工具（不发请求、不写磁盘）。
	ReadOnly bool
	// Confirm 在写工具执行前被调用；返回错误即拒绝执行（应用可以据此弹窗让用户确认）。
	Confirm func(ctx context.Context, tool string, in any) error
	// Groups 是要暴露的工具组；空 = 全部。
	Groups []string
	// Audit 记录每次工具调用；nil 表示不记。
	Audit *Audit
}

// NewServer 装配 MCP 服务器并注册全部工具。
func NewServer(cfg Config) (*sdk.Server, error) {
	if cfg.Service == nil {
		return nil, errors.New("mcp: 缺少 Service")
	}
	server := sdk.NewServer(&sdk.Implementation{
		Name:    "api-doc-go-viva",
		Title:   "api-doc-go 接口集合（验证版）",
		Version: ServerVersion,
	}, nil)
	// Confirm 必须一起传下去：漏了它，写工具的「先问用户」就在注册这一步被丢掉了，
	// 界面上永远不会弹确认框（写操作直接落盘）。
	registerTools(server, cfg.Service, ToolOptions{
		ReadOnly: cfg.ReadOnly,
		Groups:   cfg.Groups,
		Audit:    cfg.Audit,
		Confirm:  cfg.Confirm,
	})
	return server, nil
}

// ToolOptions 是注册工具时的开关：只读模式、勾选的分组、审计器。
type ToolOptions struct {
	// ReadOnly 为真时只注册读工具（不发请求、不写磁盘）。
	ReadOnly bool
	// Confirm 在写工具执行前被调用；返回错误即拒绝执行。
	Confirm func(ctx context.Context, tool string, in any) error
	// Groups 是要注册的分组；空 = 全部（只读模式下再按 ReadOnly 过滤）。
	Groups []string
	Audit  *Audit
}

// allows 判断某个分组（含写标记）要不要注册。
func (o ToolOptions) allows(group string, write bool) bool {
	if write {
		if o.ReadOnly {
			return false
		}
		// 写工具同时属于「写」分组：任何一组里出现 write 都算勾上。
		if !o.picks(GroupWrite) {
			return false
		}
	}
	return o.picks(group)
}

// picks 报告分组是否被勾选（Groups 为空视为全选）。
func (o ToolOptions) picks(group string) bool {
	if len(o.Groups) == 0 {
		return true
	}
	for _, g := range o.Groups {
		if g == group {
			return true
		}
	}
	return false
}

// registerTools 把 Service 的方法注册成 MCP 工具。
// 结果一律是「人类 / AI 可读的文本」：工具链里文本比嵌套 JSON 更省 token、也更好读。
func registerTools(server *sdk.Server, svc *Service, opts ToolOptions) {
	// 读：集合与请求
	register(server, opts, &sdk.Tool{
		Name:        "list_workspaces",
		Description: "列出当前客户端里打开的集合（别名、名称、请求/文档数量与环境）。别名用于其它工具的 workspace 参数。",
	}, GroupCollection, false, func(ctx context.Context, _ struct{}) (string, error) {
		return svc.ListWorkspaces(ctx)
	})
	register(server, opts, &sdk.Tool{
		Name:        "list_requests",
		Description: "列出集合里的请求与文档：方法、名称、分组与相对路径（path 供 get_request / get_doc / send_request 使用）。",
	}, GroupCollection, false, svc.ListRequests)
	register(server, opts, &sdk.Tool{
		Name:        "get_request",
		Description: "读取一条请求的完整定义：方法与地址、查询参数、请求头、认证、请求体、请求体字段与文档。",
	}, GroupCollection, false, svc.GetRequest)

	// 读：文档
	register(server, opts, &sdk.Tool{
		Name:        "get_doc",
		Description: "读取集合里一篇 Markdown 文档的正文。",
	}, GroupDocs, false, svc.GetDoc)

	// 读：环境变量
	register(server, opts, &sdk.Tool{
		Name:        "list_envs",
		Description: "列出集合的环境与变量（敏感值只显示掩码）。可用 env 参数只看某一个环境。",
	}, GroupEnv, false, svc.ListEnvs)

	// 读：响应示例
	register(server, opts, &sdk.Tool{
		Name:        "list_examples",
		Description: "列出某条请求保存过的响应示例（名称、状态码、耗时、体积）。",
	}, GroupExample, false, svc.ListExamples)

	// 网络：真实发送（只读模式下不注册）
	register(server, opts, &sdk.Tool{
		Name:        "send_request",
		Description: "发送一条请求（真实网络调用），返回状态码、耗时、响应头与响应正文（超长会截断）。",
	}, GroupNetwork, true, svc.SendRequest)
	register(server, opts, &sdk.Tool{
		Name: "run_collection",
		Description: "按集合顺序跑一批 HTTP 请求（带 extract / assert 规则），返回逐条结果与汇总；" +
			"可用 folder 只跑某个分组、stopOnFailure 遇错即停。顺序执行：前一条提取的变量供后一条使用。",
	}, GroupNetwork, true, svc.RunCollection)

	// 写：改请求 / 新建请求 / 写环境变量
	register(server, opts, &sdk.Tool{
		Name:        "save_request",
		Description: "修改一条已有请求（只改传入的字段）：名称、方法、地址、请求头。",
	}, GroupCollection, true, svc.SaveRequest)
	register(server, opts, &sdk.Tool{
		Name:        "create_request",
		Description: "新建一条请求（可指定分组与请求头），返回新请求的相对路径。",
	}, GroupCollection, true, svc.CreateRequest)
	register(server, opts, &sdk.Tool{
		Name:        "set_env_var",
		Description: "写入或删除一个环境变量（secret=true 落 *.secrets.yml，delete=true 删除）。",
	}, GroupEnv, true, svc.SetEnvVar)

	// 读：检索与文档清单
	register(server, opts, &sdk.Tool{
		Name:        "search_requests",
		Description: "按关键词在名称 / 路径 / 方法 / 地址里检索请求与文档，返回可用的 path。",
	}, GroupCollection, false, svc.SearchRequests)
	register(server, opts, &sdk.Tool{
		Name:        "list_docs",
		Description: "列出集合里的文档（名称与相对路径），可按分组过滤。",
	}, GroupDocs, false, svc.ListDocs)

	// 写：删除与分组维护（都是软删除，进回收站）
	register(server, opts, &sdk.Tool{
		Name:        "delete_request",
		Description: "把一条请求移入回收站（不永久删除）。",
	}, GroupCollection, true, svc.DeleteRequest)
	register(server, opts, &sdk.Tool{
		Name:        "delete_requests",
		Description: "批量把请求移入回收站，返回成功与没处理的清单。",
	}, GroupCollection, true, svc.DeleteRequests)
	register(server, opts, &sdk.Tool{
		Name:        "create_group",
		Description: "新建一个分组（可指定父分组），返回分组相对路径。",
	}, GroupCollection, true, svc.CreateGroup)
	register(server, opts, &sdk.Tool{
		Name:        "rename_group",
		Description: "改分组的显示名（存在 folder.yml 里，目录名不变）。",
	}, GroupCollection, true, svc.RenameGroup)
	register(server, opts, &sdk.Tool{
		Name:        "delete_group",
		Description: "把分组连同里面的请求一起移入回收站。",
	}, GroupCollection, true, svc.DeleteGroup)

	// 写：文档增删改
	register(server, opts, &sdk.Tool{
		Name:        "create_doc",
		Description: "新建一篇 Markdown 文档（落 docs/），可同时写正文。",
	}, GroupDocs, true, svc.CreateDoc)
	register(server, opts, &sdk.Tool{
		Name:        "update_doc",
		Description: "改文档正文：整篇替换或追加，frontmatter 保持不变。",
	}, GroupDocs, true, svc.UpdateDoc)
	register(server, opts, &sdk.Tool{
		Name:        "delete_doc",
		Description: "把文档移入回收站。",
	}, GroupDocs, true, svc.DeleteDoc)

	// 写：响应示例
	register(server, opts, &sdk.Tool{
		Name:        "save_example",
		Description: "给一条请求存一份响应示例（可先 send_request 再把结果存下来）。",
	}, GroupExample, true, svc.SaveExample)
	register(server, opts, &sdk.Tool{
		Name:        "delete_example",
		Description: "删除某条请求下的一份响应示例。",
	}, GroupExample, true, svc.DeleteExample)

	// 写：环境增删改名
	register(server, opts, &sdk.Tool{
		Name:        "create_env",
		Description: "新建环境，可带初始变量（普通变量，敏感值请用 set_env_var）。",
	}, GroupEnv, true, svc.CreateEnv)
	register(server, opts, &sdk.Tool{
		Name:        "rename_env",
		Description: "给环境改名（文件名随之更新）。",
	}, GroupEnv, true, svc.RenameEnv)
	register(server, opts, &sdk.Tool{
		Name:        "delete_env",
		Description: "删除一个环境（含其敏感值文件）。",
	}, GroupEnv, true, svc.DeleteEnv)
}

// register 把「入参 → 文本」的函数包成 SDK 要求的处理函数，并套上分组过滤与审计。
func register[In any](server *sdk.Server, opts ToolOptions, tool *sdk.Tool,
	group string, write bool, fn func(context.Context, In) (string, error)) {
	if !opts.allows(group, write) {
		return
	}
	sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		started := time.Now()
		// 写工具先问应用：用户可以在界面上拒绝这一次调用。
		if write && opts.Confirm != nil {
			if err := opts.Confirm(ctx, tool.Name, in); err != nil {
				opts.Audit.Record(tool.Name, write, in, started, err)
				return nil, nil, err
			}
		}
		text, err := fn(ctx, in)
		opts.Audit.Record(tool.Name, write, in, started, err)
		if err != nil {
			return nil, nil, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil, nil
	})
}

// HTTPOptions 是 HTTP 传输配置。
type HTTPOptions struct {
	Addr         string
	Path         string
	Token        string
	AllowOrigins []string
	Logger       *log.Logger
}

// ServeHTTP 以 streamable HTTP 方式服务；ctx 取消即优雅关闭。
// 安全默认：Token 非空 → 校验 Authorization；带 Origin 的请求必须命中 AllowOrigins。
// /healthz 恒开且只回版本，方便先探通端口再谈鉴权。
func ServeHTTP(ctx context.Context, server *sdk.Server, opts HTTPOptions) error {
	if opts.Path == "" {
		opts.Path = "/mcp"
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	opts.Addr = normalizeAddr(opts.Addr)
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.Handle(opts.Path, withHTTPGuard(handler, opts, logger))
	srv := &http.Server{Addr: opts.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	// 不设 ReadTimeout / WriteTimeout：SSE 是长连接，写超时会掐断正常会话。
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return <-errCh
	}
}

// healthz 是存活探针：不要求令牌，只回版本信息。
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write([]byte(`{"ok":true,"service":"api-doc-go-viva","version":"` + ServerVersion + `"}`))
}

// withHTTPGuard 给 MCP 端点套上「令牌 + 来源」校验；OPTIONS 预检与允许的来源回 CORS 头。
func withHTTPGuard(next http.Handler, opts HTTPOptions, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		originOK := originAllowed(origin, opts.AllowOrigins)
		// 带 Origin = 浏览器脚本，必须显式允许（DNS rebinding 防护）；不带的不受来源限制。
		if origin != "" && !originOK {
			logger.Printf("拒绝来自 %s 的请求（Origin 不在允许列表内）", origin)
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if originOK && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", strings.Join([]string{
				"Authorization", "Content-Type", "Last-Event-ID",
				"Mcp-Session-Id", "Mcp-Protocol-Version",
			}, ", "))
			w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if opts.Token != "" && !tokenOK(r.Header.Get("Authorization"), opts.Token) {
			logger.Printf("拒绝 %s %s：缺少或错误的 Bearer 令牌", r.Method, r.URL.Path)
			w.Header().Set("WWW-Authenticate", `Bearer realm="api-doc-go"`)
			http.Error(w, "unauthorized: 需要 Authorization: Bearer <token>", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK 校验 `Authorization: Bearer <token>`（前缀大小写不敏感 + 定长比较）。
func tokenOK(header, want string) bool {
	const prefix = "bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	got := strings.TrimSpace(header[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// originAllowed 来源白名单：空 Origin 放行；allow 为空 → 拒绝一切带 Origin 的请求。
func originAllowed(origin string, allow []string) bool {
	if origin == "" {
		return true
	}
	if len(allow) == 0 {
		return false
	}
	normal := func(s string) string { return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "/") }
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "*" || normal(a) == normal(origin) {
			return true
		}
	}
	return false
}

// normalizeAddr 补全监听地址：兜底必须回环，漏传地址不该把集合暴露到局域网。
func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return DefaultAddr
	}
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}

// ToolNames 用内存传输自检：返回已注册的工具名（测试与设置面板用）。
func ToolNames(ctx context.Context, server *sdk.Server) ([]string, error) {
	t1, t2 := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "selfcheck", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		return nil, fmt.Errorf("自检会话建立失败: %w", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		out = append(out, tool.Name)
	}
	return out, nil
}
