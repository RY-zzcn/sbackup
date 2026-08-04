# 配置文件规范

## 1. 客户端完整示例

默认路径：`/etc/sbackup/config.yaml`。

```yaml
version: 1

global:
  hostname: server-a
  display_name: 上海家庭服务器
  timezone: Asia/Shanghai
  language: zh-CN
  state_db: /var/lib/sbackup/state.db
  max_parallel_jobs: 1
  temp_dir: /var/lib/sbackup/tmp
  log_retention_days: 30
  log_max_total_mb: 500

tools:
  restic_path: /usr/local/bin/restic
  rclone_path: /usr/local/bin/rclone
  pg_dump_path: /usr/bin/pg_dump
  mysqldump_path: /usr/bin/mysqldump
  sqlite_path: /usr/bin/sqlite3

storages:
  - id: webdav-main
    name: 主 WebDAV 仓库
    type: webdav
    repository_path: /sbackup/server-a
    password_file: /etc/sbackup/secrets/repositories/webdav-main.pass
    webdav:
      remote_name: webdav-main
      url: https://dav.example.com/remote.php/dav/files/backup-user
      vendor: nextcloud
      username: backup-user
      rclone_config: /etc/sbackup/rclone.conf
      remote_root: /server-backups
      verify_tls: true
      ca_file: ""
      allow_http: false
      allow_private_network: false
      transfers: 2
      checkers: 4
      timeout: 60s
      retries: 5
      retries_sleep: 10s

  - id: local-usb
    name: 本地 USB 磁盘
    type: local
    repository_path: /mnt/backup/sbackup/server-a
    password_file: /etc/sbackup/secrets/repositories/local-usb.pass

database_sources:
  - id: app-postgres
    name: 应用 PostgreSQL
    type: postgres
    host: 127.0.0.1
    port: 5432
    database: app
    username: backup
    credential_file: /etc/sbackup/secrets/databases/app-postgres.env
    format: custom
    connect_timeout: 15s
    sslmode: prefer

  - id: site-mysql
    name: 网站 MariaDB
    type: mysql
    host: 127.0.0.1
    port: 3306
    database: site
    username: backup
    credential_file: /etc/sbackup/secrets/databases/site-mysql.env
    single_transaction: true
    connect_timeout: 15s

  - id: service-sqlite
    name: 服务 SQLite
    type: sqlite
    path: /srv/service/data/service.db

jobs:
  - id: home
    name: 主机文件与数据库
    enabled: true
    storage_id: webdav-main

    sources:
      paths:
        - /etc
        - /home
        - /srv
      databases:
        - app-postgres
        - site-mysql
        - service-sqlite
      one_file_system: false
      strict_paths: true

    excludes:
      - /home/*/.cache
      - /srv/**/node_modules
      - /srv/**/.git
      - /var/cache
      - /tmp

    schedule:
      enabled: true
      type: calendar
      # 多个每天时间点用分号连接；固定间隔则用 type: interval 和 expression: 6h
      expression: "*-*-* 02:30:00;*-*-* 14:30:00"
      persistent: true
      randomized_delay: 10m
      grace_period: 45m
      timeout: 6h

    retention:
      keep_last: 3
      keep_daily: 14
      keep_weekly: 8
      keep_monthly: 12
      keep_yearly: 3
      forget_after_backup: true
      prune_schedule: weekly

    verification:
      metadata_after_backup: true
      standard_schedule: weekly
      full_schedule: monthly
      full_read_data_subset: 10%

    restic:
      compression: auto
      read_concurrency: 2
      pack_size_mb: 16
      # incremental=智能增量扫描，full=每次强制重新扫描全部源文件
      backup_mode: incremental
      extra_tags:
        - production

    notifications:
      on_success:
        - mail-main
      on_warning:
        - mail-main
        - gotify-main
      on_failure:
        - mail-main
        - telegram-main
        - gotify-main
      on_recovery:
        - mail-main
        - telegram-main

    monitoring:
      report: true
      heartbeat: true

notifications:
  - id: mail-main
    name: 运维邮箱
    type: smtp
    enabled: true
    secret_file: /etc/sbackup/secrets/notifications/mail-main.json
    settings:
      host: smtp.example.com
      port: 465
      security: tls
      from: backup@example.com
      to:
        - admin@example.com
      timeout: 15s
      cooldown: 30m

  - id: telegram-main
    name: Telegram
    type: telegram
    enabled: true
    secret_file: /etc/sbackup/secrets/notifications/telegram-main.json
    settings:
      timeout: 10s
      cooldown: 30m

  - id: gotify-main
    name: Gotify
    type: gotify
    enabled: true
    secret_file: /etc/sbackup/secrets/notifications/gotify-main.json
    settings:
      url: https://gotify.example.com
      priority_failure: 8
      priority_success: 2
      timeout: 10s

monitoring:
  enabled: true
  endpoint: https://backup-status.example.com/api/v1/report
  node_id: node_0198example
  key_file: /etc/sbackup/secrets/monitor.key
  key_version: 1
  report_system_info: true
  report_capacity_stats: true
  heartbeat_enabled: true
  heartbeat_interval: 5m
  request_timeout: 10s
  max_pending_events: 10000
  event_retention: 30d
```

## 2. WebDAV 密钥示例

`/etc/sbackup/rclone.conf`：

```ini
[webdav-main]
type = webdav
url = https://dav.example.com/remote.php/dav/files/backup-user
vendor = nextcloud
user = backup-user
pass = <由 rclone obscure 生成的值>
```

Restic 密码文件：

```text
/etc/sbackup/secrets/repositories/webdav-main.pass
```

内容是随机生成的高强度密码，只包含一行。此密码必须另行离线保存；丢失后无法恢复仓库数据。

推荐使用：

```bash
sbackup storage password webdav-main --generate
```

随机密码只显示一次。也可在交互式仓库初始化中输入自定义密码；程序拒绝覆盖已经存在的密码文件。

## 3. 通知 secret 示例

SMTP：

```json
{
  "username": "backup@example.com",
  "password": "replace-me"
}
```

Telegram：

```json
{
  "bot_token": "replace-me",
  "chat_id": "123456789"
}
```

Gotify：

```json
{
  "app_token": "replace-me"
}
```

所有 secret 文件必须为 `0600`。`sbackup doctor` 发现权限过宽时默认报错，而不只是警告。

## 4. 配置校验规则

- 所有 ID 必须匹配 `[a-z0-9][a-z0-9_-]{0,63}`。
- job 引用的 storage、database source 和 notification 必须存在。
- 启用自动任务时必须配置合法时区和 schedule。
- `schedule.type` 支持 `calendar`（单个或分号分隔的多个 OnCalendar 表达式）和 `interval`（如 `30m`、`6h`、`24h`，最短 1 分钟）。
- `restic.backup_mode` 支持 `incremental` 和 `full`；两者产生的快照都可完整恢复，全量模式仍保留内容去重。
- WebDAV 默认要求 HTTPS。
- 关闭 TLS 校验必须进行交互式危险确认，非交互模式需要显式危险参数。
- 任务至少包含一个目录或数据库来源。
- 恢复目标不能是 `/`、空字符串或未解析变量。
- 同一任务不能同时存在重复来源。
- 保留策略必须至少保留一个快照，除非使用显式危险配置。
- full check 的读取比例必须在 1% 到 100% 之间。
- 监控 endpoint 不能携带用户名密码形式的 URL 凭据。

## 5. 任务删除语义

交互菜单删除任务时只删除本机 `jobs` 配置和对应 systemd timer，可选择是否同时删除该任务的本地运行历史与 JSONL 日志。Restic 仓库中的快照不会自动删除，仍可通过保留原存储配置或 Restic 命令访问。

## 5. 配置迁移

`version` 是配置 schema 版本。升级流程：

1. 读取旧配置；
2. 生成迁移预览；
3. 保存原文件备份；
4. 原子写入新版本；
5. 重新执行完整校验；
6. 校验失败则自动恢复旧配置。
