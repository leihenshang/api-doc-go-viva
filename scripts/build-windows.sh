#!/usr/bin/env bash
# 交叉编译 Windows 版（在 Linux / macOS / WSL 上运行）。
#
#   bash scripts/build-windows.sh                 # amd64，隐藏控制台
#   bash scripts/build-windows.sh arm64           # Windows on ARM
#   bash scripts/build-windows.sh --console       # 保留控制台，方便看日志
#   bash scripts/build-windows.sh --out /tmp/win  # 换输出目录
#
# 产物默认 dist/api-doc-viva-windows-<arch>.exe。
# Viva 的 Windows 后端是纯 Go（purego 动态调用），因此 CGO_ENABLED=0 即可交叉编译。
# go.mod 用 replace 指向同级目录的 api-doc-go-share，构建前必须存在。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
	sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
}

ARCH=amd64
CONSOLE=0
OUT_DIR=dist
while (($#)); do
	case "$1" in
	amd64 | arm64)
		ARCH="$1"
		shift
		;;
	--console)
		CONSOLE=1
		shift
		;;
	--out)
		OUT_DIR="${2:?--out 需要一个目录}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "未知参数: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if ! command -v go >/dev/null 2>&1; then
	echo "找不到 go，请先安装 Go 1.26+ 并加入 PATH（本机通常在 \$HOME/go/bin）" >&2
	exit 1
fi

if [[ ! -f ../api-doc-go-share/go.mod ]]; then
	echo "找不到 ../api-doc-go-share：go.mod 的 replace 需要它和本仓库并排" >&2
	exit 1
fi

OUT="$OUT_DIR/api-doc-viva-windows-$ARCH.exe"
mkdir -p "$OUT_DIR"

LDFLAGS="-s -w"
if [[ "$CONSOLE" -eq 0 ]]; then
	# 桌面程序不要黑框；调试时用 --console 换成控制台子系统。
	LDFLAGS="$LDFLAGS -H windowsgui"
fi

echo "构建 $OUT（GOOS=windows GOARCH=$ARCH CGO_ENABLED=0）"
CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" GOWORK="${GOWORK:-off}" \
	go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUT" .

ls -lh "$OUT"
case "$OUT" in
/*) echo "完成：$OUT" ;;
*) echo "完成：$ROOT/$OUT" ;;
esac
