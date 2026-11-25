#!/bin/sh
# 安装/更新 GoHome systemd 服务，并设置开机启动。
set -eu

APP_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SOURCE_UNIT="$APP_DIR/deploy/systemd/gohome.service"
TARGET_UNIT="/etc/systemd/system/gohome.service"

if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo "$APP_DIR/scripts/install-systemd.sh" "$@"
  fi
  echo "请使用 root 执行：sh scripts/install-systemd.sh" >&2
  exit 1
fi

if [ ! -f "$SOURCE_UNIT" ]; then
  echo "找不到 systemd 服务模板：$SOURCE_UNIT" >&2
  exit 1
fi
if [ ! -x "$APP_DIR/scripts/service-start.sh" ]; then
  chmod 0755 "$APP_DIR/scripts/service-start.sh"
fi
mkdir -p "$APP_DIR/data"
chmod 0700 "$APP_DIR/data"

# 在安装阶段设置当前机器要运行的程序权限，服务运行期间不需要修改项目目录。
MACHINE="$(uname -m)"
BITS="$(getconf LONG_BIT 2>/dev/null || true)"
case "$MACHINE" in
  aarch64|arm64)
    if [ "$BITS" = "32" ]; then APP_BIN="$APP_DIR/gohome-linux-armv7"; else APP_BIN="$APP_DIR/gohome-linux-arm64"; fi
    ;;
  armv7l|armv7*|armv8l|armhf) APP_BIN="$APP_DIR/gohome-linux-armv7" ;;
  x86_64|amd64) APP_BIN="$APP_DIR/gohome-linux-amd64" ;;
  *) echo "暂不支持当前系统架构：$MACHINE" >&2; exit 1 ;;
esac
if [ ! -f "$APP_BIN" ]; then
  echo "缺少当前系统架构对应的 GoHome 程序：$APP_BIN" >&2
  exit 1
fi
chmod 0755 "$APP_BIN"

# 将模板中的项目根目录替换为当前绝对路径，便于目录并非 /root/gohome 时复用。
# systemd unit 的路径不接受 shell 变量展开，因此在安装时生成固定路径。
APP_DIR_SED="$(printf '%s' "$APP_DIR" | sed 's/[\\&|]/\\&/g')"
sed "s|@GOHOME_DIR@|$APP_DIR_SED|g" "$SOURCE_UNIT" > "$TARGET_UNIT"
chmod 0644 "$TARGET_UNIT"

systemctl daemon-reload
systemctl enable gohome.service
if systemctl is-active --quiet gohome.service; then
  systemctl restart gohome.service
else
  systemctl start gohome.service
fi

printf '\nGoHome systemd 服务已安装并启用。\n'
printf '状态：systemctl status gohome --no-pager\n'
printf '日志：journalctl -u gohome -f\n'
