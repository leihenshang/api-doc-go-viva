#!/usr/bin/env bash
# 验证版门禁：格式 → 编译 → vet → 测试（含无头界面）。
# 父目录的 go.work 只含三个正式模块，这里一律 GOWORK=off 走本模块自己的 go.mod（replace 指向 share）。
set -euo pipefail

cd "$(dirname "$0")/.."
export GOWORK=off

# Go 不在默认 PATH 时用本机安装位置兜底
command -v go >/dev/null || export PATH="$PATH:$HOME/go/bin"

echo "== gofmt"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
	echo "$unformatted"
	echo "以上文件未格式化：go fmt ./..." >&2
	exit 1
fi

echo "== build"
go build ./...

echo "== vet"
go vet ./...

echo "== test"
go test ./...

echo "全部通过"
