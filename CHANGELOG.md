# Changelog

本项目遵循语义化版本。详细提交记录可在 GitHub Release 中查看。

## 0.4.1 - 2026-08-04

### 修复与加固

- WebDAV rclone 配置增加跨进程文件锁，避免并发新增 remote 时丢失更新。
- rclone 主配置、备份配置及 YAML 主配置、备份配置统一使用原子替换和目录同步。
- MySQL 导出统一接入外部命令执行器，具备超时、进程组终止、日志脱敏和失败半成品清理能力。
- 隐藏 mysqldump `--defaults-extra-file` 凭据文件路径。
- 修复 outbox 重试退避上限不可达的问题，指数退避最终封顶 12 小时。
- 监控状态文件原子替换后同步父目录，增强异常断电下的持久性。

### 工程质量

- 增加 WebDAV 并发更新、MySQL 取消清理、流式输出、配置备份和退避边界回归测试。
- CI 与 Release 加入 Go race detector。
- 修复 ShellCheck 发现的条件分支歧义，并将 ShellCheck 纳入 CI 与 Release 门禁。
- Release 增加归档结构、嵌入版本和 SHA256 校验自测，并在容器构建成功后再发布 Release。
- 明确 `rclone obscure` 仅为混淆，不能替代加密或密钥保险库。
