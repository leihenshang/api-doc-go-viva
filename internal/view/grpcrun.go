package view

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	"github.com/leihenshang/api-doc-go-viva/internal/grpc"
	"github.com/leihenshang/api-doc-go-viva/internal/proto"
	"github.com/leihenshang/api-doc-go-viva/internal/send"
)

// gRPC 请求的执行：编译 .proto（带缓存）→ 拨号 → 动态调用 → 结果交回界面线程。
//
// 与 HTTP 同一套分层：internal/proto 只负责「.proto → 描述符」，
// internal/grpc 只负责「描述符 + 地址 + 消息 → 结果」，这里只做编排。

// protoCache 是集合内 .proto 的编译结果：用户不用装 protoc，同一份定义只编一次。
var protoCache = proto.NewCache()

// grpcOutcome 是一次调用的结果（后台线程算完交回界面线程）。
type grpcOutcome struct {
	gen    int
	result grpc.Result
	err    string
}

// loadGRPCFromEntry 换到一条 gRPC 请求时，把元数据与消息装进编辑区。
func (m *Model) loadGRPCFromEntry(entry *explorer.Entry) {
	m.grpcMetaRows, m.grpcMessage = nil, ""
	m.grpcStatus, m.grpcBody, m.grpcNote = "", "", ""
	m.grpcTime, m.grpcDirty = 0, false
	if entry == nil || entry.Payload == nil || entry.Payload.GRPC == nil {
		return
	}
	block := entry.Payload.GRPC
	m.grpcMetaRows = newKVEditor(block.Metadata)
	if len(m.grpcMetaRows) == 0 {
		m.grpcMetaRows = []*kvRow{{enabled: true}}
	}
	m.grpcMessage = block.Message
}

// saveGRPC 把元数据与请求消息写回请求文件（其余字段原样保留）。
func (m *Model) saveGRPC() {
	entry := m.entryByFile(m.req.file)
	if entry == nil || entry.Payload == nil || entry.Payload.GRPC == nil {
		return
	}
	file := entry.Payload
	file.GRPC.Metadata = kvFromEditor(m.grpcMetaRows)
	file.GRPC.Message = m.grpcMessage
	if err := explorer.SaveRequest(m.col.Dir, entry.File, file); err != nil {
		m.grpcNote = "保存失败：" + err.Error()
		m.invalidator.Invalidate()
		return
	}
	m.grpcDirty = false
	m.grpcNote = "已保存"
	m.invalidator.Invalidate()
}

// sendGRPC 发起一次调用：先编 .proto，再在后台拨号调用。
func (m *Model) sendGRPC(entry *explorer.Entry) {
	if entry == nil || entry.Payload == nil || entry.Payload.GRPC == nil {
		m.grpcNote = "这条请求没有 grpc 段"
		m.invalidator.Invalidate()
		return
	}
	if m.grpcSending {
		return
	}
	block := entry.Payload.GRPC
	protoPath := strings.TrimSpace(block.Proto)
	if protoPath == "" {
		m.grpcStatus = "缺少定义"
		m.grpcNote = "请求里没有 proto 路径：先在文件里写 grpc.proto（相对集合根）"
		m.invalidator.Invalidate()
		return
	}

	target, missing := send.Resolve(block.Target, m.vars)
	message, missingMessage := send.Resolve(m.grpcMessage, m.vars)
	missing = append(missing, missingMessage...)
	metadata := make([]grpc.KeyValue, 0, len(m.grpcMetaRows))
	for _, row := range m.grpcMetaRows {
		if !row.enabled || strings.TrimSpace(row.name) == "" {
			continue
		}
		name, _ := send.Resolve(row.name, m.vars)
		value, _ := send.Resolve(row.value, m.vars)
		metadata = append(metadata, grpc.KeyValue{Name: name, Value: value})
	}

	dir := m.col.Dir
	files := []string{filepath.Join(dir, filepath.FromSlash(protoPath))}
	imports := make([]string, 0, len(block.Imports))
	for _, importPath := range block.Imports {
		imports = append(imports, filepath.Join(dir, filepath.FromSlash(importPath)))
	}
	req := grpc.Request{
		Target:   target,
		Service:  block.Service,
		Method:   block.Method,
		Metadata: metadata,
		Message:  message,
		Timeout:  time.Duration(m.settings.TimeoutSec) * time.Second,
		Compress: block.Compress,
	}
	if block.TLS != nil {
		req.TLS = grpc.TLS{
			Mode:               block.TLS.Mode,
			CA:                 resolvePath(dir, block.TLS.CA),
			Cert:               resolvePath(dir, block.TLS.Cert),
			Key:                resolvePath(dir, block.TLS.Key),
			InsecureSkipVerify: block.TLS.InsecureSkipVerify,
		}
	}
	if m.settings.InsecureSSL {
		req.TLS.InsecureSkipVerify = true
	}

	m.grpcSending = true
	m.grpcStatus, m.grpcBody = "调用中…", ""
	m.grpcNote = ""
	if len(missing) > 0 {
		m.grpcNote = "未解析变量：" + strings.Join(missing, ", ")
	}
	m.gen++
	gen := m.gen
	service, method := block.Service, block.Method
	m.invalidator.Invalidate()

	go func() {
		m.grpcResults <- m.runGRPCCall(context.Background(), files, imports, req, service, method, gen)
		m.invalidator.Invalidate()
	}()
}

// runGRPCCall 是后台线程里的整段调用：编译 → 取方法描述符 → 调。
func (m *Model) runGRPCCall(ctx context.Context, files, imports []string, req grpc.Request,
	service, method string, gen int) grpcOutcome {
	compiled, err := protoCache.Compile(ctx, files, imports)
	if err != nil {
		return grpcOutcome{gen: gen, err: "编译 proto 失败：" + err.Error()}
	}
	descriptor, err := compiled.MethodDescriptor(service, method)
	if err != nil {
		return grpcOutcome{gen: gen, err: err.Error()}
	}
	result, err := grpc.Invoke(ctx, req, descriptor, compiled.Types)
	if err != nil {
		return grpcOutcome{gen: gen, err: err.Error()}
	}
	return grpcOutcome{gen: gen, result: result}
}

// drainGRPC 把后台结果搬到界面状态（每帧开头调用）。
func (m *Model) drainGRPC() {
	for {
		select {
		case out := <-m.grpcResults:
			if out.gen != m.gen {
				continue // 换过请求，旧结果作废
			}
			m.grpcSending = false
			if out.err != "" {
				m.grpcStatus, m.grpcBody = "失败", out.err
				continue
			}
			m.grpcStatus = out.result.CodeName
			m.grpcBody = out.result.Body
			m.grpcTime = out.result.Duration
		default:
			return
		}
	}
}

// grpcResultLine 是响应区顶部那行状态：OK / InvalidArgument + 耗时 + 大小。
func (m *Model) grpcResultLine() string {
	if m.grpcStatus == "" {
		return ""
	}
	line := m.grpcStatus
	if m.grpcTime > 0 {
		line += fmt.Sprintf(" · %d ms", m.grpcTime.Milliseconds())
	}
	if len(m.grpcBody) > 0 {
		line += " · " + humanBytes(len(m.grpcBody))
	}
	return line
}

// resolvePath 把集合相对的证书路径补成绝对路径；空串原样返回。
func resolvePath(dir, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(dir, filepath.FromSlash(rel))
}
