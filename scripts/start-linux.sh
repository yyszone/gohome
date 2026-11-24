#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
command -v go >/dev/null 2>&1 || { echo '请先安装 Go 1.23+' >&2; exit 1; }
command -v make >/dev/null 2>&1 || { echo '请先安装 make' >&2; exit 1; }
mkdir -p data
make run
