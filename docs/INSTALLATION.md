# 安装、升级与运维

## 安装路径

| 内容 | 路径 |
|---|---|
| 客户端 | `/usr/local/bin/sbackup` |
| 监控端 | `/usr/local/bin/sbackup-monitor` |
| 客户端配置 | `/etc/sbackup/config.yaml` |
| 客户端密钥 | `/etc/sbackup/secrets/` |
| 客户端状态 | `/var/lib/sbackup/` |
| 客户端日志 | `/var/log/sbackup/` |
| 监控配置 | `/etc/sbackup-monitor/environment` |
| 监控状态 | `/var/lib/sbackup-monitor/state.json` |

## 首次安装

```bash
./scripts/preflight.sh --source-build
sudo ./scripts/install.sh
```

数据库工具默认按需安装，以避免在只备份文件的主机上引入无关客户端。需要一次安装全部数据库客户端时使用 `--all-database-tools`。

安装器支持 apt、dnf、yum、zypper、pacman 和 apk。Go、Restic、rclone 的官方包均在安装前进行 SHA256 校验。

## 升级

拉取新版本后再次执行：

```bash
sudo ./scripts/install.sh
```

升级只替换程序和项目提供的 systemd 单元，不覆盖已有配置、密钥和状态。升级前仍建议备份 `/etc/sbackup` 和 `/var/lib/sbackup`。

## Runtime 更新

```bash
./scripts/install-runtime-tools.sh --check
sudo ./scripts/install-runtime-tools.sh --update
```

可使用 `--restic-only`、`--rclone-only` 或 `--force`。下载物只存在于安全临时目录，结束后自动清理。

## systemd

维护任务：

```bash
systemctl status sbackup-maintenance.timer
journalctl -u sbackup-maintenance.service
```

任务调度：

```bash
sbackup schedule install --job home
systemctl enable --now sbackup-job-home.timer
systemctl list-timers 'sbackup-*'
```

## 故障诊断

```bash
sbackup config validate
sbackup doctor
sbackup status
sbackup logs list
restic version
rclone version
```

`doctor` 会检查实际配置引用的数据库命令和所有密钥文件权限。Restic 和 rclone 无论当前是否使用 WebDAV都会检查，确保切换存储时不会突然缺少运行时。

## 备份配置与灾难恢复

至少离线保存：

- `/etc/sbackup/config.yaml`
- `/etc/sbackup/secrets/`
- `/etc/sbackup/rclone.conf`
- Restic 仓库密码
- 监控节点 secret（启用监控时）

Restic 仓库密码丢失后无法恢复仓库数据。状态数据库可以重建，但会丢失客户端运行历史和未发送 outbox。
