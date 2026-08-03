#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source_build=false
with_monitor=false
all_database_tools=false
with_webdav=false
enable_timers=true
runtime_update=true

usage() {
  cat <<'EOF'
用法: sudo scripts/install.sh [选项]

默认从 GitHub Releases 下载已构建的静态客户端，不安装 Go、Git 或编译工具。

选项:
  --source-build          从源码构建（开发/无 Release 网络环境）
  --with-monitor          同时安装 sbackup-monitor
  --all-database-tools    安装 PostgreSQL、MySQL/MariaDB 和 SQLite 客户端
  --with-webdav           安装或更新 rclone；不指定则只安装 Restic
  --skip-runtime-update   不检查/更新 Restic 和 rclone
  --no-enable-timers      不启用 maintenance timer
  -h, --help              显示帮助

重复执行即为升级，不覆盖已有配置、密钥、状态和日志。
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-build) source_build=true ;;
    --with-monitor) with_monitor=true ;;
    --all-database-tools) all_database_tools=true ;;
    --with-webdav) with_webdav=true ;;
    --skip-runtime-update) runtime_update=false ;;
    --no-enable-timers) enable_timers=false ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

[[ $(uname -s) == Linux ]] || { echo "SBackup 当前仅支持 Linux" >&2; exit 1; }
[[ $(id -u) -eq 0 ]] || { echo "安装需要 root 权限（例如 sudo $0）" >&2; exit 1; }

package_manager=""
for candidate in apt-get dnf yum zypper pacman apk; do
  if command -v "$candidate" >/dev/null 2>&1; then package_manager=$candidate; break; fi
done
[[ -n $package_manager ]] || { echo "未找到受支持的包管理器" >&2; exit 1; }

case "$package_manager" in
  apt-get)
    packages=(ca-certificates curl bzip2 tar)
    $with_webdav && packages+=(unzip)
    $all_database_tools && packages+=(sqlite3 postgresql-client default-mysql-client)
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${packages[@]}"
    ;;
  dnf|yum)
    packages=(ca-certificates curl bzip2 tar)
    $with_webdav && packages+=(unzip)
    $all_database_tools && packages+=(sqlite postgresql mysql)
    "$package_manager" install -y "${packages[@]}"
    ;;
  zypper)
    packages=(ca-certificates curl bzip2 tar)
    $with_webdav && packages+=(unzip)
    $all_database_tools && packages+=(sqlite3 postgresql-client mariadb-client)
    zypper --non-interactive install "${packages[@]}"
    ;;
  pacman)
    packages=(ca-certificates curl bzip2 tar)
    $with_webdav && packages+=(unzip)
    $all_database_tools && packages+=(sqlite postgresql-libs mariadb-clients)
    pacman -Sy --needed --noconfirm "${packages[@]}"
    ;;
  apk)
    packages=(ca-certificates curl bzip2 tar)
    $with_webdav && packages+=(unzip)
    $all_database_tools && packages+=(sqlite postgresql-client mariadb-client)
    apk add --no-cache "${packages[@]}"
    ;;
esac

if [[ $runtime_update == true ]]; then
  runtime_args=(--restic-only --update)
  $with_webdav && runtime_args=(--update)
  "$project_root/scripts/install-runtime-tools.sh" "${runtime_args[@]}"
else
  command -v restic >/dev/null 2>&1 || { echo "缺少 restic；去掉 --skip-runtime-update 或先手工安装" >&2; exit 1; }
  if $with_webdav; then command -v rclone >/dev/null 2>&1 || { echo "缺少 rclone；去掉 --skip-runtime-update 或先手工安装" >&2; exit 1; }; fi
fi

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l) arch=armv7 ;;
  *) echo "不支持的 CPU 架构: $(uname -m)" >&2; exit 1 ;;
esac
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
if $source_build; then
  if ! command -v go >/dev/null 2>&1 && [[ -x /usr/local/go/bin/go ]]; then
    export PATH="/usr/local/go/bin:$PATH"
  fi
  command -v go >/dev/null 2>&1 || { echo "--source-build 需要 Go 1.23+" >&2; exit 1; }
  go_version=$(go env GOVERSION 2>/dev/null || true)
  [[ $go_version =~ ^go1\.(2[3-9]|[3-9][0-9])([.]|$) ]] || { echo "--source-build 需要 Go 1.23+，当前为 ${go_version:-未知}" >&2; exit 1; }
  cd "$project_root"
  version=$(tr -d '[:space:]' < VERSION)
  GOTOOLCHAIN=local go test -buildvcs=false ./...
  GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" -o "$tmp_dir/sbackup" ./cmd/sbackup
  GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" -o "$tmp_dir/sbackup-monitor" ./cmd/sbackup-monitor
else
  curl --fail --silent --show-error --location https://api.github.com/repos/RY-zzcn/sbackup/releases/latest --output "$tmp_dir/release.json"
  version=$(sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' "$tmp_dir/release.json" | head -n1)
  [[ -n $version ]] || { echo "无法读取 SBackup 最新 Release" >&2; exit 1; }
  archive="sbackup_${version}_linux_${arch}.tar.gz"
  base="https://github.com/RY-zzcn/sbackup/releases/download/v${version}"
  curl --fail --silent --show-error --location "$base/$archive" --output "$tmp_dir/$archive"
  curl --fail --silent --show-error --location "$base/SHA256SUMS" --output "$tmp_dir/SHA256SUMS"
  checksum_line=$(grep "  ${archive}$" "$tmp_dir/SHA256SUMS" || true)
  [[ -n $checksum_line ]] || { echo "Release 校验文件中没有 $archive" >&2; exit 1; }
  printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && sha256sum --check --strict -)
  tar -C "$tmp_dir" -xzf "$tmp_dir/$archive"
fi
install -m 0755 "$tmp_dir/sbackup" /usr/local/bin/sbackup
if $with_monitor; then install -m 0755 "$tmp_dir/sbackup-monitor" /usr/local/bin/sbackup-monitor; fi

install -d -m 0700 /etc/sbackup /etc/sbackup/secrets /var/lib/sbackup /var/lib/sbackup/tmp
install -d -m 0750 /var/log/sbackup
install -d -m 0755 /usr/local/share/sbackup/scripts /usr/local/share/sbackup/deploy/systemd
install -m 0755 "$project_root/scripts/install.sh" "$project_root/scripts/install-runtime-tools.sh" "$project_root/scripts/preflight.sh" "$project_root/scripts/uninstall.sh" /usr/local/share/sbackup/scripts/
install -m 0644 "$project_root/deploy/systemd/"* /usr/local/share/sbackup/deploy/systemd/
if [[ ! -f /etc/sbackup/config.yaml ]]; then /usr/local/bin/sbackup init; fi
install -m 0644 "$project_root/deploy/systemd/sbackup-maintenance.service" /etc/systemd/system/sbackup-maintenance.service
install -m 0644 "$project_root/deploy/systemd/sbackup-maintenance.timer" /etc/systemd/system/sbackup-maintenance.timer
if $with_monitor; then
  if ! id sbackup-monitor >/dev/null 2>&1; then useradd --system --home /var/lib/sbackup-monitor --shell /usr/sbin/nologin sbackup-monitor; fi
  install -d -o sbackup-monitor -g sbackup-monitor -m 0700 /var/lib/sbackup-monitor
  install -d -m 0750 /etc/sbackup-monitor
  if [[ ! -f /etc/sbackup-monitor/environment ]]; then
    install -m 0600 /dev/null /etc/sbackup-monitor/environment
    printf 'SBACKUP_MONITOR_USER=admin\nSBACKUP_MONITOR_PASSWORD=CHANGE-ME-BEFORE-START\n' > /etc/sbackup-monitor/environment
  fi
  install -m 0644 "$project_root/deploy/systemd/sbackup-monitor.service" /etc/systemd/system/sbackup-monitor.service
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  $enable_timers && systemctl enable --now sbackup-maintenance.timer
fi
/usr/local/bin/sbackup doctor
echo "SBackup $version 安装/升级完成。配置文件: /etc/sbackup/config.yaml"
