# 监控上报协议

## 1. 基本约定

- 协议版本：`v1`；
- 传输：HTTPS；
- 编码：JSON UTF-8；
- 最大请求体：256 KiB；
- 时间：RFC3339 UTC；
- 事件 ID：UUIDv7；
- 服务端必须支持幂等接收。

基础路径：

```text
POST /api/v1/report
```

## 2. 请求认证

请求头：

```text
Content-Type: application/json
X-SBackup-Node: node_0198example
X-SBackup-Key-Version: 1
X-SBackup-Timestamp: 1785762600
X-SBackup-Nonce: 0198-example-random
X-SBackup-Signature: v1=0123456789abcdef...
```

Canonical string：

```text
POST
/api/v1/report
1785762600
0198-example-random
<lowercase hex sha256 body>
```

签名：

```text
hex(HMAC-SHA256(SHA256(trim(node_secret)), canonical_string))
```

## 3. 通用事件结构

```json
{
  "protocol_version": 1,
  "event_id": "0198d5f0-0000-7000-8000-000000000001",
  "event_type": "run.completed",
  "occurred_at": "2026-08-03T14:30:00Z",
  "node": {
    "id": "node_0198example",
    "name": "server-a",
    "display_name": "上海家庭服务器",
    "client_version": "0.3.0",
    "os": "linux",
    "arch": "amd64"
  },
  "job": {
    "id": "home",
    "name": "主机文件与数据库",
    "enabled": true,
    "timezone": "Asia/Shanghai",
    "next_expected_at": "2026-08-04T02:40:00+08:00",
    "grace_seconds": 2700
  },
  "run": {
    "id": "0198d5ef-0000-7000-8000-000000000001",
    "status": "success",
    "phase": "completed",
    "scheduled_at": "2026-08-03T02:40:00+08:00",
    "started_at": "2026-08-03T02:40:03+08:00",
    "finished_at": "2026-08-03T02:44:12+08:00",
    "duration_ms": 249000,
    "snapshot_id": "2f8a1c3d",
    "files_new": 120,
    "files_changed": 38,
    "bytes_processed": 88476326297,
    "bytes_added": 1932735283,
    "error_code": "",
    "error_summary": ""
  }
}
```

## 4. 事件类型

### 4.1 `node.heartbeat`

用于判断主机是否在线。没有启用心跳时不发送。

```json
{
  "protocol_version": 1,
  "event_id": "...",
  "event_type": "node.heartbeat",
  "occurred_at": "2026-08-03T14:30:00Z",
  "node": {
    "id": "node_0198example",
    "name": "server-a",
    "client_version": "0.3.0"
  },
  "heartbeat": {
    "uptime_seconds": 86400,
    "pending_reports": 0,
    "jobs_enabled": 3,
    "jobs_running": 0
  }
}
```

### 4.2 `job.synced`

任务创建、修改、启用或禁用后发送，只包含调度和展示摘要。

### 4.3 `run.started`

自动或手动备份开始时发送，用于在面板显示“运行中”。

### 4.4 `run.completed`

备份结束时发送。状态：

```text
success
warning
failed
cancelled
```

### 4.5 `verification.completed`

```json
{
  "protocol_version": 1,
  "event_id": "...",
  "event_type": "verification.completed",
  "occurred_at": "2026-08-03T15:00:00Z",
  "node": {"id": "node_0198example", "name": "server-a"},
  "job": {"id": "home", "name": "主机文件与数据库"},
  "verification": {
    "id": "...",
    "level": "standard",
    "status": "success",
    "started_at": "2026-08-03T14:50:00Z",
    "finished_at": "2026-08-03T15:00:00Z",
    "duration_ms": 600000,
    "error_code": "",
    "error_summary": ""
  }
}
```

### 4.6 `repository.warning`

用于空间不足、限流、证书即将到期等不一定发生于最终备份失败的仓库问题。

## 5. 响应

成功：

```json
{
  "ok": true,
  "event_id": "0198d5f0-0000-7000-8000-000000000001",
  "duplicate": false,
  "server_time": "2026-08-03T14:30:01Z"
}
```

幂等重复：

```json
{
  "ok": true,
  "event_id": "0198d5f0-0000-7000-8000-000000000001",
  "duplicate": true,
  "server_time": "2026-08-03T14:30:01Z"
}
```

错误状态：

| HTTP 状态 | 含义 | 是否重试 |
|---:|---|---|
| 400 | JSON 或字段错误 | 否，进入死信并提示用户 |
| 401 | 节点或签名错误 | 否，要求重新配置密钥 |
| 409 | event ID 内容冲突 | 否 |
| 413 | 请求过大 | 否 |
| 429 | 限流 | 是，遵循 Retry-After |
| 500/502/503/504 | 服务临时故障 | 是 |

## 6. 密钥注册和轮换

监控面板创建节点后一次性显示：

```text
node_id
node_secret
endpoint
```

客户端通过终端菜单粘贴或导入一次性配置。当前轻量监控端保存 `SHA256(node_secret)`，并以该 32 字节派生值作为 HMAC 密钥；客户端发送前执行相同派生，明文 secret 不会写入监控状态文件。该方案保护静态状态文件不直接暴露原始 secret，但读取状态文件的攻击者仍可伪造上报，因此状态文件必须保持 `0600`，生产环境还应使用加密磁盘或外部密钥管理。

轮换流程：

1. 面板生成新 key version；
2. 新旧密钥并行有效一段时间；
3. 客户端更新 key；
4. 测试上报；
5. 撤销旧 key。

## 7. 隐私开关

客户端可以分别关闭：

- 操作系统和架构；
- 容量统计；
- 快照短 ID；
- 主机真实 hostname，仅发送 display name；
- 心跳；
- 成功事件，只上报失败事件。

即使全部可选信息开启，也不得发送凭据、文件名和绝对来源路径。
