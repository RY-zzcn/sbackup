#!/usr/bin/env bash
set -euo pipefail

mode=update
install_restic=true
install_rclone=true
force=false

usage() {
  cat <<'EOF'
用法: scripts/install-runtime-tools.sh [选项]

从官方来源检查、安装或更新 Restic 和 rclone。第三方二进制不会写入项目目录。

选项:
  --check          只检查本机版本与官方最新版，不修改系统
  --update         安装缺失工具并更新到官方最新版（默认）
  --restic-only    只处理 Restic
  --rclone-only    只处理 rclone
  --force          即使版本相同也重新安装
  -h, --help       显示帮助
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check) mode=check ;;
    --update) mode=update ;;
    --restic-only) install_rclone=false ;;
    --rclone-only) install_restic=false ;;
    --force) force=true ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [[ $mode != check && $(id -u) -ne 0 ]]; then
  echo "安装或更新需要 root 权限（例如 sudo $0）" >&2
  exit 1
fi

for tool in curl sha256sum bzip2 unzip sed awk sort install; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "缺少基础命令 $tool；请先运行 scripts/install.sh，或通过系统包管理器安装 curl、bzip2、unzip 和 coreutils" >&2
    exit 1
  }
done

case "$(uname -m)" in
  x86_64|amd64) restic_arch=amd64; rclone_arch=amd64 ;;
  aarch64|arm64) restic_arch=arm64; rclone_arch=arm64 ;;
  armv7l) restic_arch=arm; rclone_arch=arm-v7 ;;
  *) echo "不支持的 CPU 架构: $(uname -m)" >&2; exit 1 ;;
esac
[[ $(uname -s) == Linux ]] || { echo "当前安装脚本仅支持 Linux" >&2; exit 1; }

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
outdated=0

version_lt() {
  [[ $1 != "$2" && $(printf '%s\n' "$1" "$2" | sort -V | head -n1) == "$1" ]]
}

if $install_restic; then
  release_json="$tmp_dir/restic-release.json"
  curl --fail --silent --show-error --location \
    https://api.github.com/repos/restic/restic/releases/latest \
    --output "$release_json"
  restic_latest=$(sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' "$release_json" | head -n1)
  [[ -n $restic_latest ]] || { echo "无法读取 Restic 最新版本" >&2; exit 1; }
  restic_current=""
  if command -v restic >/dev/null 2>&1; then
    restic_current=$(restic version | awk 'NR==1 {print $2}')
  fi
  printf 'Restic: 当前 %s，官方最新 %s\n' "${restic_current:-未安装}" "$restic_latest"
  if [[ $restic_current != "$restic_latest" ]]; then outdated=1; fi
  if [[ $mode == update && ( $force == true || $restic_current != "$restic_latest" ) ]]; then
    asset="restic_${restic_latest}_linux_${restic_arch}.bz2"
    base="https://github.com/restic/restic/releases/download/v${restic_latest}"
    curl --fail --silent --show-error --location "$base/$asset" --output "$tmp_dir/$asset"
    curl --fail --silent --show-error --location "$base/SHA256SUMS" --output "$tmp_dir/SHA256SUMS"
    checksum_line=$(grep "  ${asset}$" "$tmp_dir/SHA256SUMS" || true)
    [[ -n $checksum_line ]] || { echo "官方校验文件中没有 $asset" >&2; exit 1; }
    printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && sha256sum --check --strict -)
    bzip2 --decompress --stdout "$tmp_dir/$asset" > "$tmp_dir/restic"
    chmod 0755 "$tmp_dir/restic"
    install -m 0755 "$tmp_dir/restic" /usr/local/bin/restic
    /usr/local/bin/restic version
  fi
fi

if $install_rclone; then
  curl --fail --silent --show-error --location https://downloads.rclone.org/version.txt --output "$tmp_dir/rclone-version.txt"
  rclone_latest=$(sed -n 's/^rclone v\([^[:space:]]*\).*/\1/p' "$tmp_dir/rclone-version.txt" | head -n1)
  [[ -n $rclone_latest ]] || { echo "无法读取 rclone 最新版本" >&2; exit 1; }
  rclone_current=""
  if command -v rclone >/dev/null 2>&1; then
    rclone_current=$(rclone version | sed -n 's/^rclone v//p' | head -n1)
  fi
  printf 'rclone: 当前 %s，官方最新 %s\n' "${rclone_current:-未安装}" "$rclone_latest"
  if [[ $rclone_current != "$rclone_latest" ]]; then outdated=1; fi
  if [[ $mode == update && ( $force == true || $rclone_current != "$rclone_latest" ) ]]; then
    archive="rclone-v${rclone_latest}-linux-${rclone_arch}.zip"
    base="https://downloads.rclone.org/v${rclone_latest}"
    curl --fail --silent --show-error --location "$base/$archive" --output "$tmp_dir/$archive"
    curl --fail --silent --show-error --location "$base/SHA256SUMS" --output "$tmp_dir/rclone-SHA256SUMS"
    checksum_line=$(grep "  ${archive}$" "$tmp_dir/rclone-SHA256SUMS" || true)
    [[ -n $checksum_line ]] || { echo "官方校验文件中没有 $archive" >&2; exit 1; }
    printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && sha256sum --check --strict -)
    unzip -q "$tmp_dir/$archive" -d "$tmp_dir/rclone"
    install -m 0755 "$tmp_dir/rclone/rclone-v${rclone_latest}-linux-${rclone_arch}/rclone" /usr/local/bin/rclone
    /usr/local/bin/rclone version | sed -n '1,4p'
  fi
fi

if [[ $mode == check && $outdated -ne 0 ]]; then
  echo "存在缺失或可更新的运行时工具。" >&2
  exit 2
fi

echo "运行时工具检查完成。"
