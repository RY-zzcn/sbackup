#!/usr/bin/env bash
set -euo pipefail

[[ $(id -u) -eq 0 ]] || { echo "请使用 sudo 执行安装命令" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "缺少 curl，请先使用系统包管理器安装 curl" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "缺少 tar，请先使用系统包管理器安装 tar" >&2; exit 1; }

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
curl --fail --silent --show-error --location \
  https://github.com/RY-zzcn/sbackup/archive/refs/heads/main.tar.gz \
  --output "$tmp_dir/sbackup.tar.gz"
tar -C "$tmp_dir" -xzf "$tmp_dir/sbackup.tar.gz"
exec "$tmp_dir/sbackup-main/scripts/install.sh" "$@"
