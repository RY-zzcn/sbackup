#!/usr/bin/env bash
set -euo pipefail

purge=false
yes=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge) purge=true ;;
    --yes) yes=true ;;
    *) echo "用法: sudo scripts/uninstall.sh [--purge] [--yes]" >&2; exit 2 ;;
  esac
  shift
done
[[ $(id -u) -eq 0 ]] || { echo "卸载需要 root 权限" >&2; exit 1; }

if $purge && ! $yes; then
  if [[ ! -t 0 ]]; then
    echo "非交互彻底清理必须显式指定 --yes" >&2
    exit 2
  fi
  echo "警告：将永久删除 /etc/sbackup、/var/lib/sbackup 和 /var/log/sbackup。"
  echo "Restic 仓库及其中快照不会删除。"
  read -r -p "输入 PURGE SBACKUP 确认: " confirm
  [[ $confirm == "PURGE SBACKUP" ]] || { echo "已取消。"; exit 0; }
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now sbackup-maintenance.timer 2>/dev/null || true
  while IFS= read -r unit; do
    [[ -n $unit ]] && systemctl disable --now "$unit" 2>/dev/null || true
  done < <(systemctl list-unit-files 'sbackup-job-*.timer' --no-legend 2>/dev/null | awk '{print $1}')
fi
rm -f /etc/systemd/system/sbackup-maintenance.service /etc/systemd/system/sbackup-maintenance.timer
job_units=(/etc/systemd/system/sbackup-job-*.service /etc/systemd/system/sbackup-job-*.timer)
for unit in "${job_units[@]}"; do
  [[ -e $unit ]] && rm -f "$unit"
done
rm -f /usr/local/bin/sbackup
rm -rf /usr/local/share/sbackup
command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload || true

if $purge; then
  echo "正在永久删除 SBackup 配置、状态和日志。Restic 仓库及系统级 Restic/rclone 不会删除。"
  rm -rf /etc/sbackup /var/lib/sbackup /var/log/sbackup
else
  echo "程序已卸载；配置、密钥、状态、日志和 Restic/rclone 已保留。"
fi
