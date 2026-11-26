#!/bin/sh
# Safe remote deployment for GoHome. Replaces only the architecture-specific
# executable selected by scripts/service-start.sh; leaves data/ and config intact.
set -eu

: "${DEPLOY_HOST:?用法：make deploy DEPLOY_HOST=root@主机地址 DEPLOY_ARCH=arm64}"
DEPLOY_ARCH=${DEPLOY_ARCH:-arm64}
DEPLOY_PATH=${DEPLOY_PATH:-/root/gohome}
DEPLOY_SERVICE=${DEPLOY_SERVICE:-gohome}
DEPLOY_BINARY=${DEPLOY_BINARY:-}

case "$DEPLOY_ARCH" in
  arm64) DEPLOY_BINARY=${DEPLOY_BINARY:-gohome-linux-arm64} ;;
  armv7) DEPLOY_BINARY=${DEPLOY_BINARY:-gohome-linux-armv7} ;;
  amd64) DEPLOY_BINARY=${DEPLOY_BINARY:-gohome-linux-amd64} ;;
  *) echo "不支持的 DEPLOY_ARCH：$DEPLOY_ARCH（可选 arm64 / armv7 / amd64）" >&2; exit 2 ;;
esac

case "$DEPLOY_PATH" in
  /*) ;;
  *) echo "DEPLOY_PATH 必须是绝对路径：$DEPLOY_PATH" >&2; exit 2 ;;
esac
case "$DEPLOY_PATH$DEPLOY_SERVICE$DEPLOY_BINARY" in
  *[!A-Za-z0-9_./-]*) echo "部署目录、服务名和二进制文件名不能包含空格或 Shell 特殊字符" >&2; exit 2 ;;
esac

LOCAL_BINARY="dist/$DEPLOY_BINARY"
if [ ! -s "$LOCAL_BINARY" ]; then
  echo "本地编译产物不存在：$LOCAL_BINARY（先运行对应 make 目标）" >&2
  exit 1
fi
command -v ssh >/dev/null 2>&1 || { echo "找不到 ssh 命令" >&2; exit 1; }
command -v scp >/dev/null 2>&1 || { echo "找不到 scp 命令" >&2; exit 1; }

printf '部署目标：%s:%s\n架构：%s\n服务：%s\n二进制：%s\n' \
  "$DEPLOY_HOST" "$DEPLOY_PATH" "$DEPLOY_ARCH" "$DEPLOY_SERVICE" "$DEPLOY_BINARY"

# Check the real remote user-space architecture before transferring anything.
ssh "$DEPLOY_HOST" sh -s -- "$DEPLOY_ARCH" <<'REMOTE_CHECK'
set -eu
want=$1
machine=$(uname -m)
bits=$(getconf LONG_BIT 2>/dev/null || echo unknown)
case "$want:$machine:$bits" in
  arm64:aarch64:64|arm64:arm64:64|armv7:armv7l:32|armv7:armv8l:32|armv7:aarch64:32|armv7:armhf:32|amd64:x86_64:64|amd64:amd64:64) ;;
  *) echo "远程架构不匹配：uname -m=$machine LONG_BIT=$bits，要求 $want" >&2; exit 1 ;;
esac
REMOTE_CHECK

REMOTE_TEMP="$DEPLOY_PATH/.$DEPLOY_BINARY.uploading"
echo "正在上传 $LOCAL_BINARY ..."
ssh "$DEPLOY_HOST" "mkdir -p '$DEPLOY_PATH'"
scp "$LOCAL_BINARY" "$DEPLOY_HOST:$REMOTE_TEMP"

# The replacement is atomic on the same filesystem. Keep one rollback copy
# until systemd reports the new process as active.
ssh "$DEPLOY_HOST" sh -s -- \
  "$DEPLOY_PATH" "$DEPLOY_ARCH" "$DEPLOY_SERVICE" "$DEPLOY_BINARY" <<'REMOTE_DEPLOY'
set -eu
app_dir=$1
want=$2
service=$3
binary=$4
machine=$(uname -m)
bits=$(getconf LONG_BIT 2>/dev/null || echo unknown)
case "$want:$machine:$bits" in
  arm64:aarch64:64|arm64:arm64:64|armv7:armv7l:32|armv7:armv8l:32|armv7:aarch64:32|armv7:armhf:32|amd64:x86_64:64|amd64:amd64:64) ;;
  *) echo "远程架构在上传后发生变化，拒绝替换" >&2; exit 1 ;;
esac
candidate="$app_dir/.$binary.uploading"
target="$app_dir/$binary"
backup="$app_dir/.$binary.previous"
[ -s "$candidate" ] || { echo "上传文件缺失或为空：$candidate" >&2; exit 1; }
chmod 0755 "$candidate"
if [ -f "$target" ]; then cp -p "$target" "$backup"; else rm -f "$backup"; fi
mv -f "$candidate" "$target"
if systemctl restart "$service" && systemctl is-active --quiet "$service"; then
  rm -f "$backup"
  echo "部署成功：$target，systemd 服务 $service 已运行。"
  exit 0
fi

echo "新版本启动失败，正在恢复旧二进制..." >&2
if [ -f "$backup" ]; then
  mv -f "$backup" "$target"
  chmod 0755 "$target"
  systemctl restart "$service" || true
else
  rm -f "$target"
  systemctl stop "$service" || true
fi
exit 1
REMOTE_DEPLOY
