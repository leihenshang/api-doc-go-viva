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

// Config 是内嵌服务的启动配置。
type Config struct {
	// Addr 监听地址：默认 127.0.0.1:8189（只本机可连）。
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
	registerTools(server, cfg.Service)
	return server, nil
}

// registerTools 把 Service 的方法注册成 MCP 工具。
// 结果一律是「人类 / AI 可读的文本」：工具链里文本比嵌套 JSON 更省 token、也更好读。
func registerTools(server *sdk.Server, svc *Service) {
	addTextTool(server, &sdk.Tool{
		Name:        "list_workspaces",
		Description: "列出当前客户端里打开的集合（别名、名称、请求/文档数量与环境）。别名用于其它工具的 workspace 参数。",
	}, func(ctx context.Context, _ struct{}) (string, error) {
		return svc.ListWorkspaces(ctx)
	})

	addTextTool(server, &sdk.Tool{
		Name:        "list_requests",
		Description: "列出集合里的请求与文档：方法、名称、分组与相对路径（path 供 get_request / get_doc / send_request 使用）。",
	}, svc.ListRequests)

	addTextTool(server, &sdk.Tool{
		Name:        "get_request",
		Description: "读取一条请求的完整定义：方法与地址、查询参数、请求头、认证、请求体、请求体字段与文档。",
	}, svc.GetRequest)

	addTextTool(server, &sdk.Tool{
		Name:        "get_doc",
		Description: "读取集合里一篇 Markdown 文档的正文。",
	}, svc.GetDoc)

	addTextTool(server, &sdk.Tool{
		Name:        "send_request",
		Description: "发送一条请求（真实网络调用），返回状态码、耗时、响应头与响应正文（超长会截断）。",
	}, svc.SendRequest)
}

// addTextTool 把「入参 → 文本」的函数包成 SDK 要求的处理函数。
func addTextTool[In any](server *sdk.Server, tool *sdk.Tool, fn func(context.Context, In) (string, error)) {
	sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		text, err := fn(ctx, in)
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
		return "127.0.0.1:8189"
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
