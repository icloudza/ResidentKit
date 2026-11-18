#!/bin/bash
# 不依赖 Theos，在 macOS 上打 rootless（Dopamine，/var/jb）deb 包：
# 只需 Xcode iOS SDK + ldid + Python（无需 dpkg-deb）。
#
#   scripts/package-rootless.sh        → packages/<pkg>_<ver>_iphoneos-arm64.deb
#
# 正式构建仍以 Theos 为准（`make package THEOS_PACKAGE_SCHEME=rootless`）；
# 本脚本供只装了 Xcode 的机器使用。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PKG=$(awk '/^Package:/{print $2}' control)
VER=$(awk '/^Version:/{print $2}' control)
BUILD="$ROOT/.build-local"
STAGE="$BUILD/stage"
CTRL="$BUILD/control"
JB="$STAGE/var/jb"

rm -rf "$BUILD"
mkdir -p "$JB/usr/libexec" "$JB/Library/LaunchDaemons" \
         "$JB/Applications/ResidentKit.app" "$CTRL" "$ROOT/packages"

# 调试构建的额外编译参数，例：RK_EXTRA_CFLAGS="-DRK_DEBUG_SNAPSHOT"。
# shellcheck disable=SC2206
EXTRA=(${RK_EXTRA_CFLAGS:-})

# 默认服务器：从发布者本机的 server.env 读取（不在仓库里），写入安装包，设备装好即自动申请接入。
# 没有这个文件时打出的包需要在 App 里手动配置服务器。
SERVER_ENV="${RK_SERVER_ENV:-$HOME/.config/residentkit/server.env}"
RK_SERVER_URL=""
RK_SERVER_PIN=""
if [ -f "$SERVER_ENV" ]; then
    RK_SERVER_URL=$(awk -F= '/^RK_SERVER_URL=/{print $2}' "$SERVER_ENV")
    RK_SERVER_PIN=$(awk -F= '/^RK_SERVER_PIN=/{print $2}' "$SERVER_ENV")
    case "$RK_SERVER_URL" in wss://*|ws://*|"") ;; *) echo "RK_SERVER_URL 须以 wss:// 开头" >&2; exit 1 ;; esac
    # 指纹只在 IP + 自签证书时需要（逗号分隔，公钥指纹 + 证书指纹，见 rk-release pin）；
    # 域名证书由受信任 CA 签发，设备按系统校验，留空即可。
    if [ -n "$RK_SERVER_PIN" ] && ! [[ "$RK_SERVER_PIN" =~ ^[0-9a-f]{64}(,[0-9a-f]{64})*$ ]]; then
        echo "RK_SERVER_PIN 须为逗号分隔的 64 位小写十六进制" >&2; exit 1
    fi
fi
TRUST="系统证书校验"; [ -n "$RK_SERVER_PIN" ] && TRUST="证书指纹"
echo "==> default server: ${RK_SERVER_URL:-（无，需在 App 中配置）}${RK_SERVER_URL:+ · $TRUST}"

CFLAGS=(-arch arm64 -arch arm64e -miphoneos-version-min=14.0 -fobjc-arc -O2
        -Wall -Wextra -Wno-unused-parameter "-DRK_VERSION=\"$VER\""
        "-DRK_DEFAULT_URL=\"$RK_SERVER_URL\"" "-DRK_DEFAULT_PIN=\"$RK_SERVER_PIN\"" ${EXTRA[@]+"${EXTRA[@]}"})

echo "==> compiling residentd $VER"
xcrun --sdk iphoneos clang "${CFLAGS[@]}" -framework Foundation -framework Network -framework IOKit -framework Security \
    -Idaemon daemon/*.m daemon/Modules/*.m -o "$JB/usr/libexec/residentd"
ldid -Sdaemon/entitlements.plist "$JB/usr/libexec/residentd"

echo "==> compiling rk-updater $VER"
xcrun --sdk iphoneos clang "${CFLAGS[@]}" -framework Foundation \
    daemon/updater/main.m -o "$JB/usr/libexec/rk-updater"
ldid -Sdaemon/updater/entitlements.plist "$JB/usr/libexec/rk-updater"

echo "==> compiling ResidentKit.app $VER"
xcrun --sdk iphoneos clang "${CFLAGS[@]}" -framework UIKit -framework Foundation -framework QuartzCore -framework CoreGraphics -framework AVFoundation \
    app/*.m app/Core/*.m app/UI/*.m app/Features/*.m \
    -o "$JB/Applications/ResidentKit.app/ResidentKit"
ldid -Sapp/entitlements.plist "$JB/Applications/ResidentKit.app/ResidentKit"
plutil -convert binary1 -o "$JB/Applications/ResidentKit.app/Info.plist" app/Resources/Info.plist
# App 版本号统一取自 control（与 daemon、OTA 同一个版本），源文件 Info.plist 里的版本号不用再手动改。
plutil -replace CFBundleShortVersionString -string "$VER" "$JB/Applications/ResidentKit.app/Info.plist"
plutil -replace CFBundleVersion -string "$VER" "$JB/Applications/ResidentKit.app/Info.plist"
cp app/Resources/*.png "$JB/Applications/ResidentKit.app/"

cp layout/Library/LaunchDaemons/*.plist "$JB/Library/LaunchDaemons/"

echo "==> control"
SIZE=$(du -sk "$STAGE" | awk '{print $1}')
grep -v -E '^(Architecture|Installed-Size):' control > "$CTRL/control"
printf 'Architecture: iphoneos-arm64\nInstalled-Size: %s\n' "$SIZE" >> "$CTRL/control"
for s in postinst prerm postrm; do
    [ -f "layout/DEBIAN/$s" ] && install -m 755 "layout/DEBIAN/$s" "$CTRL/$s"
done

OUT="$ROOT/packages/${PKG}_${VER}_iphoneos-arm64.deb"
echo "==> packing $OUT"
python3 -I - "$STAGE" "$CTRL" "$OUT" <<'PY'
import io, os, sys, tarfile, time

stage, ctrl, out = sys.argv[1:4]

def tar_dir(src):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz", format=tarfile.GNU_FORMAT) as t:
        for dirpath, dirs, files in os.walk(src):
            dirs.sort()
            for name in [None] + sorted(files):
                p = dirpath if name is None else os.path.join(dirpath, name)
                rel = os.path.relpath(p, src)
                arc = "./" if rel == "." else "./" + rel
                ti = t.gettarinfo(p, arcname=arc)
                ti.uid = ti.gid = 0
                ti.uname, ti.gname = "root", "wheel"
                if name is None:
                    t.addfile(ti)
                else:
                    with open(p, "rb") as f:
                        t.addfile(ti, f)
    return buf.getvalue()

members = [("debian-binary", b"2.0\n"),
           ("control.tar.gz", tar_dir(ctrl)),
           ("data.tar.gz", tar_dir(stage))]
now = int(time.time())
with open(out, "wb") as f:
    f.write(b"!<arch>\n")
    for name, data in members:
        hdr = f"{name:<16}{now:<12}{0:<6}{0:<6}{'100644':<8}{len(data):<10}`\n"
        f.write(hdr.encode())
        f.write(data)
        if len(data) % 2:
            f.write(b"\n")
PY

echo "==> done: $OUT"
