package grpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/leihenshang/api-doc-go-viva/internal/proto"
)

const greeterProto = `syntax = "proto3";

package demo;

service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply);
  rpc Fail (HelloRequest) returns (HelloReply);
}

message HelloRequest {
  string name = 1;
}

message HelloReply {
  string message = 1;
}
`

// fixture 起一个真的 gRPC 服务（未知服务处理器 + 动态消息，不依赖生成代码），
// 返回它的地址与编译好的描述符。
func fixture(t *testing.T) (string, *proto.Result) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "greeter.proto")
	if err := os.WriteFile(path, []byte(greeterProto), 0o644); err != nil {
		t.Fatalf("写 proto: %v", err)
	}
	compiled, err := proto.Compile(context.Background(), []string{path}, nil)
	if err != nil {
		t.Fatalf("编译 proto: %v", err)
	}

	handler := func(_ any, stream grpc.ServerStream) error {
		methodPath, _ := grpc.MethodFromServerStream(stream)
		methodName := protoreflect.Name(methodPath[strings.LastIndex(methodPath, "/")+1:])
		md, err := compiled.MethodDescriptor("demo.Greeter", string(methodName))
		if err != nil {
			return err
		}
		in := dynamicpb.NewMessage(md.Input())
		if err := stream.RecvMsg(in); err != nil {
			return err
		}
		if methodName == "Fail" {
			return status.Error(codes.InvalidArgument, "名字不能为空")
		}
		name := in.Get(md.Input().Fields().ByName("name")).String()
		out := dynamicpb.NewMessage(md.Output())
		out.Set(md.Output().Fields().ByName("message"), protoreflect.ValueOfString("hello "+name))
		return stream.SendMsg(out)
	}

	server := grpc.NewServer(grpc.UnknownServiceHandler(handler))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String(), compiled
}

// TestInvokeUnary 真调一次：地址 + 元数据 + protojson 消息都能走通，响应转回 JSON。
func TestInvokeUnary(t *testing.T) {
	addr, compiled := fixture(t)
	md, err := compiled.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取方法: %v", err)
	}
	result, err := Invoke(context.Background(), Request{
		Target:  addr,
		Message: `{"name":"neo"}`,
		Metadata: []KeyValue{
			{Name: "x-trace", Value: "abc"},
		},
		Timeout: 5 * time.Second,
	}, md, compiled.Types)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Code != int32(codes.OK) || result.CodeName != "OK" {
		t.Fatalf("状态不对：%d %s / %s", result.Code, result.CodeName, result.Body)
	}
	if !strings.Contains(result.Body, `"hello neo"`) {
		t.Fatalf("响应正文不对：%s", result.Body)
	}
	if result.SentSize == 0 {
		t.Fatal("应记录发送字节数")
	}
}

// TestInvokeReportsStatus 非 OK 的状态不是 error，而是带状态码回来。
func TestInvokeReportsStatus(t *testing.T) {
	addr, compiled := fixture(t)
	md, err := compiled.MethodDescriptor("demo.Greeter", "Fail")
	if err != nil {
		t.Fatalf("取方法: %v", err)
	}
	result, err := Invoke(context.Background(), Request{
		Target: addr, Message: `{"name":"x"}`, Timeout: 5 * time.Second,
	}, md, compiled.Types)
	if err != nil {
		t.Fatalf("Invoke 不该返回 error：%v", err)
	}
	if result.Code != int32(codes.InvalidArgument) || result.CodeName != "InvalidArgument" {
		t.Fatalf("状态不对：%d %s", result.Code, result.CodeName)
	}
	if !strings.Contains(result.Body, "名字不能为空") {
		t.Fatalf("详情里应有错误消息：%s", result.Body)
	}
}

// TestInvokeRejects 消息不合法 / 地址为空这些「到不了服务端」的情况才报 error。
func TestInvokeRejects(t *testing.T) {
	_, compiled := fixture(t)
	md, err := compiled.MethodDescriptor("demo.Greeter", "SayHello")
	if err != nil {
		t.Fatalf("取方法: %v", err)
	}
	if _, err := Invoke(context.Background(), Request{Target: "127.0.0.1:1", Message: `{`}, md, nil); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
	if _, err := Invoke(context.Background(), Request{Target: "127.0.0.1:1"}, nil, nil); err == nil {
		t.Fatal("没有方法应报错")
	}
}

// TestNormalizeTarget scheme 与结尾斜杠都要剥掉。
func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"grpc://127.0.0.1:9090": "127.0.0.1:9090",
		"https://api.dev:443/":  "api.dev:443",
		"  10.0.0.1:50051 ":     "10.0.0.1:50051",
	}
	for in, want := range cases {
		if got := normalizeTarget(in); got != want {
			t.Errorf("%q → %q，期望 %q", in, got, want)
		}
	}
}
