#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mode=install
with_monitor=false
all_database_tools=false
enable_timers=true
update_runtime=true

if [[ -x /usr/local/go/bin/go ]]; then
  export PATH=/usr/local/go/bin:$PATH
fi

usage() {
  cat <<'EOF'
用法: sudo scripts/install.sh [选项]

选项:
  --check                 只执行完整安装前检查
  --with-monitor          同时安装可选的 sbackup-monitor
  --all-database-tools    安装 PostgreSQL、MySQL/MariaDB 和 SQLite 客户端
  --skip-runtime-update   不检查 Restic/rclone 官方更新
  --no-enable-timers      安装 systemd 单元但不启用维护定时器
  -h, --help              显示帮助

重复运行即为升级：二进制和 systemd 单元会更新，已有配置、密钥和状态不会被覆盖。
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check) mode=check ;;
    --with-monitor) with_monitor=true ;;
    --all-database-tools) all_database_tools=true ;;
    --skip-runtime-update) update_runtime=false ;;
    --no-enable-timers) enable_timers=false ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

[[ $(uname -s) == Linux ]] || { echo "SBackup 当前仅支持 Linux" >&2; exit 1; }

preflight_args=(--source-build)
$all_database_tools && preflight_args+=(--all-database-tools)
if [[ $mode == check ]]; then
  exec "$project_root/scripts/preflight.sh" "${preflight_args[@]}"
fi

[[ $(id -u) -eq 0 ]] || { echo "安装需要 root 权限（例如 sudo $0）" >&2; exit 1; }

package_manager=""
for candidate in apt-get dnf yum zypper pacman apk; do
  if command -v "$candidate" >/dev/null 2>&1; then package_manager=$candidate; break; fi
done
[[ -n $package_manager ]] || { echo "未找到受支持的包管理器" >&2; exit 1; }

install_packages() {
  local packages=()
  case "$package_manager" in
    apt-get)
      packages=(ca-certificates curl bzip2 unzip git tar)
      $all_database_tools && packages+=(sqlite3 postgresql-client default-mysql-client)
      apt-get update
      DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${packages[@]}"
      ;;
    dnf|yum)
      packages=(ca-certificates curl bzip2 unzip git tar)
      $all_database_tools && packages+=(sqlite postgresql mysql)
      "$package_manager" install -y "${packages[@]}"
      ;;
    zypper)
      packages=(ca-certificates curl bzip2 unzip git tar)
      $all_database_tools && packages+=(sqlite3 postgresql-client mariadb-client)
      zypper --non-interactive install "${packages[@]}"
      ;;
    pacman)
      packages=(ca-certificates curl bzip2 unzip git tar)
      $all_database_tools && packages+=(sqlite postgresql-libs mariadb-clients)
      pacman -Sy --needed --noconfirm "${packages[@]}"
      ;;
    apk)
      packages=(ca-certificates curl bzip2 unzip git tar)
      $all_database_tools && packages+=(sqlite postgresql-client mariadb-client)
      apk add --no-cache "${packages[@]}"
      ;;
  esac
}

go_version_ok() {
  command -v go >/dev/null 2>&1 || return 1
  local numeric
  numeric=$(go env GOVERSION 2>/dev/null || true)
  numeric=${numeric#go}
  [[ -n $numeric && $(printf '%s\n' 1.23 "$numeric" | sort -V | head -n1) == 1.23 ]]
}

install_go() {
  local go_arch tmp_dir metadata version archive sha
  case "$(uname -m)" in
    x86_64|amd64) go_arch=amd64 ;;
    aarch64|arm64) go_arch=arm64 ;;
    armv7l) go_arch=armv6l ;;
    *) echo "Go 官方包不支持当前架构" >&2; exit 1 ;;
  esac
  tmp_dir=$(mktemp -d)
  metadata="$tmp_dir/go.json"
  curl --fail --silent --show-error --location 'https://go.dev/dl/?mode=json' --output "$metadata"
  version=$(sed -n 's/.*"version": *"\(go[0-9.]*\)".*/\1/p' "$metadata" | head -n1)
  [[ -n $version ]] || { echo "无法读取 Go 最新稳定版" >&2; exit 1; }
  archive="${version}.linux-${go_arch}.tar.gz"
  sha=$(awk -v file="$archive" 'BEGIN{RS="\\{"} index($0,"\"filename\": \"" file "\""){if(match($0,/\"sha256\": \"[0-9a-f]+\"/)){x=substr($0,RSTART+11,RLENGTH-12); print x; exit}}' "$metadata")
  [[ -n $sha ]] || { echo "无法读取 $archive 的官方校验值" >&2; exit 1; }
  curl --fail --silent --show-error --location "https://go.dev/dl/$archive" --output "$tmp_dir/$archive"
  printf '%s  %s\n' "$sha" "$tmp_dir/$archive" | sha256sum --check --strict -
  tar -C "$tmp_dir" -xzf "$tmp_dir/$archive"
	local backup_dir=""
	if [[ -d /usr/local/go ]]; then
		backup_dir="/usr/local/go.sbackup-old-$(date +%s)"
		mv /usr/local/go "$backup_dir"
	fi
	mv "$tmp_dir/go" /usr/local/go
	if [[ -n $backup_dir ]]; then rm -rf "$backup_dir"; fi
	rm -rf "$tmp_dir"
  export PATH=/usr/local/go/bin:$PATH
}

echo "[1/6] 安装基础系统组件"
install_packages

echo "[2/6] 检查 Go 工具链"
if ! go_version_ok; then
  echo "安装 Go 官方最新稳定版"
  install_go
fi
export PATH=/usr/local/go/bin:$PATH
go version

echo "[3/6] 检查并更新 Restic/rclone"
if $update_runtime; then
  "$project_root/scripts/install-runtime-tools.sh" --update
else
  command -v restic >/dev/null 2>&1 || { echo "缺少 restic" >&2; exit 1; }
  command -v rclone >/dev/null 2>&1 || { echo "缺少 rclone" >&2; exit 1; }
fi

echo "[4/6] 测试并构建项目"
cd "$project_root"
GOTOOLCHAIN=local go test -buildvcs=false ./...
version=$(tr -d '[:space:]' < VERSION)
GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" -o bin/sbackup ./cmd/sbackup
install -m 0755 bin/sbackup /usr/local/bin/sbackup
if $with_monitor; then
  GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" -o bin/sbackup-monitor ./cmd/sbackup-monitor
  install -m 0755 bin/sbackup-monitor /usr/local/bin/sbackup-monitor
fi

echo "[5/6] 安装目录、配置和 systemd 单元"
install -d -m 0700 /etc/sbackup /etc/sbackup/secrets /var/lib/sbackup /var/lib/sbackup/tmp
install -d -m 0750 /var/log/sbackup
if [[ ! -f /etc/sbackup/config.yaml ]]; then
  /usr/local/bin/sbackup init
fi
install -m 0644 deploy/systemd/sbackup-maintenance.service /etc/systemd/system/sbackup-maintenance.service
install -m 0644 deploy/systemd/sbackup-maintenance.timer /etc/systemd/system/sbackup-maintenance.timer
if $with_monitor; then
  if ! id sbackup-monitor >/dev/null 2>&1; then useradd --system --home /var/lib/sbackup-monitor --shell /usr/sbin/nologin sbackup-monitor; fi
  install -d -o sbackup-monitor -g sbackup-monitor -m 0700 /var/lib/sbackup-monitor
  install -d -m 0750 /etc/sbackup-monitor
  if [[ ! -f /etc/sbackup-monitor/environment ]]; then
    install -m 0600 /dev/null /etc/sbackup-monitor/environment
    printf 'SBACKUP_MONITOR_USER=admin\nSBACKUP_MONITOR_PASSWORD=CHANGE-ME-BEFORE-START\n' > /etc/sbackup-monitor/environment
  fi
  install -m 0644 deploy/systemd/sbackup-monitor.service /etc/systemd/system/sbackup-monitor.service
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload
  $enable_timers && systemctl enable --now sbackup-maintenance.timer
fi

echo "[6/6] 安装后检查"
"$project_root/scripts/preflight.sh"
/usr/local/bin/sbackup doctor
echo "SBackup $version 安装/升级完成。配置文件: /etc/sbackup/config.yaml"
