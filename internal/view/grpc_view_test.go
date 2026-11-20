package view

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/leihenshang/api-doc-go-viva/internal/explorer"
	grpcexec "github.com/leihenshang/api-doc-go-viva/internal/grpc"
	"github.com/leihenshang/api-doc-go-viva/internal/proto"
)

const viewGreeterProto = `syntax = "proto3";

package demo;

service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply);
}

message HelloRequest {
  string name = 1;
}

message HelloReply {
  string message = 1;
}
`

// startFakeGreeter 起一个真的 gRPC 服务（未知服务处理器 + 动态消息），返回地址。
func startFakeGreeter(t *testing.T, compiled *proto.Result) string {
	t.Helper()
	handler := func(_ any, stream grpc.ServerStream) error {
		methodPath, _ := grpc.MethodFromServerStream(stream)
		name := protoreflect.Name(methodPath[strings.LastIndex(methodPath, "/")+1:])
		md, err := compiled.MethodDescriptor("demo.Greeter", string(name))
		if err != nil {
			return err
		}
		in := dynamicpb.NewMessage(md.Input())
		if err := stream.RecvMsg(in); err != nil {
			return err
		}
		who := in.Get(md.Input().Fields().ByName("name")).String()
		out := dynamicpb.NewMessage(md.Output())
		out.Set(md.Output().Fields().ByName("message"), protoreflect.ValueOfString("hello "+who))
		return stream.SendMsg(out)
	}
	server := grpc.NewServer(grpc.UnknownServiceHandler(handler))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

// TestGRPCRequestCallsServer gRPC 请求页：点「调用」→ 真发一次 → 显示 OK 与响应 JSON。
func TestGRPCRequestCallsServer(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "greeter.proto"), viewGreeterProto)
	compiled, err := proto.Compile(context.Background(), []string{filepath.Join(dir, "greeter.proto")}, nil)
	if err != nil {
		t.Fatalf("编译 proto: %v", err)
	}
	addr := startFakeGreeter(t, compiled)

	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: gRPC 集合\n")
	writeFile(t, filepath.Join(dir, "问候.yml"), fmt.Sprintf(`info:
  name: 问候
  type: grpc
  seq: 1
meta:
  uid: grpc-1
grpc:
  target: %s
  service: demo.Greeter
  method: SayHello
  proto: greeter.proto
  message: '{"name":"neo"}'
`, addr))

	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "问候")
	pump(t, driver)

	if model.grpcMessage != `{"name":"neo"}` {
		t.Fatalf("请求消息没装进编辑区：%q", model.grpcMessage)
	}
	click(t, driver, "调用")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		pump(t, driver)
		if !model.grpcSending && model.grpcStatus != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if model.grpcStatus != "OK" {
		t.Fatalf("状态应为 OK，实际 %q（%s）", model.grpcStatus, model.grpcBody)
	}
	if !strings.Contains(model.grpcBody, "hello neo") {
		t.Fatalf("响应正文不对：%s", model.grpcBody)
	}
	if !shows(driver, "hello neo") {
		t.Fatalf("响应没画出来：%v", painted(driver))
	}

	// 改消息 → 保存：写回请求文件的 grpc.message。
	model.grpcMessage = `{"name":"trinity"}`
	model.grpcDirty = true
	model.saveGRPC()
	raw, err := os.ReadFile(filepath.Join(dir, "问候.yml"))
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	if !strings.Contains(string(raw), "trinity") {
		t.Fatalf("消息没写回文件：\n%s", raw)
	}
}

// TestGRPCMissingProtoExplains 没有 proto 路径时给出人话提示，而不是静默失败。
func TestGRPCMissingProtoExplains(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "opencollection.yml"), "opencollection: 1.0.0\ninfo:\n  name: 缺定义\n")
	writeFile(t, filepath.Join(dir, "裸.yml"), `info:
  name: 裸请求
  type: grpc
  seq: 1
meta:
  uid: grpc-2
grpc:
  target: 127.0.0.1:1
  service: demo.Greeter
  method: SayHello
`)
	col, err := explorer.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	model := New(col, "", nil)
	driver := openWindow(t, model)
	click(t, driver, "裸请求")
	pump(t, driver)
	click(t, driver, "调用")
	pump(t, driver)
	if !strings.Contains(model.grpcNote, "proto") {
		t.Fatalf("应提示缺 proto：status=%q note=%q", model.grpcStatus, model.grpcNote)
	}
	_ = grpcexec.Result{} // 包被界面层间接使用，保留引用避免误删
}

// grpcEntry 取集合里唯一那条 gRPC 请求。
func grpcEntry(t *testing.T, model *Model) *explorer.Entry {
	t.Helper()
	for _, entry := range model.col.Entries {
		if entry.Kind == explorer.KindGRPC {
			return entry
		}
	}
	t.Fatal("集合里没有 gRPC 请求")
	return nil
}
