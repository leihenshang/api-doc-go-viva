module github.com/leihenshang/api-doc-go-viva

go 1.26.0

require (
	github.com/bufbuild/protocompile v0.14.1
	github.com/leihenshang/api-doc-go-share v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/vivaui/viva v0.6.5
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1 // indirect
)

// 共享库走本地代码：集合格式只有一份真源（api-doc-go-share/collection）。
replace github.com/leihenshang/api-doc-go-share => ../api-doc-go-share

// viva 只在 Windows 用到 x/sys（claim_windows.go）；本机离线缓存只有 v0.48.0，
// 固定到它能保证 Linux 构建可用，Windows 侧的 windows 包 API 也够用。
replace golang.org/x/sys => golang.org/x/sys v0.48.0
