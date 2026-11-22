#!/usr/bin/env bash
# 打 Linux 安装包（.deb）：把二进制、桌面条目与多档图标装进一个能 dpkg -i 的包里。
#
#   bash scripts/build-deb.sh                 # 版本取 updater.Version，架构取本机
#   bash scripts/build-deb.sh --version 1.2.3
#   bash scripts/build-deb.sh --arch arm64    # 交叉打 ARM（Go 纯静态，无需 MinGW 之类）
#   bash scripts/build-deb.sh --installer-only# 复用 dist 下已有二进制，只重打包
#
# 产物：dist/api-doc-viva_<版本>_<架构>.deb
#
# 为什么用 dpkg-deb 直接拼目录，而不引 nfpm / dh_make：
#   要装的只有「一个静态二进制 + 一个 .desktop + 几档 PNG」，用不上 debhelper 的构建体系；
#   dpkg-deb --build --root-owner-group 已经能把属主规整成 root，够用且没有额外依赖。
#
# 安装后的布局：
#   /usr/lib/api-doc-viva/api-doc-viva     真正的二进制
#   /usr/bin/api-doc-viva                  软链（PATH 里能直接敲）
#   /usr/share/applications/api-doc-viva.desktop
#   /usr/share/icons/hicolor/<n>x<n>/apps/api-doc-viva.png
#
# 依赖声明只有 libc6：Vulkan 是运行时用 purego 打开 libvulkan.so.1，缺了会退回 CPU 光栅，
# 所以写 Recommends 而不是 Depends —— 没有显卡驱动的机器上也该能装。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
	sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
}

VERSION=""
ARCH="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
INSTALLER_ONLY=0

while (($#)); do
	case "$1" in
	--version)
		VERSION="${2:?--version 需要一个版本号}"
		shift 2
		;;
	--arch)
		ARCH="${2:?--arch 需要 amd64 或 arm64}"
		shift 2
		;;
	--installer-only)
		INSTALLER_ONLY=1
		shift
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

case "$ARCH" in
amd64 | arm64) ;;
*)
	echo "--arch 目前只支持 amd64 / arm64，实际：$ARCH" >&2
	exit 2
	;;
esac

command -v dpkg-deb >/dev/null 2>&1 || {
	echo "找不到 dpkg-deb：这是打 .deb 的必要工具（Debian / Ubuntu / Deepin 自带）" >&2
	exit 1
}

if ! command -v go >/dev/null 2>&1; then
	echo "找不到 go，请先安装 Go 1.26+ 并加入 PATH" >&2
	exit 1
fi
if [[ ! -f ../api-doc-go-share/go.mod ]]; then
	echo "找不到 ../api-doc-go-share：go.mod 的 replace 需要它和本仓库并排" >&2
	exit 1
fi

# 版本号与 Windows 包、检查更新用同一个数：只维护 internal/updater 里那一处。
if [[ -z "$VERSION" ]]; then
	VERSION="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/updater/updater.go | head -1)"
	[[ -n "$VERSION" ]] || {
		echo "读不到 internal/updater/updater.go 里的 Version，请用 --version 指定" >&2
		exit 1
	}
fi

# Debian 版本号不能带下划线之外的花活：预发布用 ~ 表示「比正式版小」。
DEB_VERSION="${VERSION/-/\~}"

BIN="dist/api-doc-viva-linux-$ARCH"
if [[ "$INSTALLER_ONLY" -eq 1 ]]; then
	[[ -f "$BIN" ]] || {
		echo "--installer-only 需要已有的 $BIN，先跑一次不带该参数的构建" >&2
		exit 1
	}
	echo "复用 $BIN"
else
	mkdir -p dist
	echo "构建 $BIN（GOOS=linux GOARCH=$ARCH CGO_ENABLED=0）"
	CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" GOWORK="${GOWORK:-off}" \
		go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/leihenshang/api-doc-go-viva/internal/updater.Version=$VERSION" \
		-o "$BIN" .
	printf '  产物 %s  %s\n' "$BIN" "$(du -h "$BIN" | cut -f1)"
fi

# ---- 铺安装树 ----

PKG_NAME="api-doc-viva"
STAGE="build/deb/${PKG_NAME}_${DEB_VERSION}_${ARCH}"
rm -rf "$STAGE"
mkdir -p "$STAGE/DEBIAN" \
	"$STAGE/usr/bin" \
	"$STAGE/usr/lib/$PKG_NAME" \
	"$STAGE/usr/share/applications" \
	"$STAGE/usr/share/doc/$PKG_NAME"

install -m 0755 "$BIN" "$STAGE/usr/lib/$PKG_NAME/$PKG_NAME"
# 软链而不是把二进制再放一份：升级时只有一份要替换。
ln -sf "../lib/$PKG_NAME/$PKG_NAME" "$STAGE/usr/bin/$PKG_NAME"

install -m 0644 packaging/linux/api-doc-viva.desktop \
	"$STAGE/usr/share/applications/$PKG_NAME.desktop"

echo "生成图标（internal/brand 的画法 → 多档 PNG）"
GOWORK="${GOWORK:-off}" go run ./cmd/makeicon -out build/linux -png-dir build/linux/png >/dev/null
for size in 16 24 32 48 64 128 256 512; do
	dir="$STAGE/usr/share/icons/hicolor/${size}x${size}/apps"
	mkdir -p "$dir"
	install -m 0644 "build/linux/png/icon-$size.png" "$dir/$PKG_NAME.png"
done

if [[ -f README.md ]]; then
	install -m 0644 README.md "$STAGE/usr/share/doc/$PKG_NAME/README.md"
fi

INSTALLED_SIZE="$(du -sk --exclude=DEBIAN "$STAGE" | cut -f1)"

# ---- control ----

cat >"$STAGE/DEBIAN/control" <<EOF
Package: $PKG_NAME
Version: $DEB_VERSION
Section: devel
Priority: optional
Architecture: $ARCH
Installed-Size: $INSTALLED_SIZE
Maintainer: api-doc-go <noreply@example.com>
Recommends: libvulkan1
Description: API-DOC-GO 接口集合客户端（Viva 原生渲染验证版）
 读写 opencollection 集合：浏览接口、真实发送 HTTP/gRPC 请求、
 写接口文档、跑集合运行器、起本地 Mock、内置 MCP 服务。
 .
 界面由 Viva 原生绘制（Vulkan，缺驱动时退回 CPU 光栅），
 不依赖 webview 与前端构建产物；同一个二进制也支持 Windows 与 macOS。
Homepage: https://github.com/leihenshang/api-doc-go
EOF

# 安装后刷新桌面与图标缓存；没装对应工具时静默跳过（容器里常见）。
cat >"$STAGE/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database -q /usr/share/applications || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
	gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor || true
fi
exit 0
EOF
chmod 0755 "$STAGE/DEBIAN/postinst"

# ---- 打包 ----

mkdir -p dist
DEB="dist/${PKG_NAME}_${DEB_VERSION}_${ARCH}.deb"
rm -f "$DEB"
# --root-owner-group：以当前用户构建时也把属主写成 root:root（无需 fakeroot）。
dpkg-deb --build --root-owner-group "$STAGE" "$DEB" >/dev/null

printf '  安装包 %s  %s\n' "$DEB" "$(du -h "$DEB" | cut -f1)"
echo "  安装：sudo dpkg -i $DEB   卸载：sudo apt remove $PKG_NAME"
echo "完成。"
