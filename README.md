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
5. 选择备份目录和每天运行时间；
6. 安装系统定时器，并可立即执行第一次备份。

恢复同样通过菜单完成。恢复文件默认写入独立目录，不会直接覆盖业务数据或自动导入数据库。

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
```

详细配置和运维说明：

- [安装与升级](docs/INSTALLATION.md)
- [配置参考](docs/CONFIGURATION.md)
- [监控协议](docs/MONITOR-PROTOCOL.md)

开发检查：`make check`。源码构建：`make build`。

License: Apache-2.0
