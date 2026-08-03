# 安装、升级与运维

## 安装路径

| 内容 | 路径 |
|---|---|
| 客户端 | `/usr/local/bin/sbackup` |
| 客户端配置 | `/etc/sbackup/config.yaml` |
| 客户端密钥 | `/etc/sbackup/secrets/` |
| 客户端状态 | `/var/lib/sbackup/` |
| 客户端日志 | `/var/log/sbackup/` |

## 首次安装（推荐二进制方式）

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s --
```

默认从 GitHub Releases 下载静态客户端，不在 VPS 安装 Go、Git 或构建链。rclone 在菜单首次配置 WebDAV 时自动安装，数据库工具按任务实际引用按需准备。

安装器支持 apt、dnf、yum、zypper、pacman 和 apk。SBackup Release、Restic、rclone 下载物均在安装前进行 SHA256 校验。监控端独立通过 Docker/GHCR 部署，不进入普通客户端安装流程。

## 升级

再次执行 bootstrap 即可升级：

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s --
```

升级只替换程序和项目提供的 systemd 单元，不覆盖已有配置、密钥和状态。升级前仍建议备份 `/etc/sbackup` 和 `/var/lib/sbackup`。

## Runtime 更新

```bash
sudo /usr/local/share/sbackup/scripts/install-runtime-tools.sh --restic-only --check
sudo /usr/local/share/sbackup/scripts/install-runtime-tools.sh --restic-only --update
```

只有配置 WebDAV 时才使用 `--rclone-only`；下载物只存在于安全临时目录，结束后自动清理。

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

`doctor` 会检查实际配置引用的数据库命令、Restic 和 WebDAV 所需的 rclone，以及所有密钥文件权限。使用本地、SFTP 或 S3 时不要求安装 rclone。

## 备份配置与灾难恢复

至少离线保存：

- `/etc/sbackup/config.yaml`
- `/etc/sbackup/secrets/`
- `/etc/sbackup/rclone.conf`
- Restic 仓库密码
- 监控节点 secret（启用监控时）

Restic 仓库密码丢失后无法恢复仓库数据。状态数据库可以重建，但会丢失客户端运行历史和未发送 outbox。

## 仓库密码初始化

每个仓库独立设置密码：

```bash
sbackup storage password local-usb --generate
sbackup storage init local-usb
```

随机密码只显示一次，并保存到配置中 `password_file` 指向的 `0600` 文件。必须离线保存。也可以直接交互执行 `sbackup storage init local-usb`，选择自定义密码或自动生成。
