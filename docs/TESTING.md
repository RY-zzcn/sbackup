# 测试与发布检查

## 自动化检查

```bash
GOTOOLCHAIN=local go test -buildvcs=false ./...
GOTOOLCHAIN=local go vet -buildvcs=false ./...
bash -n scripts/*.sh
```

覆盖内容：

- YAML 配置校验、原子保存和 round-trip；
- SQLite 状态和 outbox 持久化；
- Restic JSON 摘要解析；
- Restic 随机/自定义密码文件创建、权限及拒绝覆盖；
- fake Restic 备份、快照、恢复、验证和保留策略；
- 真实 Restic 本地仓库初始化、两次增量备份、恢复旧快照和哈希校验；
- 恢复目标与符号链接安全；
- 外部命令敏感参数脱敏；
- systemd ExecStart 路径引用；
- HMAC 签名、重放拒绝、客户端与监控端协议互通；
- 监控状态持久化。

## 手工发布检查

1. 执行 `scripts/preflight.sh --source-build`；
2. 执行 `scripts/install-runtime-tools.sh --check --restic-only`；
3. 在临时系统路径执行完整安装或升级；
4. 运行 `sbackup version`、`config validate` 和 `doctor`；
5. 创建临时本地仓库并完成备份、恢复、`standard` check；
6. 校验 systemd 单元；
7. 启动监控端，注册节点并发送带签名心跳；
8. 扫描仓库确认没有密钥、状态库、日志和构建产物；
9. 确认 README、配置示例和 CLI 用法一致；
10. 创建版本 tag 前再次运行 CI 同等命令。

Release tag 会额外构建 amd64、arm64、armv7 静态二进制并发布 SHA256SUMS，同时构建 amd64/arm64 的 GHCR 监控镜像。

## 外部服务矩阵

本地 Restic 流程由自动化测试覆盖。WebDAV、SFTP、S3、PostgreSQL、MySQL/MariaDB 和各通知服务依赖实际外部服务及凭据，应在发布候选环境按使用场景分别验证。项目不会在测试中嵌入生产凭据。
