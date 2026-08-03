#!/usr/bin/env bash
set -euo pipefail

purge=false
if [[ ${1:-} == --purge ]]; then purge=true; shift; fi
if [[ $# -ne 0 ]]; then echo "用法: sudo scripts/uninstall.sh [--purge]" >&2; exit 2; fi
[[ $(id -u) -eq 0 ]] || { echo "卸载需要 root 权限" >&2; exit 1; }

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now sbackup-maintenance.timer 2>/dev/null || true
  systemctl disable --now sbackup-monitor.service 2>/dev/null || true
fi
rm -f /etc/systemd/system/sbackup-maintenance.service /etc/systemd/system/sbackup-maintenance.timer /etc/systemd/system/sbackup-monitor.service
rm -f /usr/local/bin/sbackup /usr/local/bin/sbackup-monitor
rm -rf /usr/local/share/sbackup
command -v systemctl >/dev/null 2>&1 && systemctl daemon-reload || true

if $purge; then
  echo "正在永久删除 SBackup 配置、状态和日志。Restic 仓库及系统级 Restic/rclone 不会删除。"
  rm -rf /etc/sbackup /etc/sbackup-monitor /var/lib/sbackup /var/lib/sbackup-monitor /var/log/sbackup
else
  echo "程序已卸载；配置、密钥、状态、日志和 Restic/rclone 已保留。"
fi
