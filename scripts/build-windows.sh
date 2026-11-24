#!/usr/bin/env bash
# 交叉编译 Windows 版，并可选地打出 NSIS 安装包（在 Linux / macOS / WSL 上运行）。
#
#   bash scripts/build-windows.sh                     # 只出 exe：amd64，隐藏控制台
#   bash scripts/build-windows.sh arm64               # Windows on ARM
#   bash scripts/build-windows.sh --console           # 保留控制台，方便看日志
#   bash scripts/build-windows.sh --out /tmp/win      # 换 exe 输出目录
#
#   bash scripts/build-windows.sh --installer              # exe + 安装包（Program Files，需管理员）
#   bash scripts/build-windows.sh --installer --scope user # 装到 %LOCALAPPDATA%\Programs，免提权
#   bash scripts/build-windows.sh --installer --dev        # 开发版：产品名/目录/卸载键独立，可与正式版并存
#   bash scripts/build-windows.sh --installer --version 1.2.3
#   bash scripts/build-windows.sh --installer-only         # 复用 dist 下已有 exe，只重打安装包
#
# 产物：
#   exe     dist/api-doc-viva-windows-<arch>.exe
#   安装包  dist/api-doc-viva_<版本>_<arch>-setup.exe（--dev 时是 api-doc-viva-dev_…）
#
# Viva 的 Windows 后端是纯 Go（purego 动态调用），CGO_ENABLED=0 即可交叉编译，
# 不需要 MinGW；NSIS 也能在 Linux 上跑（makensis 是原生程序，输出的是 Windows 安装器）。
#
# 找 makensis 的顺序：$NSIS_BIN → PATH 里的 makensis → $NSISDIR/usr/bin/makensis
# （最后一种是为「下载 deb 解包、没有 root」的场景准备的）：
#
#   apt-get download nsis nsis-common
#   for d in nsis*.deb; do dpkg-deb -x "$d" /tmp/nsisroot; done
#   export NSISDIR=/tmp/nsisroot/usr/share/nsis NSIS_BIN=/tmp/nsisroot/usr/bin/makensis
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
	sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
}

ARCH=amd64
CONSOLE=0
OUT_DIR=dist
INSTALLER=0
INSTALLER_ONLY=0
DEV=0
SCOPE=machine
VERSION=""

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
	--installer)
		INSTALLER=1
		shift
		;;
	--installer-only)
		INSTALLER=1
		INSTALLER_ONLY=1
		shift
		;;
	--dev)
		DEV=1
		shift
		;;
	--scope)
		SCOPE="${2:?--scope 需要 user 或 machine}"
		shift 2
		;;
	--version)
		VERSION="${2:?--version 需要一个版本号}"
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

case "$SCOPE" in
user | machine) ;;
*)
	echo "--scope 只能是 user 或 machine，实际：$SCOPE" >&2
	exit 2
	;;
esac

if ! command -v go >/dev/null 2>&1; then
	echo "找不到 go，请先安装 Go 1.26+ 并加入 PATH（本机通常在 \$HOME/go/bin）" >&2
	exit 1
fi

# ---- 版本号 ----

# 默认取 updater.Version：检查更新与安装包显示同一个数，免得两处各写一份。
default_version() {
	sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/updater/updater.go | head -1
}

if [[ -z "$VERSION" && "$INSTALLER" -eq 1 ]]; then
	VERSION="$(default_version)"
	[[ -n "$VERSION" ]] || {
		echo "读不到 internal/updater/updater.go 里的 Version，请用 --version 指定" >&2
		exit 1
	}
fi

# VIProductVersion 要「四段纯数字」：先去掉 -beta.1 / +build 这类后缀，再补齐到 4 段。
version4() {
	local base="${1%%-*}"
	base="${base%%+*}"
	IFS=. read -r a b c d <<<"$base"
	a="${a:-0}"; b="${b:-0}"; c="${c:-0}"; d="${d:-0}"
	echo "$a.$b.$c.$d"
}

# ---- 名字（正式版 / 开发版必须互不相同，否则不能并存安装）----

if [[ "$DEV" -eq 1 ]]; then
	PRODUCT_NAME="api-doc-go-viva-dev"
	APP_BASE="api-doc-viva-dev"
	UNINST_KEY='Software\Microsoft\Windows\CurrentVersion\Uninstall\api-doc-go-viva-dev'
	EXE_OUT="$OUT_DIR/$APP_BASE-windows-$ARCH.exe"
else
	PRODUCT_NAME="api-doc-go-viva"
	APP_BASE="api-doc-viva"
	UNINST_KEY='Software\Microsoft\Windows\CurrentVersion\Uninstall\api-doc-go-viva'
	EXE_OUT="$OUT_DIR/$APP_BASE-windows-$ARCH.exe"
fi

mkdir -p "$OUT_DIR"

LDFLAGS="-s -w"
if [[ "$CONSOLE" -eq 0 ]]; then
	# 桌面程序不要黑框；调试时用 --console 换成控制台子系统。
	LDFLAGS="$LDFLAGS -H windowsgui"
fi
if [[ -n "$VERSION" ]]; then
	# 让「关于 / 检查更新」里显示的版本与安装包一致。
	LDFLAGS="$LDFLAGS -X github.com/leihenshang/api-doc-go-viva/internal/updater.Version=$VERSION"
fi

build_exe() {
	local out="$1"
	echo "构建 $out（GOOS=windows GOARCH=$ARCH CGO_ENABLED=0）"
	CGO_ENABLED=0 GOOS=windows GOARCH="$ARCH" GOWORK="${GOWORK:-off}" \
		go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$out" .
	printf '  产物 %s  %s\n' "$out" "$(du -h "$out" | cut -f1)"
}

# ---- 找 makensis ----

find_makensis() {
	if [[ -n "${NSIS_BIN:-}" && -x "${NSIS_BIN}" ]]; then
		echo "$NSIS_BIN"
		return 0
	fi
	if command -v makensis >/dev/null 2>&1; then
		command -v makensis
		return 0
	fi
	# 解 deb 的场景：NSISDIR=/tmp/nsisroot/usr/share/nsis，二进制在同级的 ../../bin。
	if [[ -n "${NSISDIR:-}" ]]; then
		local prefix
		prefix="$(cd "${NSISDIR}/../.." 2>/dev/null && pwd || true)"
		if [[ -n "$prefix" && -x "$prefix/bin/makensis" ]]; then
			echo "$prefix/bin/makensis"
			return 0
		fi
	fi
	return 1
}

# ---- 打安装包 ----

build_installer() {
	local exe="$1"
	local makensis
	if ! makensis="$(find_makensis)"; then
		cat >&2 <<'MSG'
找不到 makensis（NSIS 编译器）。三种装法任选：

  Debian / Ubuntu   sudo apt install nsis
  macOS             brew install makensis
  Windows           choco install nsis   或 https://nsis.sourceforge.io/Download

没有 root 时可以把 deb 解出来用（makensis 是原生程序，不需要安装）：

  apt-get download nsis nsis-common
  for d in nsis*.deb; do dpkg-deb -x "$d" /tmp/nsisroot; done
  export NSISDIR=/tmp/nsisroot/usr/share/nsis NSIS_BIN=/tmp/nsisroot/usr/bin/makensis

再重新运行本脚本即可（--installer-only 可以跳过重复编译）。
MSG
		return 1
	fi

	local stage="build/windows/stage"
	local icon_dir="build/windows"
	local icon="$icon_dir/icon.ico"
	rm -rf "$stage"
	mkdir -p "$stage"

	echo "生成图标（internal/brand 的画法 → $icon）"
	GOWORK="${GOWORK:-off}" go run ./cmd/makeicon -out "$icon_dir"

	local app_exe="$APP_BASE.exe"
	cp "$exe" "$stage/$app_exe"

	local out_name="${APP_BASE}_${VERSION}_${ARCH}-setup.exe"
	local out_path="$OUT_DIR/$out_name"
	rm -f "$out_path"

	echo "打包 $out_path（$PRODUCT_NAME $VERSION，scope=$SCOPE）"
	# -WX：警告也算失败。安装包里「少个图标 / 变量拼错」这类问题都不该静默放过。
	"$makensis" -V2 -WX \
		-DSTAGE_DIR="$ROOT/$stage" \
		-DICON_FILE="$ROOT/$icon" \
		-DOUT_FILE="$ROOT/$out_path" \
		-DPRODUCT_NAME="$PRODUCT_NAME" \
		-DAPP_EXE="$app_exe" \
		-DAPP_ARCH="$ARCH" \
		-DAPP_VERSION="$VERSION" \
		-DAPP_VERSION4="$(version4 "$VERSION")" \
		-DINSTALL_SCOPE="$SCOPE" \
		-DUNINST_KEY="$UNINST_KEY" \
		"$ROOT/packaging/windows/installer.nsi"

	if [[ ! -f "$out_path" ]]; then
		echo "makensis 返回成功但找不到产物：$out_path" >&2
		return 1
	fi
	printf '  安装包 %s  %s\n' "$out_path" "$(du -h "$out_path" | cut -f1)"
	echo "  安装包内含：$app_exe + icon.ico（快捷方式与「应用和功能」里的图标）"
	echo "  静默安装：$out_name /S    卸载：控制面板「应用和功能」或安装目录 uninstall.exe"
}

# ---- 主流程 ----

if [[ "$INSTALLER_ONLY" -eq 1 ]]; then
	if [[ ! -f "$EXE_OUT" ]]; then
		echo "--installer-only 需要已有的 $EXE_OUT，先跑一次不带该参数的构建" >&2
		exit 1
	fi
	echo "复用 $EXE_OUT"
else
	build_exe "$EXE_OUT"
fi

if [[ "$INSTALLER" -eq 1 ]]; then
	build_installer "$EXE_OUT"
fi

echo "完成。"
