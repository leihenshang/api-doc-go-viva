// Package grpc 是 gRPC 请求的执行器：按 .proto 编译出的描述符做动态调用。
//
// 只做调用本身（拨号、元数据、消息编解码、状态码），把「定义从哪来」留给调用方；
// 与 HTTP 的 send 包同一层次，界面层两边长得也一样。
package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// maxMsgSize 是收发消息的上限（与客户端一致）：4 MB 足够覆盖常见接口。
const maxMsgSize = 4 << 20

// KeyValue 是元数据 / 响应头里的一行。
type KeyValue struct {
	Name  string
	Value string
}

// TLS 是连接安全设置；Mode 为空或 none 表示明文。
type TLS struct {
	Mode               string
	CA                 string // 服务端 CA（PEM 路径）
	Cert               string // 客户端证书（mTLS）
	Key                string // 客户端私钥（mTLS）
	InsecureSkipVerify bool
}

// Request 一次调用所需的输入。变量替换由调用方先做完。
type Request struct {
	Target   string // host:port
	Service  string // demo.Greeter
	Method   string // SayHello
	Metadata []KeyValue
	Message  string // protojson
	TLS      TLS
	Timeout  time.Duration
	Compress string // 空 / identity / gzip
}

// Result 一次调用的结果。
//
// 非 OK 的 gRPC 状态**不算错误**：它带着状态码与详情返回，界面照常渲染；
// 只有「没到服务端」（地址非法、消息不合法、证书读不出来）才返回 error。
type Result struct {
	Code     int32
	CodeName string
	Body     string // 响应消息（protojson）；出错时是状态详情
	Headers  []KeyValue
	Trailers []KeyValue
	Duration time.Duration
	SentSize int
	Stream   bool
}

// Invoke 执行一次调用（unary 或 server-streaming）。
func Invoke(ctx context.Context, req Request, method protoreflect.MethodDescriptor, types *protoregistry.Types) (Result, error) {
	if method == nil {
		return Result{}, fmt.Errorf("没有可调用的方法（先导入 .proto 并选服务与方法）")
	}
	if method.IsStreamingClient() {
		return Result{}, fmt.Errorf("暂不支持客户端流 / 双工方法 %s", method.Name())
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	creds, err := transportCreds(req.TLS)
	if err != nil {
		return Result{}, err
	}
	conn, err := grpc.NewClient(normalizeTarget(req.Target),
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize), grpc.MaxCallSendMsgSize(maxMsgSize)),
	)
	if err != nil {
		return Result{}, fmt.Errorf("建立连接失败: %w", err)
	}
	defer func() { _ = conn.Close() }()

	message := dynamicpb.NewMessage(method.Input())
	if text := strings.TrimSpace(req.Message); text != "" {
		if err := protojson.Unmarshal([]byte(text), message); err != nil {
			return Result{}, fmt.Errorf("请求消息不是合法 JSON: %w", err)
		}
	}
	sentSize := 0
	if raw, marshalErr := proto.Marshal(message); marshalErr == nil {
		sentSize = len(raw)
	}

	callOpts := []grpc.CallOption{}
	if strings.EqualFold(strings.TrimSpace(req.Compress), "gzip") {
		callOpts = append(callOpts, grpc.UseCompressor(gzip.Name))
	}
	ctx = withMetadata(ctx, req.Metadata)
	path := "/" + string(method.Parent().FullName()) + "/" + string(method.Name())
	start := time.Now()

	if method.IsStreamingServer() {
		return invokeStream(ctx, conn, path, message, callOpts, method, types, start, sentSize)
	}
	return invokeUnary(ctx, conn, path, message, callOpts, method, types, start, sentSize)
}

// invokeUnary 一元调用：拿一条响应消息。
func invokeUnary(ctx context.Context, conn *grpc.ClientConn, path string, message *dynamicpb.Message,
	callOpts []grpc.CallOption, method protoreflect.MethodDescriptor, types *protoregistry.Types,
	start time.Time, sentSize int) (Result, error) {
	reply := dynamicpb.NewMessage(method.Output())
	var header, trailer metadata.MD
	callOpts = append(callOpts, grpc.Header(&header), grpc.Trailer(&trailer))
	err := conn.Invoke(ctx, path, message, reply, callOpts...)

	result := Result{
		Duration: time.Since(start),
		SentSize: sentSize,
		Headers:  mdToKV(header),
		Trailers: mdToKV(trailer),
	}
	if err != nil {
		st, _ := status.FromError(err)
		result.Code, result.CodeName = int32(st.Code()), codeName(st.Code())
		result.Body = fmt.Sprintf("{\n  \"code\": %d,\n  \"message\": %s\n}", int32(st.Code()), quote(st.Message()))
		return result, nil
	}
	result.Code, result.CodeName = int32(codes.OK), codeName(codes.OK)
	body, err := protojson.MarshalOptions{Multiline: true, Indent: "  ", EmitUnpopulated: false}.
		Marshal(proto.Message(reply))
	if err != nil {
		return result, fmt.Errorf("响应无法转成 JSON: %w", err)
	}
	result.Body = string(body)
	return result, nil
}

// invokeStream 服务器流：把每条消息按到达顺序收进数组。
func invokeStream(ctx context.Context, conn *grpc.ClientConn, path string, message *dynamicpb.Message,
	callOpts []grpc.CallOption, method protoreflect.MethodDescriptor, types *protoregistry.Types,
	start time.Time, sentSize int) (Result, error) {
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, path, callOpts...)
	if err != nil {
		return Result{}, fmt.Errorf("打开流失败: %w", err)
	}
	if err := stream.SendMsg(message); err != nil {
		return Result{}, fmt.Errorf("发送消息失败: %w", err)
	}
	if err := stream.CloseSend(); err != nil {
		return Result{}, fmt.Errorf("关闭发送端失败: %w", err)
	}

	result := Result{Duration: 0, SentSize: sentSize, Stream: true}
	rows := make([]string, 0, 8)
	for {
		reply := dynamicpb.NewMessage(method.Output())
		if err := stream.RecvMsg(reply); err != nil {
			if err.Error() == "EOF" {
				break
			}
			st, _ := status.FromError(err)
			result.Code, result.CodeName = int32(st.Code()), codeName(st.Code())
			result.Body = fmt.Sprintf("{\n  \"code\": %d,\n  \"message\": %s\n}", int32(st.Code()), quote(st.Message()))
			result.Duration = time.Since(start)
			result.Headers = mdToKV(result.headersOf(stream))
			return result, nil
		}
		body, marshalErr := protojson.MarshalOptions{Multiline: false}.Marshal(proto.Message(reply))
		if marshalErr != nil {
			return result, fmt.Errorf("响应无法转成 JSON: %w", marshalErr)
		}
		rows = append(rows, string(body))
	}
	result.Code, result.CodeName = int32(codes.OK), codeName(codes.OK)
	result.Duration = time.Since(start)
	result.Headers = mdToKV(result.headersOf(stream))
	result.Body = "[\n  " + strings.Join(rows, ",\n  ") + "\n]"
	return result, nil
}

// headersOf 取流的响应头。
func (r Result) headersOf(stream grpc.ClientStream) metadata.MD {
	md, _ := stream.Header()
	return md
}

// transportCreds 按 TLS 模式给出传输凭据。
func transportCreds(cfg TLS) (credentials.TransportCredentials, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", "none":
		return insecure.NewCredentials(), nil
	case "tls", "mtls":
		config, err := tlsConfig(cfg)
		if err != nil {
			return nil, err
		}
		return credentials.NewTLS(config), nil
	}
	return nil, fmt.Errorf("不认识的 TLS 模式 %q", cfg.Mode)
}

// normalizeTarget 去掉 scheme 与结尾斜杠：gRPC 只认 host:port。
func normalizeTarget(raw string) string {
	target := strings.TrimSpace(raw)
	target = strings.TrimPrefix(target, "grpc://")
	target = strings.TrimPrefix(target, "https://")
	target = strings.TrimPrefix(target, "http://")
	return strings.TrimRight(target, "/")
}

// withMetadata 把元数据挂到 ctx 上（空名跳过）。
func withMetadata(ctx context.Context, rows []KeyValue) context.Context {
	pairs := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" || row.Value == "" {
			continue
		}
		pairs = append(pairs, row.Name, row.Value)
	}
	if len(pairs) == 0 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// mdToKV 把元数据摊平成有序行。
func mdToKV(md metadata.MD) []KeyValue {
	if len(md) == 0 {
		return nil
	}
	keys := make([]string, 0, len(md))
	for key := range md {
		keys = append(keys, key)
	}
	sortStrings(keys)
	out := make([]KeyValue, 0, len(keys))
	for _, key := range keys {
		for _, value := range md[key] {
			out = append(out, KeyValue{Name: key, Value: value})
		}
	}
	return out
}

// codeName 是状态码的名字（OK / InvalidArgument…），界面直接用。
func codeName(code codes.Code) string {
	return code.String()
}

// quote 把字符串变成 JSON 字符串字面量（错误详情用）。
func quote(text string) string {
	raw, err := jsonMarshal(text)
	if err != nil {
		return `"(无法显示)"`
	}
	return string(raw)
}
