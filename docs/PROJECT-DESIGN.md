# SBackup 完整项目设计

## 1. 项目定位

SBackup 是一个面向个人服务器、家庭实验室、小型业务服务器和边缘节点的单机备份工具。它关注的是“一台服务器如何稳定、可恢复、容易观察地完成备份”，而不是构建远程运维控制平台。

项目应当满足以下定位：

1. 每台服务器能够独立完成备份，不依赖监控面板在线。
2. 管理入口以终端菜单为主，能够通过 SSH 完成初始化、修改配置、备份、恢复和诊断。
3. 同时提供非交互式 CLI，便于 systemd、cron、Ansible 和脚本调用。
4. 支持本地目录、SFTP、S3/MinIO 和 WebDAV 等常见目标。
5. 使用成熟备份引擎处理加密、去重、增量快照和完整性检查。
6. 可选向独立监控面板上报状态，以集中观察多台独立服务器，但监控面板不成为备份控制面。
7. 本地通知与中心告警相互补充；关闭监控后，本地通知仍能工作。

### 1.1 目标用户

- 只有一台或少量 Linux 服务器的个人用户。
- 需要通过 WebDAV、SFTP、NAS 或对象存储保存备份的用户。
- 希望使用终端管理，但又需要直观查看备份健康状态的运维人员。
- 不希望为了备份部署数据库集群、消息队列或复杂 Agent 平台的小型团队。

### 1.2 非目标

第一阶段明确不实现：

- 从监控面板远程执行备份、恢复或命令。
- 在浏览器中编辑客户端备份策略。
- 任意 pre/post Shell Hook。
- 跨主机文件浏览和远程文件管理。
- 多租户、复杂 RBAC 或企业 SSO。
- 自研加密、去重或备份存储格式。
- Windows 和 macOS 原生支持；第一阶段专注 Linux。

## 2. 核心设计原则

### 2.1 本地自治

配置、调度和任务执行都发生在本机。监控面板不可达时：

- 备份照常运行；
- 结果写入本地数据库；
- 上报事件进入本地重试队列；
- 本地通知照常发送；
- 后续通过定时维护任务重试上报。

### 2.2 终端优先

`sbackup` 不启动管理 Web 服务。运行不带子命令的 `sbackup` 进入交互式菜单；所有重要操作也必须有对应的非交互子命令。

终端交互应兼容：

- 普通 SSH；
- 无颜色终端；
- 非 TTY 环境下的清晰错误提示；
- 中文和英文文案扩展；
- Ctrl+C 安全退出，不留下半写配置。

### 2.3 最小权限和显式危险操作

- 默认不允许执行任意用户命令。
- 恢复默认只能写入独立目标目录。
- 原位置覆盖恢复必须同时提供 `--original-paths` 和 `--force`。
- 删除快照、执行 prune、覆盖配置等操作必须二次确认。
- 密码不写入主配置和任务日志。

### 2.4 状态上报不是远程控制

监控面板仅接受以下类型的数据：

- 主机心跳；
- 任务定义摘要；
- 任务开始、成功、失败和警告；
- 校验结果；
- 下次计划执行时间；
- 不包含凭据的容量和耗时统计。

面板不能向客户端下发命令、策略、脚本、更新包或凭据。

## 3. 总体功能范围

### 3.1 备份来源

第一阶段支持：

- 普通文件和目录；
- PostgreSQL 逻辑备份；
- MySQL/MariaDB 逻辑备份；
- SQLite 一致性副本；
- 可选包含 Docker Compose 文件和显式指定的持久化目录。

备份来源不自动扫描整个 Docker 环境。自动发现容易把临时卷、秘密文件和错误路径纳入备份。交互菜单可以辅助用户选择目录，但最终配置必须明确记录实际路径。

### 3.2 存储目标

计划支持：

- 本地目录或挂载磁盘；
- SFTP；
- S3 兼容存储，包括 MinIO；
- WebDAV；
- Rest Server，可作为后续增强。

底层统一使用 Restic 仓库。WebDAV 通过 Restic 的 rclone backend 接入：

```text
restic repository = rclone:<remote>:<path>
                 ↓
               rclone
                 ↓
              WebDAV
```

例如：

```text
rclone:webdav-main:/server-backups/host-a/home
```

不建议将 WebDAV 挂载到本地后当成普通目录仓库，因为网络文件系统的锁、原子重命名、缓存和断线行为可能损坏备份体验。

### 3.3 备份能力

- Restic 加密仓库初始化。
- 增量备份和内容去重。
- include/exclude 规则。
- 文件系统边界控制，例如是否跨越挂载点。
- 标签和主机名标记。
- 可配置并发、超时、带宽限制和重试。
- 备份任务互斥锁。
- 任务超时和取消。
- 备份后保留策略。
- 可选 prune，默认采用低频执行以降低仓库风险和耗时。
- 输出快照 ID、扫描文件数、数据量、耗时和错误摘要。

### 3.4 快照和恢复

- 列出任务快照。
- 查看单个快照摘要。
- 浏览快照中的文件列表。
- 恢复整个快照。
- 恢复指定路径。
- 恢复到临时目录进行预览。
- 比较快照内容与当前文件。
- 明确区分“恢复完成”和“业务已可用”；数据库恢复只负责导出备份文件，不默认覆盖生产数据库。

### 3.5 完整性验证

验证分为三个等级：

1. `metadata`：执行仓库访问和快照元数据检查，适合每次备份后运行。
2. `standard`：执行 `restic check`，建议每周运行。
3. `full`：读取全部或指定比例的数据包进行校验，建议每月或季度运行。

验证失败是独立事件，不能被最近一次备份成功覆盖。监控面板应同时展示“最近备份状态”和“最近验证状态”。

### 3.6 保留策略

支持：

- `keep_last`；
- `keep_hourly`；
- `keep_daily`；
- `keep_weekly`；
- `keep_monthly`；
- `keep_yearly`；
- `keep_within`；
- 按任务标签分组保留。

建议默认策略：

```yaml
keep_last: 3
keep_daily: 14
keep_weekly: 8
keep_monthly: 12
keep_yearly: 3
```

`forget` 和 `prune` 分开处理：

- 每次成功备份后可以执行 `forget`；
- `prune` 默认每周或每月执行一次；
- prune 失败将任务标记为 `warning`，不把已经成功创建的快照改成失败。

## 4. 终端交互设计

### 4.1 主菜单

运行：

```bash
sbackup
```

显示：

```text
SBackup

1. 查看备份状态
2. 立即执行备份
3. 管理备份任务
4. 查看和恢复快照
5. 验证备份仓库
6. 管理存储目标
7. 管理通知方式
8. 管理监控上报
9. 调度与自动备份
10. 日志与故障诊断
11. 全局设置
0. 退出
```

### 4.2 任务创建向导

交互流程：

```text
任务名称
  ↓
选择存储目标
  ↓
选择目录或数据库来源
  ↓
配置排除规则
  ↓
配置执行时间和错过补跑
  ↓
配置保留策略
  ↓
选择通知事件
  ↓
选择是否向监控面板上报
  ↓
执行配置检查
  ↓
可选执行首次试备份
```

向导结束时必须展示最终摘要，用户确认后才原子写入配置。

### 4.3 WebDAV 创建向导

交互字段：

- 配置名称；
- WebDAV URL；
- 服务类型：通用、Nextcloud、OwnCloud、SharePoint 等；
- 用户名；
- 密码或应用密码；
- 远程根目录；
- 是否校验 TLS；
- 自定义 CA 文件；
- 连接超时；
- 上传并发数；
- 是否启用显式私网地址访问。

保存前执行：

1. URL 语法检查；
2. TLS 检查；
3. rclone 连接检查；
4. 创建并删除随机临时对象；
5. Restic 仓库探测或初始化确认。

WebDAV 密码通过 `rclone obscure` 后写入独立的 rclone 配置文件，文件权限必须为 `0600`。需要强调：obscure 不是加密存储，只是避免明文展示，真正安全依赖文件权限和主机安全。

### 4.4 非交互 CLI

建议命令树：

```text
sbackup init
sbackup status [--json]

sbackup job list
sbackup job show <id>
sbackup job add [--interactive]
sbackup job edit <id> [--interactive]
sbackup job enable <id>
sbackup job disable <id>
sbackup job run <id>
sbackup job run-all
sbackup job remove <id>

sbackup storage list
sbackup storage add [--type webdav|local|sftp|s3]
sbackup storage edit <id>
sbackup storage test <id>
sbackup storage remove <id>

sbackup snapshot list --job <id>
sbackup snapshot show --job <id> --snapshot <id>
sbackup snapshot files --job <id> --snapshot <id> [--path <path>]

sbackup restore --job <id> --snapshot <id|latest> --target <dir>
sbackup restore --job <id> --snapshot <id> --include <path> --target <dir>

sbackup verify --job <id> --level metadata|standard|full
sbackup retention apply --job <id>
sbackup prune --job <id>

sbackup notification list
sbackup notification add [--type smtp|telegram|webhook|gotify|ntfy|bark|wecom|dingtalk|feishu]
sbackup notification test <id>
sbackup notification remove <id>

sbackup monitor configure
sbackup monitor test
sbackup monitor enable
sbackup monitor disable
sbackup monitor heartbeat
sbackup monitor flush

sbackup schedule install --job <id>
sbackup schedule enable --job <id>
sbackup schedule disable --job <id>
sbackup schedule status

sbackup logs list [--job <id>]
sbackup logs show <run-id>
sbackup doctor
sbackup config validate
sbackup config export --redacted
```

### 4.5 退出码

建议固定退出码，方便自动化判断：

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 1 | 通用错误 |
| 2 | 配置错误 |
| 3 | 仓库不可用 |
| 4 | 备份任务失败 |
| 5 | 恢复失败 |
| 6 | 验证失败 |
| 7 | 任务已在运行 |
| 8 | 用户取消 |
| 9 | 部分成功或警告 |

## 5. 技术架构

### 5.1 组件图

```text
┌──────────────────────── 单节点服务器 ────────────────────────┐
│                                                              │
│  用户 / SSH                                                  │
│      │                                                       │
│      ▼                                                       │
│  sbackup CLI + 终端菜单                                      │
│      │                                                       │
│      ├── 配置管理 ─────── YAML + secrets                     │
│      ├── 状态记录 ─────── SQLite                             │
│      ├── 调度管理 ─────── systemd timer / cron               │
│      ├── 数据库导出 ───── pg_dump / mysqldump / SQLite API   │
│      ├── 备份执行 ─────── Restic                             │
│      │                         │                              │
│      │                         ├── 本地 / SFTP / S3           │
│      │                         └── rclone ── WebDAV           │
│      ├── 本地通知 ─────── SMTP / Telegram / Gotify / ...     │
│      └── 可选状态上报 ─── HTTPS + HMAC                       │
│                                      │                       │
└──────────────────────────────────────┼───────────────────────┘
                                       ▼
                         ┌──────────────────────────┐
                         │ sbackup-monitor（可选）  │
                         │                          │
                         │ 接收上报 / 漏报判断      │
                         │ SQLite / Web 仪表盘      │
                         │ 中心告警 / Prometheus    │
                         └──────────────────────────┘
```

### 5.2 技术选型

核心客户端建议：

- 语言：Go；
- CLI：Cobra，或保持接口兼容的轻量命令路由；
- 终端表单：优先自研小型 prompt 层，必要时使用 `charmbracelet/huh`；
- 配置：YAML；
- 状态库：SQLite，使用纯 Go 驱动以简化交叉编译；
- 备份引擎：Restic；
- WebDAV：rclone；
- 调度：systemd timer，兼容 cron；
- 日志：结构化 JSON 文件加终端友好输出。

监控面板建议：

- 语言：Go，与客户端共用协议类型和签名库；
- HTTP：标准库或 Chi；
- 数据库：SQLite，后续可选 PostgreSQL；
- 页面：Go template + 内嵌 CSS + 少量原生 JavaScript；
- 页面刷新：SSE 或定时请求；
- 不引入 Node.js 构建链；
- 单二进制内嵌静态资源；
- Docker 镜像作为推荐部署方式。

### 5.3 二进制

仓库生成两个主要二进制：

```text
sbackup
sbackup-monitor
```

可选辅助命令不应拆成常驻服务。客户端主要依赖 systemd 定时调用：

```text
sbackup job run <id>
sbackup maintenance
sbackup monitor heartbeat
```

### 5.4 进程和并发模型

- 同一任务只允许一个实例运行。
- 默认全局最多同时运行一个备份任务，避免磁盘和网络争抢。
- 可在高级设置中提高全局并发，但每个仓库仍需要独立锁。
- 恢复、prune 和 full check 与同一仓库的备份互斥。
- 锁文件放在 `/run/lock/sbackup/`，SQLite 同时记录运行租约，用于识别异常退出。

## 6. 文件与目录布局

推荐 Linux FHS 布局：

```text
/usr/local/bin/sbackup
/etc/sbackup/config.yaml
/etc/sbackup/rclone.conf
/etc/sbackup/secrets/
    repositories/<storage-id>.pass
    databases/<source-id>.env
    notifications/<channel-id>.json
    monitor.key
/var/lib/sbackup/state.db
/var/lib/sbackup/spool/reports/
/var/lib/sbackup/spool/notifications/
/var/lib/sbackup/tmp/
/var/log/sbackup/
/run/lock/sbackup/
```

权限建议：

```text
/etc/sbackup                 0700 root:root
config.yaml                  0600 root:root
rclone.conf                  0600 root:root
secrets/*                    0600 root:root
/var/lib/sbackup             0700 root:root
/var/log/sbackup             0750 root:adm
```

后续可以支持专用系统用户，但文件系统备份通常需要读取多个受限目录。第一版可由 root 运行，同时通过“不执行任意命令”和严格参数化减少风险。

## 7. 核心模块设计

### 7.1 配置模块

职责：

- 加载和版本迁移；
- Schema 校验；
- 默认值填充；
- 引用完整性检查；
- 原子写入；
- 自动备份上一版本；
- 输出脱敏配置。

写入方式：

1. 写入同目录临时文件；
2. `fsync`；
3. 设置权限；
4. 原子 rename；
5. 保留最近三份配置备份。

### 7.2 存储适配模块

统一接口：

```go
type Repository interface {
    ID() string
    ResticRepository() string
    Environment(ctx context.Context) ([]string, error)
    Preflight(ctx context.Context) error
    Init(ctx context.Context) error
    Capabilities() Capabilities
}
```

WebDAV adapter 负责生成或引用 rclone remote，Restic 只看到 `rclone:` 仓库地址。

### 7.3 备份执行模块

建议状态机：

```text
queued
  → preflight
  → preparing_sources
  → backing_up
  → retention
  → verifying_metadata
  → completed | warning | failed | cancelled
```

每次状态转换都写 SQLite，并产生可选上报事件。

单次运行步骤：

1. 加载配置快照；
2. 获取任务和仓库锁；
3. 创建 run 记录；
4. 上报 `run.started`；
5. 检查二进制、源路径、临时空间和仓库连接；
6. 生成数据库 dump；
7. 构造 Restic 参数数组；
8. 解析 Restic JSON 输出；
9. 保存快照和容量统计；
10. 执行保留策略；
11. 可选元数据验证；
12. 清理临时文件；
13. 写最终状态；
14. 发送本地通知；
15. 上报最终状态；
16. 释放锁。

### 7.4 命令执行安全

所有外部命令必须使用参数数组：

```go
exec.CommandContext(ctx, "restic", args...)
```

禁止：

```go
exec.CommandContext(ctx, "sh", "-c", userInput)
```

执行器还应：

- 设置最小化环境变量；
- 从日志中移除密码和 Token；
- 限制输出单行和总大小；
- 支持超时和进程组终止；
- 记录可公开的参数，但隐藏密码文件和敏感 URL 查询参数。

### 7.5 数据库导出模块

统一接口：

```go
type Dumper interface {
    Validate(ctx context.Context) error
    Dump(ctx context.Context, targetDir string) (Artifact, error)
}
```

PostgreSQL：

- 使用 `pg_dump --format=custom`；
- 密码通过临时 `PGPASSFILE`；
- 临时文件权限 `0600`；
- 支持 TLS 参数。

MySQL/MariaDB：

- 使用 `mysqldump --single-transaction --quick`；
- 凭据通过权限受限的临时 defaults-extra-file；
- 不把密码放进命令行。

SQLite：

- 优先使用 SQLite online backup API；
- 不直接复制仍在写入的数据库文件；
- 同时处理 `-wal` 和 `-shm` 的一致性问题。

### 7.6 状态库

客户端 SQLite 表建议：

```sql
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    phase TEXT NOT NULL,
    scheduled_at TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    snapshot_id TEXT,
    files_new INTEGER,
    files_changed INTEGER,
    files_unmodified INTEGER,
    bytes_added INTEGER,
    bytes_processed INTEGER,
    duration_ms INTEGER,
    warning TEXT,
    error_code TEXT,
    error_summary TEXT,
    log_path TEXT NOT NULL
);

CREATE TABLE verification_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    level TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    error_summary TEXT
);

CREATE TABLE outbox (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    destination_id TEXT NOT NULL,
    payload BLOB NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_error TEXT
);
```

配置仍以 YAML 为事实来源，SQLite 不重复存储敏感配置。

## 8. WebDAV 详细设计

### 8.1 接入方式

主配置只保存 WebDAV 存储 ID 和非敏感选项。实际 rclone remote 写入 `/etc/sbackup/rclone.conf`：

```ini
[webdav-main]
type = webdav
url = https://dav.example.com/remote.php/dav/files/backup
vendor = nextcloud
user = backup-user
pass = <rclone-obscured-value>
```

Restic 使用：

```bash
restic -r rclone:webdav-main:/sbackup/host-a/home snapshots
```

### 8.2 WebDAV 连接策略

默认设置应偏保守：

- TLS 校验开启；
- HTTP 超时 60 秒；
- 低上传并发；
- 有限次数指数退避；
- 不跟随跨域重定向携带 Authorization；
- 默认拒绝明文 HTTP，需显式确认才能启用；
- 自定义 CA 优先于关闭 TLS 校验；
- 避免过高并发触发网盘限流。

### 8.3 失败分类

WebDAV 错误需要转换为用户可理解的类别：

- DNS 解析失败；
- TLS 证书失败；
- 401/403 凭据或权限错误；
- 404 路径错误；
- 409/423 锁冲突；
- 429 限流；
- 507 空间不足；
- 5xx 服务端故障；
- 上传超时或连接重置。

监控和通知只发送分类与脱敏摘要，不发送完整 URL、用户名或响应正文。

## 9. 自动调度

### 9.1 systemd timer

Linux systemd 是首选调度器。每个任务生成：

```text
sbackup-job@<job-id>.service
sbackup-job@<job-id>.timer
```

服务模板：

```ini
[Unit]
Description=SBackup job %i
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/sbackup job run %i --scheduled
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
```

Timer 使用 `Persistent=true`，支持错过后补跑，并加入随机延迟：

```ini
RandomizedDelaySec=10m
Persistent=true
```

### 9.2 维护任务

单独安装维护 timer，例如每 10 分钟执行：

```bash
sbackup maintenance
```

职责：

- 重试监控上报；
- 重试通知；
- 清理过期临时文件；
- 修复异常退出留下的租约；
- 发送可选心跳；
- 执行到期的定期 verify/prune。

### 9.3 cron 兼容

没有 systemd 时，CLI 输出明确的 crontab 行。cron 模式不应降低任务锁、日志和状态上报能力。

## 10. 多种通知方式

### 10.1 通知事件

支持按通道订阅：

- 备份开始，可选，默认关闭；
- 备份成功，可选；
- 备份失败，默认开启；
- 备份完成但有警告，默认开启；
- 验证失败，默认开启；
- 仓库不可访问；
- WebDAV 限流或空间不足；
- 连续失败达到阈值；
- 恢复成功或失败；
- 监控上报持续失败；
- 配置校验失败。

### 10.2 第一阶段通知通道

建议首发支持：

1. SMTP 邮件；
2. Telegram Bot；
3. 通用 Webhook；
4. Gotify；
5. ntfy；
6. Bark。

第二阶段增加：

- 企业微信机器人；
- 钉钉机器人；
- 飞书机器人；
- Slack/Discord；
- 本地 syslog/journald；
- 自定义 Apprise 网关。

### 10.3 通知接口

```go
type Notifier interface {
    ID() string
    Validate(ctx context.Context) error
    Send(ctx context.Context, event Event) error
}
```

通知内容必须包含：

- 主机显示名；
- 任务名；
- 状态；
- 开始和结束时间；
- 持续时间；
- 快照 ID 的短形式；
- 数据量；
- 脱敏错误摘要；
- 连续失败次数；
- 监控面板链接，可选。

### 10.4 去重、冷却和恢复通知

避免故障期间刷屏：

- 同一任务同一错误指纹在冷却时间内合并；
- 首次失败立即通知；
- 连续失败按 2、5、10、每 10 次提醒；
- 从失败恢复到成功时发送恢复通知；
- 成功通知可以按任务独立关闭；
- 通知重试使用指数退避，并设置最大保存时间。

### 10.5 通知安全

- 密钥放入 secrets 目录；
- 错误响应不得原样返回到终端或监控面板；
- Webhook 默认只允许 HTTPS；
- Webhook 默认拒绝私网、环回和 link-local 地址，用户可在本地配置中显式放开；
- 禁止把仓库密码、数据库密码、完整 WebDAV URL 和 HTTP Authorization 写入通知。

## 11. 可选监控面板

### 11.1 定位

`sbackup-monitor` 是可选的备份状态观察与告警服务，外观和使用体验可以接近常见可用性监控面板，但监控对象是备份任务而不是普通 HTTP 站点。

它解决：

- 最近一次自动备份是否成功；
- 计划时间到了但客户端没有上报；
- 主机是否长时间没有心跳；
- 是否连续失败；
- 最近一次完整性验证是否过期或失败；
- 仓库容量和备份耗时是否出现异常趋势。

### 11.2 明确边界

面板允许：

- 创建监控节点和上报密钥；
- 配置预期执行间隔和宽限时间；
- 查看运行历史；
- 暂停监控告警；
- 配置中心通知通道；
- 导出状态和指标。

面板禁止：

- 远程触发备份、恢复、prune 或 verify；
- 修改客户端目录和仓库配置；
- 获取客户端凭据；
- 下发 Shell 命令；
- 自动更新客户端。

### 11.3 展示内容

首页卡片：

```text
主机 / 任务
当前状态
最近成功时间
最近尝试时间
下次预期时间
连续失败次数
最近耗时
最近数据增量
最近验证状态
```

状态定义：

- `healthy`：最近备份成功且未超过预期时间；
- `warning`：备份成功但清理、通知或上报有非核心错误，或验证即将过期；
- `critical`：最近备份失败或验证失败；
- `missed`：超过预期时间和宽限期仍未收到结果；
- `offline`：主机心跳超时；
- `paused`：人为暂停告警；
- `unknown`：尚未收到足够数据。

### 11.4 漏报判断

客户端上报任务摘要：

- schedule 表达式或固定间隔；
- 时区；
- 下一次预期执行时间；
- 宽限时间；
- 任务是否启用。

面板以客户端提供的 `next_expected_at` 为主，避免服务端重新解释不同调度语法。

判断逻辑：

```text
now <= next_expected_at + grace_period  → 正常
now >  next_expected_at + grace_period  → missed
```

当任务下一次正常成功后，发送恢复通知。

### 11.5 主机心跳

状态上报分两种：

- 任务事件：只在任务开始和结束时上报；
- 主机心跳：可选，建议每 5 分钟上报。

如果用户只关心备份结果，可以关闭心跳。关闭后面板只能判断任务漏报，不能区分“主机离线”和“调度任务未运行”。

### 11.6 面板部署

推荐 Docker Compose：

```yaml
services:
  sbackup-monitor:
    image: example/sbackup-monitor:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8788:8788"
    volumes:
      - ./data:/var/lib/sbackup-monitor
      - ./config.yaml:/etc/sbackup-monitor/config.yaml:ro
```

生产环境通过 Caddy、Nginx 或 Traefik 提供 HTTPS。面板默认绑定 `127.0.0.1`；绑定公网地址时必须完成管理员密码初始化。

### 11.7 面板认证

管理页面第一阶段使用：

- 单管理员账号；
- Argon2id 密码哈希；
- HttpOnly、Secure、SameSite Cookie；
- CSRF 防护；
- 登录限流；
- 可选 TOTP 二次认证作为后续增强。

客户端上报使用每节点独立密钥，不使用管理员 Cookie。

### 11.8 面板数据库

主要表：

```sql
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    last_heartbeat_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE jobs (
    node_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    timezone TEXT NOT NULL,
    next_expected_at TEXT,
    grace_seconds INTEGER NOT NULL,
    last_status TEXT,
    last_attempt_at TEXT,
    last_success_at TEXT,
    last_verification_at TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(node_id, job_id)
);

CREATE TABLE reports (
    event_id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    job_id TEXT,
    event_type TEXT NOT NULL,
    status TEXT,
    occurred_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    payload_json TEXT NOT NULL
);

CREATE TABLE incidents (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    job_id TEXT,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    opened_at TEXT NOT NULL,
    resolved_at TEXT,
    message TEXT NOT NULL
);
```

## 12. 上报可靠性和安全

### 12.1 认证与签名

每个节点具有：

- `node_id`；
- 独立 HMAC 密钥；
- 可轮换密钥版本。

请求头：

```text
X-SBackup-Node: node_01...
X-SBackup-Timestamp: 1785762600
X-SBackup-Nonce: ...
X-SBackup-Signature: v1=<hex hmac-sha256>
```

签名内容应包含：

```text
HTTP method
request path
timestamp
nonce
SHA-256(body)
```

服务端拒绝：

- 时间偏差超过五分钟；
- 重复 nonce；
- 未知节点；
- 签名不匹配；
- 请求体超限；
- event ID 冲突且内容不同。

### 12.2 幂等和离线队列

每个事件有 UUIDv7 `event_id`。面板对 event ID 做幂等处理。

客户端上报失败时写入 outbox：

```text
立即重试 1 次
1 分钟
5 分钟
15 分钟
1 小时
6 小时
之后每 12 小时，直到过期
```

备份成功不因上报失败而变成失败；最终状态可以是：

```text
backup=success, report=pending
```

### 12.3 数据最小化

禁止上报：

- 文件名清单；
- 备份源绝对路径，默认只上报来源数量；
- WebDAV URL；
- 仓库用户名和密码；
- 数据库地址和凭据；
- Restic 密码；
- 通知密钥；
- 完整命令行；
- 原始 stderr。

可以上报：

- 任务 ID 和显示名称；
- 状态、阶段和错误分类；
- 脱敏错误摘要；
- 时间、耗时和统计量；
- 快照短 ID；
- 客户端版本；
- 操作系统和架构，可由用户关闭。

## 13. 失败与状态语义

### 13.1 成功

满足：

- Restic 创建快照成功；
- 数据库临时文件成功清理或已记录可清理状态；
- 核心结果写入 SQLite。

### 13.2 警告

以下情况为 warning，而不是把备份快照判为失败：

- forget 或 prune 失败；
- 元数据验证失败，但快照已创建；
- 本地通知失败；
- 监控上报失败；
- 某个非必需统计字段无法获取；
- 临时文件延迟清理。

### 13.3 失败

- 仓库不可访问；
- 源路径不可读且配置为严格模式；
- 数据库 dump 失败；
- Restic 未成功创建快照；
- 任务超时；
- 仓库密码错误；
- WebDAV 空间不足或权限错误。

每个失败必须有稳定的 `error_code`，例如：

```text
REPOSITORY_AUTH_FAILED
WEBDAV_RATE_LIMITED
WEBDAV_INSUFFICIENT_STORAGE
SOURCE_NOT_READABLE
DATABASE_DUMP_FAILED
RESTIC_BACKUP_FAILED
TASK_TIMEOUT
LOCK_BUSY
```

## 14. 日志与可观测性

### 14.1 本地日志

同时提供：

- 终端友好文本；
- 每次运行一个 JSON Lines 文件；
- journald 输出；
- `sbackup logs` 查询。

日志等级：debug、info、warning、error。

默认日志保留 30 天或 500 MiB，达到任一条件后轮转。

### 14.2 进度展示

交互终端中显示：

```text
[备份中] 已扫描 132,440 个文件
已处理 82.4 GiB
已上传 1.8 GiB
耗时 03:42
```

非 TTY 环境改为低频单行日志，避免 systemd journal 被大量刷新。

### 14.3 Prometheus

客户端不启动常驻 HTTP 服务。可以通过 textfile collector 写指标：

```text
sbackup_job_last_success_timestamp
sbackup_job_last_run_status
sbackup_job_duration_seconds
sbackup_job_bytes_added
```

监控面板可选暴露 `/metrics`，用于更大型的监控系统接入。

## 15. 安全设计

### 15.1 威胁模型

主要防范：

- 配置或路径导致命令注入；
- 凭据进入日志、通知或上报；
- 恢复误覆盖生产数据；
- 恶意或伪造的状态上报；
- 监控面板被利用为远程控制入口；
- WebDAV 中间人、重定向和凭据泄露；
- 公开监控面板的暴力登录和 CSRF；
- 过大的日志和请求导致磁盘或内存耗尽。

### 15.2 关键控制

- 不使用 shell 拼接外部命令；
- 主配置和 secrets 权限检查；
- Restic 密码使用文件或受限环境变量；
- TLS 默认开启并校验；
- 恢复默认写新目录；
- 配置原子写入；
- 上报 HMAC、时钟窗口、nonce 和幂等键；
- 面板请求体限制和速率限制；
- 页面安全头和 CSP；
- 日志统一脱敏中间层；
- 监控 API 不实现任何命令下发接口；
- 备份仓库应使用与主机不同的凭据和最小权限账号。

### 15.3 仓库安全建议

- Restic 密码至少随机 32 字节；
- WebDAV 使用专用账号和独立目录；
- 若服务端支持，开启版本控制、回收站或对象锁；
- 定期离线保存 Restic 密码；
- 至少保留一个与生产主机凭据隔离的副本；
- 删除/prune 操作应允许独立关闭或使用受限凭据。

## 16. 项目代码结构

```text
sbackup/
├── cmd/
│   ├── sbackup/
│   │   └── main.go
│   └── sbackup-monitor/
│       └── main.go
├── internal/
│   ├── app/
│   ├── cli/
│   ├── tui/
│   ├── config/
│   ├── secrets/
│   ├── executor/
│   ├── backup/
│   ├── repository/
│   │   ├── local/
│   │   ├── sftp/
│   │   ├── s3/
│   │   └── webdav/
│   ├── database/
│   │   ├── postgres/
│   │   ├── mysql/
│   │   └── sqlite/
│   ├── retention/
│   ├── restore/
│   ├── verify/
│   ├── schedule/
│   ├── notification/
│   ├── report/
│   ├── store/
│   ├── logging/
│   └── monitor/
│       ├── api/
│       ├── auth/
│       ├── incidents/
│       ├── dashboard/
│       └── store/
├── pkg/
│   └── reportprotocol/
├── web-monitor/
│   ├── templates/
│   └── static/
├── deploy/
│   ├── systemd/
│   ├── docker/
│   └── examples/
├── docs/
├── migrations/
├── go.mod
└── Makefile
```

## 17. 性能与资源目标

客户端自身，不含 Restic/rclone 子进程：

- 空闲时不常驻；
- 普通 CLI 内存目标低于 50 MiB；
- SQLite 数据库可长期保存至少十万条运行事件；
- 日志和 outbox 均有上限；
- 大目录不在客户端内存中构建完整文件清单。

监控面板初始目标：

- 单实例支持 1,000 节点、10,000 个任务；
- 每分钟数千次心跳以内无需外部缓存；
- SQLite 模式适合个人和小团队；
- 超出规模后支持 PostgreSQL，但不作为 MVP 依赖。

## 18. 兼容性

第一阶段：

- Linux amd64、arm64；
- systemd 发行版优先；
- Debian/Ubuntu、Rocky/Alma、Fedora、Arch；
- Restic 和 rclone 版本在启动时检查最低要求。

客户端版本升级需要保证：

- 配置 schema 向前迁移；
- SQLite migration 可回滚备份；
- 上报协议至少兼容前一个 major 版本；
- 面板遇到未知字段应忽略，不应拒绝整个事件。

## 19. 产品体验要求

- 首次安装后十分钟内完成一个 WebDAV 备份任务。
- `doctor` 能在一屏内指出最常见的环境问题。
- 所有失败都给出“原因、影响、建议操作”。
- 高风险操作必须显示实际目标仓库、快照和恢复路径。
- 配置修改前后显示 diff，敏感值始终显示为 `<redacted>`。
- 终端菜单和非交互 CLI 共享同一业务层，避免行为不一致。

## 20. 推荐 MVP 边界

第一个可发布版本应包含：

- 终端菜单和完整 CLI；
- YAML 配置及 secrets；
- 本地目录、SFTP、S3 和 WebDAV；
- 路径备份；
- PostgreSQL、MySQL/MariaDB、SQLite dump；
- 自动调度；
- 快照列表和安全恢复；
- 保留策略和标准验证；
- SMTP、Telegram、Webhook、Gotify、ntfy、Bark；
- 可选状态上报；
- 独立监控面板；
- 失败、漏报、离线和恢复告警；
- Docker Compose 监控部署；
- systemd 客户端部署。

明确推迟：

- 任意 Shell Hook；
- 远程管理客户端；
- 浏览器配置备份策略；
- 多租户；
- 自动发现全部 Docker 数据；
- 客户端自更新。
