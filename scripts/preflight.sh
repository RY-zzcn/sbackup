#!/usr/bin/env bash
set -uo pipefail

source_build=false
all_database_tools=false
config_path=""

if [[ -x /usr/local/go/bin/go ]]; then
  export PATH=/usr/local/go/bin:$PATH
fi

usage() {
  cat <<'EOF'
用法: scripts/preflight.sh [选项]

选项:
  --source-build          同时检查从源码构建所需的 Go 工具链
  --all-database-tools    检查 PostgreSQL、MySQL/MariaDB 和 SQLite 客户端
  --config PATH           若 sbackup 已安装，额外对指定配置执行 doctor
  -h, --help              显示帮助
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-build) source_build=true ;;
    --all-database-tools) all_database_tools=true ;;
    --config)
      [[ $# -ge 2 ]] || { echo "--config 缺少路径" >&2; exit 2; }
      config_path=$2
      shift
      ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

failures=0
warnings=0

ok() { printf '[OK]   %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*"; warnings=$((warnings + 1)); }
fail() { printf '[FAIL] %s\n' "$*"; failures=$((failures + 1)); }

if [[ $(uname -s) == Linux ]]; then
  ok "操作系统: Linux ($(uname -m))"
else
  fail "SBackup 当前仅支持 Linux"
fi

case "$(uname -m)" in
  x86_64|amd64|aarch64|arm64|armv7l) ok "CPU 架构受支持" ;;
  *) fail "CPU 架构不受支持: $(uname -m)" ;;
esac

package_manager=""
for candidate in apt-get dnf yum zypper pacman apk; do
  if command -v "$candidate" >/dev/null 2>&1; then
    package_manager=$candidate
    break
  fi
done
if [[ -n $package_manager ]]; then
  ok "包管理器: $package_manager"
else
  fail "未找到受支持的包管理器"
fi

for command_name in curl sha256sum bzip2 unzip tar install; do
  if command -v "$command_name" >/dev/null 2>&1; then
    ok "基础命令: $command_name"
  else
    fail "缺少基础命令: $command_name"
  fi
done

for runtime in restic rclone; do
  if resolved=$(command -v "$runtime" 2>/dev/null); then
    version=$($runtime version 2>&1 | sed -n '1p')
    ok "$runtime: $version ($resolved)"
  else
    fail "缺少运行时: $runtime"
  fi
done

if $source_build; then
  if resolved=$(command -v go 2>/dev/null); then
    version=$(go env GOVERSION 2>/dev/null || go version | awk '{print $3}')
    numeric=${version#go}
    if [[ $(printf '%s\n' 1.23 "$numeric" | sort -V | head -n1) == 1.23 ]]; then
      ok "Go 工具链: $version ($resolved)"
    else
      fail "Go 版本过低: $version，需要 1.23 或更高"
    fi
  else
    fail "缺少 Go 工具链（源码构建需要 Go 1.23+）"
  fi
  command -v git >/dev/null 2>&1 && ok "源码工具: git" || fail "缺少源码工具: git"
fi

if $all_database_tools; then
  for database_tool in pg_dump mysqldump sqlite3; do
    command -v "$database_tool" >/dev/null 2>&1 && ok "数据库工具: $database_tool" || fail "缺少数据库工具: $database_tool"
  done
else
  for database_tool in pg_dump mysqldump sqlite3; do
    command -v "$database_tool" >/dev/null 2>&1 || warn "可选数据库工具未安装: $database_tool"
  done
fi

if command -v systemctl >/dev/null 2>&1; then
  ok "systemd 调度工具可用"
else
  warn "未找到 systemctl；备份命令可运行，但自动调度需自行配置"
fi

if [[ -n $config_path ]]; then
  if command -v sbackup >/dev/null 2>&1; then
    if sbackup --config "$config_path" doctor; then
      ok "SBackup 配置与运行环境检查通过"
    else
      fail "SBackup doctor 检查失败"
    fi
  else
    fail "指定了 --config，但 sbackup 尚未安装"
  fi
fi

printf '\n检查完成: %d 个错误，%d 个提示。\n' "$failures" "$warnings"
[[ $failures -eq 0 ]]
