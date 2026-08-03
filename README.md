# SBackup

SBackup 是面向 Linux 单节点服务器的轻量备份工具。客户端使用 Restic 完成加密、去重、快照和恢复，使用 rclone 连接 WebDAV；可选的 `sbackup-monitor` 只接收状态上报，不具备远程控制或 Shell 执行能力。

## 功能

- 本地目录、WebDAV、SFTP、S3/MinIO Restic 仓库
- PostgreSQL、MySQL/MariaDB、SQLite 一致性导出
- 增量备份、快照查看、选择性恢复、保留策略和仓库验证
- systemd timer 自动调度、结构化日志和 SQLite WAL 状态库
- SMTP、Telegram、Webhook、Gotify、ntfy、Bark 通知及失败重试
- HTTPS HMAC 状态上报、心跳、离线 outbox、重放防护和可选监控面板
- 中文终端菜单及稳定的非交互 CLI

SBackup 不内置或分发 Restic、rclone 和数据库客户端。安装脚本会检测系统组件，并从官方来源安装或更新运行时工具。

## 支持范围

- Linux amd64、arm64、armv7
- Go 1.23 或更高（仅源码构建需要）
- systemd 为推荐调度器；没有 systemd 时仍可手动或通过其他调度器运行 CLI

## 安装前完整检查

```bash
./scripts/preflight.sh --source-build
```

只检查，不修改系统。若准备启用所有数据库来源：

```bash
./scripts/preflight.sh --source-build --all-database-tools
```

## 一键安装或升级

```bash
sudo ./scripts/install.sh
```

脚本会：

1. 识别 Linux、CPU 架构和包管理器；
2. 安装 curl、CA、解压缩和构建基础组件；
3. 检测 Go 1.23+，缺失或过旧时校验并安装 Go 官方最新稳定版；
4. 从 Restic 官方 GitHub Releases 下载最新版并校验官方 SHA256；
5. 从 rclone 官方下载站下载最新版并校验官方 SHA256；
6. 运行测试、构建并安装 `sbackup`；
7. 创建权限受限的配置、状态、临时和日志目录；
8. 安装并启用 maintenance systemd timer；
9. 执行安装后预检和 `sbackup doctor`。

重复执行即为升级，不覆盖 `/etc/sbackup/config.yaml`、密钥、状态或日志。常用选项：

```bash
sudo ./scripts/install.sh --all-database-tools
sudo ./scripts/install.sh --with-monitor
sudo ./scripts/install.sh --skip-runtime-update
sudo ./scripts/install.sh --no-enable-timers
```

仅检查或更新 Restic/rclone：

```bash
./scripts/install-runtime-tools.sh --check
sudo ./scripts/install-runtime-tools.sh --update
```

## 首次配置

安装器首次运行会创建 `/etc/sbackup/config.yaml`。编辑后执行：

```bash
sudoedit /etc/sbackup/config.yaml
sudo sbackup config validate
sudo sbackup doctor
sudo sbackup storage init local-usb
sudo sbackup storage test local-usb
sudo sbackup schedule install --job home
sudo systemctl enable --now sbackup-job-home.timer
```

所有仓库密码、数据库凭据、通知 token 和监控 key 必须写入独立的 `0600` 文件。完整示例见 [配置文档](docs/CONFIGURATION.md)。

## 日常使用

直接进入交互菜单：

```bash
sudo sbackup
```

常用自动化命令：

```bash
sbackup status --json
sbackup job list
sbackup job run home
sbackup snapshot list --job home
sbackup snapshot show --job home --snapshot latest
sbackup snapshot files --job home --snapshot latest --path /etc
sbackup restore --job home --snapshot latest --target /srv/restore-preview
sbackup verify --job home --level standard
sbackup retention apply --job home
sbackup logs list --job home
sbackup notification test mail-main
sbackup monitor test
sbackup config export
```

恢复目标必须是非根目录绝对路径，且不能是符号链接。数据库恢复默认只恢复导出文件，不覆盖生产数据库。

## 监控面板

安装：

```bash
sudo ./scripts/install.sh --with-monitor
sudoedit /etc/sbackup-monitor/environment
```

生成节点及一次性密钥：

```bash
sudo -u sbackup-monitor sbackup-monitor \
  --data /var/lib/sbackup-monitor/state.json \
  --add-node node-a --node-name server-a --generate-key
```

把输出的 `node_secret` 保存到客户端 `0600` key 文件，并在客户端配置 HTTPS 上报地址。生产环境应使用 Nginx/Caddy 等反向代理提供 TLS；面板默认只监听 `127.0.0.1:8788`。

## 源码构建与测试

```bash
PATH=/usr/local/go/bin:$PATH make check
PATH=/usr/local/go/bin:$PATH make build
```

若系统没有 `make`：

```bash
GOTOOLCHAIN=local go test -buildvcs=false ./...
GOTOOLCHAIN=local go vet -buildvcs=false ./...
GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -buildvcs=false -o bin/sbackup ./cmd/sbackup
```

测试套件包含真实本地 Restic 仓库演练：初始化、两次增量备份、快照查询、旧快照恢复、哈希校验和仓库 check。

## 卸载

```bash
sudo ./scripts/uninstall.sh
```

默认只删除程序和 systemd 单元，保留配置、密钥、状态、日志、Restic/rclone 和备份仓库。永久删除本机 SBackup 数据需显式使用：

```bash
sudo ./scripts/uninstall.sh --purge
```

`--purge` 也不会删除远端或本地 Restic 仓库，不会卸载系统级 Restic/rclone。

## 文档

- [完整设计](docs/PROJECT-DESIGN.md)
- [配置规范](docs/CONFIGURATION.md)
- [监控协议](docs/MONITOR-PROTOCOL.md)
- [安装和运维](docs/INSTALLATION.md)
- [测试与发布检查](docs/TESTING.md)

## License

Apache-2.0，见 [LICENSE](LICENSE)。
