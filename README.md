# SBackup

SBackup 是面向 Linux 单节点 VPS/服务器的轻量备份工具。客户端使用 Restic 完成加密、去重、快照和恢复，使用 rclone 连接 WebDAV；可选的 `sbackup-monitor` 只接收状态上报，不具备远程控制或 Shell 执行能力。

## 推荐安装：只下载静态二进制

默认安装不需要 Go、Git、Docker 或源码构建工具。它只安装客户端二进制、Restic，以及系统服务所需的最小目录。

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s --
```

脚本会从 [GitHub Releases](https://github.com/RY-zzcn/sbackup/releases) 下载对应架构的静态客户端，并校验 `SHA256SUMS`。也可以先下载脚本再审阅：

```bash
curl -fsSLo /tmp/sbackup-bootstrap.sh \
  https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh
less /tmp/sbackup-bootstrap.sh
sudo bash /tmp/sbackup-bootstrap.sh
```

架构支持：Linux amd64、arm64、armv7。

常用安装选项：

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s -- --with-webdav
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s -- --with-monitor --with-webdav
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s -- --all-database-tools
```

选项说明：

- `--with-webdav`：安装/更新官方 rclone；本地、SFTP、S3 仓库不需要它。
- `--with-monitor`：额外安装监控端二进制和 systemd 服务。
- `--all-database-tools`：安装 PostgreSQL、MariaDB/MySQL、SQLite 客户端；不使用数据库来源时不要安装。
- `--no-enable-timers`：安装但不启用 maintenance timer。

重复执行安装命令即为升级，不覆盖已有配置、密钥、状态和日志。卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s -- --help
sudo /usr/local/share/sbackup/scripts/uninstall.sh
```

> 如果使用 bootstrap 临时目录安装，建议先克隆仓库，或保存安装脚本；正式安装会把运维脚本复制到 `/usr/local/share/sbackup/scripts`。

## 运行时依赖

- 所有备份任务：系统级 `restic`。
- WebDAV：系统级 `rclone`，仅通过 `--with-webdav` 安装。
- PostgreSQL/MySQL/SQLite 来源：对应的 `pg_dump`、`mysqldump`、`sqlite3`，按需安装。
- 自动调度：推荐 systemd；没有 systemd 时仍可手工运行 CLI。

单独检查官方运行时最新版：

```bash
sudo /usr/local/share/sbackup/scripts/install-runtime-tools.sh --check
sudo /usr/local/share/sbackup/scripts/install-runtime-tools.sh --restic-only --update
```

## 首次配置和 Restic 密码

安装完成后：

```bash
sudoedit /etc/sbackup/config.yaml
sudo sbackup config validate
sudo sbackup doctor
```

每个 Restic 存储都有独立密码文件。推荐首次初始化时自动生成随机密码：

```bash
sudo sbackup storage password local-usb --generate
sudo sbackup storage init local-usb
```

或交互式执行：

```bash
sudo sbackup storage init local-usb
```

程序会让你选择“随机生成”或“自定义密码”。密码文件使用 `0600`、拒绝覆盖已有文件；随机密码只显示一次。请立即将它保存到离线密码管理器、加密 U 盘或纸质应急记录。密码丢失后，任何人都无法恢复 Restic 仓库数据。

非交互自定义密码不要写在命令行参数中，避免进入 shell history：

```bash
printf '%s\n' 'your-long-password-here' \
  | sudo sbackup storage password local-usb --stdin
```

## 日常使用

```bash
sudo sbackup
sudo sbackup status --json
sudo sbackup job list
sudo sbackup job run home
sudo sbackup snapshot list --job home
sudo sbackup restore --job home --snapshot latest --target /srv/restore-preview
sudo sbackup verify --job home --level standard
sudo sbackup retention apply --job home
sudo sbackup logs list --job home
```

恢复默认写入独立目录，禁止恢复到 `/` 或符号链接目标。交互菜单中的“查看和恢复备份”会逐步选择任务、快照、范围和目标，并在执行前要求输入 `YES` 确认；数据库备份只恢复导出文件，不自动覆盖生产数据库。

## 可选监控面板

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh \
  | sudo bash -s -- --with-monitor
sudoedit /etc/sbackup-monitor/environment
sudo systemctl enable --now sbackup-monitor.service
```

也可以直接使用多架构监控镜像（客户端仍推荐使用静态二进制）：

```bash
docker pull ghcr.io/ry-zzcn/sbackup-monitor:latest
git clone --depth=1 https://github.com/RY-zzcn/sbackup.git /tmp/sbackup-release
docker compose -f /tmp/sbackup-release/deploy/docker/docker-compose.yml up -d
```

监控端生产环境应通过 Nginx/Caddy 提供 HTTPS；客户端上报默认关闭，面板没有远程执行 API。

## 开发、源码构建与测试

普通 VPS 不需要这些步骤。开发者可以克隆仓库：

```bash
git clone https://github.com/RY-zzcn/sbackup.git
cd sbackup
PATH=/usr/local/go/bin:$PATH make check
PATH=/usr/local/go/bin:$PATH make build
```

源码安装（仅开发环境）：

```bash
git clone https://github.com/RY-zzcn/sbackup.git
cd sbackup
sudo ./scripts/install.sh --source-build
```

## 文档

- [安装、升级与运维](docs/INSTALLATION.md)
- [配置规范](docs/CONFIGURATION.md)
- [完整设计](docs/PROJECT-DESIGN.md)
- [监控协议](docs/MONITOR-PROTOCOL.md)
- [测试与发布检查](docs/TESTING.md)

## License

Apache-2.0，见 [LICENSE](LICENSE)。
