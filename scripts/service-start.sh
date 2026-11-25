#!/bin/sh
# systemd 启动入口：在真正运行二进制前选择与 Armbian 用户空间匹配的架构。
set -eu

APP_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$APP_DIR"

MACHINE="$(uname -m)"
BITS="$(getconf LONG_BIT 2>/dev/null || true)"
case "$MACHINE" in
  aarch64|arm64)
    # 64 位内核也可能运行 32 位 ARM 用户空间，必须同时检查 LONG_BIT。
    if [ "$BITS" = "32" ]; then
      APP_BIN="$APP_DIR/gohome-linux-armv7"
    else
      APP_BIN="$APP_DIR/gohome-linux-arm64"
    fi
    ;;
  armv7l|armv7*|armv8l|armhf)
    APP_BIN="$APP_DIR/gohome-linux-armv7"
    ;;
  x86_64|amd64)
    APP_BIN="$APP_DIR/gohome-linux-amd64"
    ;;
  *)
    echo "GoHome 暂不支持当前系统架构：$MACHINE" >&2
    echo "支持的架构：aarch64/arm64、armv7l/armhf、x86_64" >&2
    exit 1
    ;;
esac

if [ ! -f "$APP_BIN" ]; then
  echo "找不到当前架构对应的 GoHome 程序：$APP_BIN" >&2
  echo "请先运行 make armbian，并将 dist/ 中对应二进制复制到项目根目录。" >&2
  exit 1
fi
if [ ! -x "$APP_BIN" ]; then
  echo "程序没有执行权限：$APP_BIN" >&2
  echo "请执行：chmod 0755 '$APP_BIN'，然后重启 gohome 服务。" >&2
  exit 1
fi

# 固定以项目根目录为工作目录；所有运行数据都在项目下的 data/。
export GOHOME_DATA_DIR="${GOHOME_DATA_DIR:-$APP_DIR/data}"
export GOHOME_ADDR="${GOHOME_ADDR:-0.0.0.0:17880}"
mkdir -p "$GOHOME_DATA_DIR"

printf 'GoHome 启动：内核架构=%s，用户空间位数=%s，程序=%s，数据目录=%s，监听=%s\n' \
  "$MACHINE" "${BITS:-unknown}" "$APP_BIN" "$GOHOME_DATA_DIR" "$GOHOME_ADDR"
exec "$APP_BIN"
