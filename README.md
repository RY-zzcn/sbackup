# SBackup

面向 Linux VPS 的轻量备份工具。使用 Restic 加密、去重和恢复；默认只安装客户端与 Restic，WebDAV 的 rclone 在真正配置时才安装。

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/RY-zzcn/sbackup/main/scripts/bootstrap.sh | sudo bash
```

支持 Linux amd64、arm64、armv7。安装器下载 GitHub Release 静态二进制并校验 SHA256，不需要 Go、Git 或 Docker。

## 开始使用

```bash
sudo sbackup
```

第一次选择“快速设置备份”，菜单会依次完成：

1. 选择本机磁盘或 WebDAV；
2. 自动准备所需组件；
3. 创建 Restic 加密密码并提示离线保存；
4. 初始化仓库；
5. 选择备份目录、智能增量或全量扫描模式；
6. 选择每天单个时间、每天多个时间或固定间隔运行；
7. 安装系统定时器，并可立即执行第一次备份。

恢复同样通过菜单完成。恢复文件默认写入独立目录，不会直接覆盖业务数据或自动导入数据库。

交互管理中心还提供：

- 最近 24 小时成功、警告、失败和运行中数量；
- 按任务筛选每次备份的开始/结束时间、结果、耗时、文件统计和仓库新增数据；
- 查看单次运行的 JSONL 详细日志与错误摘要；
- 查看和编辑任务完整配置，包括来源、排除规则、存储、计划、模式、保留和校验选项；
- 安全删除任务，选择是否保留本地历史日志；
- 从菜单卸载程序或彻底清理本机项目数据。所有删除/卸载操作默认不删除 Restic 仓库快照。

菜单在终端中使用颜色区分成功、警告和失败；设置 `NO_COLOR=1` 可关闭颜色。

## 密码安全

随机仓库密码只显示一次，密码文件权限为 `0600`，程序拒绝覆盖已有密码。请将密码保存到离线密码管理器、加密 U 盘或纸质应急记录；密码丢失后无法恢复 Restic 仓库。

## 监控端

客户端菜单选择“连接监控端”，填写 HTTPS 地址、节点 ID 和 node secret，即可测试并启用状态上报。监控端只能接收备份状态，不能远程执行命令。

监控面板镜像：

```bash
docker pull ghcr.io/ry-zzcn/sbackup-monitor:latest
```

部署示例见 [deploy/docker/docker-compose.yml](deploy/docker/docker-compose.yml)。

## 常用命令

```bash
sudo sbackup                 # 交互菜单
sudo sbackup doctor          # 检查实际需要的组件和密钥
sudo sbackup status          # 查看任务状态
sudo sbackup job run <任务>  # 立即备份
sudo sbackup job run <任务> --mode full  # 临时强制全量扫描
sudo sbackup logs list --job <任务>      # 查看某个任务的运行历史
sudo sbackup logs show <运行ID>          # 查看单次结果和详细日志
```

Restic 的每个快照都可以独立完整恢复。“智能增量”会复用上次扫描信息以提升速度；“全量扫描”会重新读取全部源文件，但相同数据仍会去重，不会重复占用仓库空间。

详细配置和运维说明：

- [安装与升级](docs/INSTALLATION.md)
- [配置参考](docs/CONFIGURATION.md)
- [监控协议](docs/MONITOR-PROTOCOL.md)
- [版本变更](CHANGELOG.md)

开发检查：`make check`。源码构建：`make build`。

License: Apache-2.0
