module github.com/leihenshang/api-doc-go-viva

go 1.26.0

require (
	github.com/leihenshang/api-doc-go-share v0.0.0
	github.com/vivaui/viva v0.6.5
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// 共享库走本地代码：集合格式只有一份真源（api-doc-go-share/collection）。
replace github.com/leihenshang/api-doc-go-share => ../api-doc-go-share
