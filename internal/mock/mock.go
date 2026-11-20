// Package mock 起一个只在本机监听的 HTTP 服务，把当前集合里的请求「假回复」出来：
// 路径与方法取自请求文件，响应体按请求体字段表（body.fields）拼一份示例 JSON。
//
// 它不转发、不执行脚本、不写盘——只给前端联调一个能对上的桩。没配字段表的请求
// 回一份最小 JSON，并在响应头里标明 X-Mock，避免和自己真实的服务混淆。
package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/leihenshang/api-doc-go-share/collection"
	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"gopkg.in/yaml.v3"
)

// DefaultAddr 是默认监听地址：只绑回环，避免把桩服务暴露到局域网。
const DefaultAddr = "127.0.0.1:2027"

// Rule 是单条请求的桩规则：状态码、延迟与整段响应体覆盖（可只写一部分）。
// 存在请求文件的顶层 mock: 键里（走 RequestFile.Extra 原样保留）。
type Rule struct {
	Status  int    `yaml:"status,omitempty" json:"status,omitempty"`
	DelayMS int    `yaml:"delayMs,omitempty" json:"delayMs,omitempty"`
	Body    string `yaml:"body,omitempty" json:"body,omitempty"`
}

// RuleKey 是规则在请求文件里的顶层键。
const RuleKey = "mock"

// RuleOf 读出某条请求的桩规则；没有就是零值。
func RuleOf(file *collection.RequestFile) Rule {
	if file == nil || file.Extra == nil {
		return Rule{}
	}
	raw, ok := file.Extra[RuleKey]
	if !ok || raw == nil {
		return Rule{}
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return Rule{}
	}
	var rule Rule
	if err := yaml.Unmarshal(data, &rule); err != nil {
		return Rule{}
	}
	return rule
}

// ApplyRule 把规则写回请求文件；全零就删掉这个键。
func ApplyRule(file *collection.RequestFile, rule Rule) {
	if file == nil {
		return
	}
	if rule.Status == 0 && rule.DelayMS <= 0 && strings.TrimSpace(rule.Body) == "" {
		if file.Extra != nil {
			delete(file.Extra, RuleKey)
		}
		return
	}
	if file.Extra == nil {
		file.Extra = map[string]any{}
	}
	item := map[string]any{}
	if rule.Status > 0 {
		item["status"] = rule.Status
	}
	if rule.DelayMS > 0 {
		item["delayMs"] = rule.DelayMS
	}
	if strings.TrimSpace(rule.Body) != "" {
		item["body"] = rule.Body
	}
	file.Extra[RuleKey] = item
}

// Route 是一条桩路由，界面用它列出「现在能打到哪些地址」。
type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Status  int    `json:"status,omitempty"`
	DelayMS int    `json:"delayMs,omitempty"`
}

// Server 是一个已启动的桩服务。
type Server struct {
	http   *http.Server
	listen string
	mu     sync.RWMutex
	routes []Route
	bodies map[string]string // "METHOD /path" → 示例响应体
	rules  map[string]Rule   // "METHOD /path" → 状态码 / 延迟 / 覆盖体
	logger *log.Logger
}

// Start 在 addr（空 = DefaultAddr）上监听，路由取自 dirs 里的集合。
func Start(addr string, dirs []string, logger *log.Logger) (*Server, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = DefaultAddr
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	routes, bodies, rules := Build(dirs)
	server := &Server{routes: routes, bodies: bodies, rules: rules, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.serve)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("监听 %s: %w", addr, err)
	}
	server.listen = listener.Addr().String()
	server.http = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := server.http.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			if logger != nil {
				logger.Printf("mock 服务退出：%v", serveErr)
			}
		}
	}()
	return server, nil
}

// Addr 是实际监听地址（端口填 0 时这里是系统分配的那个）。
func (s *Server) Addr() string { return s.listen }

// Routes 是当前注册的桩路由。
func (s *Server) Routes() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Route(nil), s.routes...)
}

// Reload 重新从集合目录读一遍路由（集合改了以后调用）。
func (s *Server) Reload(dirs []string) {
	routes, bodies, rules := Build(dirs)
	s.mu.Lock()
	s.routes, s.bodies, s.rules = routes, bodies, rules
	s.mu.Unlock()
}

// Close 停掉服务。
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

// serve 是唯一的入口：先认 /__mock/routes，再按方法 + 路径找桩。
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/__mock/routes" {
		s.mu.RLock()
		routes := append([]Route(nil), s.routes...)
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(routes)
		return
	}
	method := strings.ToUpper(r.Method)
	key := method + " " + path
	s.mu.RLock()
	body, ok := s.bodies[key]
	rule := s.rules[key]
	s.mu.RUnlock()
	if ok {
		if rule.DelayMS > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Duration(rule.DelayMS) * time.Millisecond):
			}
		}
		status := http.StatusOK
		if rule.Status > 0 {
			status = rule.Status
		}
		if text := strings.TrimSpace(rule.Body); text != "" {
			body = rule.Body
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Mock", "api-doc-go")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
		return
	}
	// 同一路径换了方法：405 比 404 更有用。
	s.mu.RLock()
	for existing := range s.bodies {
		if strings.HasSuffix(existing, " "+path) {
			s.mu.RUnlock()
			w.Header().Set("Allow", strings.TrimSuffix(existing, " "+path))
			http.Error(w, "method not allowed（桩只接受 "+strings.TrimSuffix(existing, " "+path)+"）", http.StatusMethodNotAllowed)
			return
		}
	}
	s.mu.RUnlock()
	http.Error(w, "no mock for "+key+"（GET /__mock/routes 看全部）", http.StatusNotFound)
}

// Build 从集合目录收集桩路由与示例响应体。
func Build(dirs []string) ([]Route, map[string]string, map[string]Rule) {
	routes := make([]Route, 0, 16)
	bodies := make(map[string]string, 16)
	rules := make(map[string]Rule, 16)
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		col, err := explorer.Load(dir)
		if err != nil {
			continue // 读不动的集合跳过：桩服务不该因为一个坏目录起不来
		}
		for _, entry := range col.Entries {
			if entry.Kind != explorer.KindHTTP || entry.Payload == nil {
				continue
			}
			path := PathOf(entry.Payload.HTTP.URL)
			if path == "" {
				continue
			}
			method := strings.ToUpper(strings.TrimSpace(entry.Payload.HTTP.Method))
			if method == "" {
				method = "GET"
			}
			key := method + " " + path
			if _, exists := bodies[key]; exists {
				continue // 先出现的赢，避免同一路径两条请求互相盖
			}
			rule := RuleOf(entry.Payload)
			body := sampleBody(entry.Payload)
			if strings.TrimSpace(rule.Body) != "" {
				body = rule.Body
			}
			bodies[key] = body
			if rule.Status != 0 || rule.DelayMS != 0 || rule.Body != "" {
				rules[key] = rule
			}
			routes = append(routes, Route{
				Method: method, Path: path, Name: entry.Name, File: entry.File,
				Status: rule.Status, DelayMS: rule.DelayMS,
			})
		}
	}
	return routes, bodies, rules
}

// PathOf 从请求地址里取路径：去掉协议 / 主机 / 查询串，空路径当 "/"。
// 地址里可能是 {{host}} 这样的变量，这里只按文本切，不解析变量。
func PathOf(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if index := strings.Index(target, "://"); index >= 0 {
		target = target[index+3:]
		if slash := strings.Index(target, "/"); slash >= 0 {
			target = target[slash:]
		} else {
			target = "/"
		}
	} else if !strings.HasPrefix(target, "/") {
		// {{host}}/api/v1 这种「变量当主机」的写法：丢掉主机那一段。
		if slash := strings.Index(target, "/"); slash >= 0 {
			target = target[slash:]
		} else {
			target = "/"
		}
	}
	if cut := strings.IndexAny(target, "?#"); cut >= 0 {
		target = target[:cut]
	}
	if !strings.HasPrefix(target, "/") {
		target = "/" + target
	}
	target = strings.TrimSuffix(target, "/")
	if target == "" {
		return "/"
	}
	return target
}

// sampleBody 按请求体字段表拼一份示例 JSON；没有字段表就回一个最小对象。
func sampleBody(file *collection.RequestFile) string {
	fields := file.HTTP.Body.Fields
	if len(fields) == 0 {
		return `{"mock":true}` + "\n"
	}
	var out strings.Builder
	out.WriteString("{\n")
	for index, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		out.WriteString("  ")
		quoted, err := json.Marshal(name)
		if err != nil {
			continue
		}
		out.Write(quoted)
		out.WriteString(": ")
		out.WriteString(jsonValue(field))
		if index < len(fields)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.String()
}

// jsonValue 按字段类型给出占位值：数字 / 布尔不加引号，其余当字符串。
func jsonValue(field collection.BodyField) string {
	value := strings.TrimSpace(field.Example)
	typ := strings.ToLower(strings.TrimSpace(field.Type))
	switch typ {
	case "number", "integer", "int", "float", "double":
		if value == "" {
			return "0"
		}
		return value
	case "boolean", "bool":
		if value == "" {
			return "false"
		}
		return value
	}
	if value == "" {
		value = "示例"
	}
	quoted, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(quoted)
}
