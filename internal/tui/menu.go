package tui

import (
	"bufio"
	"fmt"
	"os"
	"sbackup/internal/config"
	"strconv"
	"strings"
)

func Run(c *config.Config, path string, save func() error) error {
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nSBackup")
		fmt.Println("1. 查看配置摘要")
		fmt.Println("2. 管理任务")
		fmt.Println("3. 管理存储")
		fmt.Println("4. 添加 WebDAV 存储")
		fmt.Println("5. 添加目录备份任务")
		fmt.Println("6. 管理监控上报")
		fmt.Println("7. 校验配置")
		fmt.Println("0. 退出")
		fmt.Print("请选择: ")
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return nil
		}
		changed := true
		switch strings.TrimSpace(line) {
		case "1":
			show(c)
			changed = false
		case "2":
			changed = jobs(c, in)
		case "3":
			storages(c, in)
			changed = false
		case "4":
			changed = addWebDAV(c, in)
		case "5":
			changed = addJob(c, in)
		case "6":
			changed = monitor(c, in)
		case "7":
			if err := c.Validate(); err != nil {
				fmt.Println("配置错误:", err)
			} else {
				fmt.Println("配置有效")
			}
			changed = false
		case "0", "":
			return nil
		default:
			fmt.Println("无效选择")
			changed = false
		}
		if changed {
			if err := save(); err != nil {
				fmt.Println("保存失败:", err)
			} else {
				fmt.Println("配置已保存到", path)
			}
		}
	}
}
func show(c *config.Config) {
	fmt.Printf("主机: %s\n任务: %d\n存储: %d\n监控: %v\n", c.Global.DisplayName, len(c.Jobs), len(c.Storages), c.Monitoring.Enabled)
	for _, j := range c.Jobs {
		fmt.Printf("- %s [%s] %s\n", j.ID, map[bool]string{true: "启用", false: "禁用"}[j.Enabled], j.Name)
	}
}
func jobs(c *config.Config, in *bufio.Reader) bool {
	for i, j := range c.Jobs {
		fmt.Printf("%d. %s [%v]\n", i+1, j.Name, j.Enabled)
	}
	fmt.Print("输入编号切换启用状态，直接回车返回: ")
	x, _ := in.ReadString('\n')
	n, _ := strconv.Atoi(strings.TrimSpace(x))
	if n > 0 && n <= len(c.Jobs) {
		c.Jobs[n-1].Enabled = !c.Jobs[n-1].Enabled
		return true
	}
	return false
}
func storages(c *config.Config, in *bufio.Reader) {
	for _, s := range c.Storages {
		fmt.Printf("- %s: %s (%s)\n", s.ID, s.Name, s.Type)
	}
	fmt.Print("回车返回: ")
	_, _ = in.ReadString('\n')
}
func monitor(c *config.Config, in *bufio.Reader) bool {
	fmt.Printf("当前监控: %v\n", c.Monitoring.Enabled)
	fmt.Print("输入 on/off 或回车返回: ")
	x, _ := in.ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(x)) {
	case "on":
		c.Monitoring.Enabled = true
		return true
	case "off":
		c.Monitoring.Enabled = false
		return true
	}
	return false
}
func ask(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Print(label + ": ")
	}
	x, _ := in.ReadString('\n')
	x = strings.TrimSpace(x)
	if x == "" {
		return def
	}
	return x
}
func addWebDAV(c *config.Config, in *bufio.Reader) bool {
	id := ask(in, "存储 ID", "")
	if id == "" {
		return false
	}
	name := ask(in, "显示名称", id)
	url := ask(in, "WebDAV URL", "")
	remote := ask(in, "rclone remote 名称", id)
	root := ask(in, "远程根目录", "/sbackup/"+c.Global.Hostname)
	repoPass := ask(in, "Restic 密码文件", "/etc/sbackup/secrets/repositories/"+id+".pass")
	conf := ask(in, "rclone 配置文件", "/etc/sbackup/rclone.conf")
	c.Storages = append(c.Storages, config.Storage{ID: id, Name: name, Type: "webdav", PasswordFile: repoPass, WebDAV: &config.WebDAVStorage{RemoteName: remote, URL: url, RcloneConfig: conf, RemoteRoot: root, VerifyTLS: true, Transfers: 2, Checkers: 4, Timeout: "60s", Retries: 5, RetriesSleep: "10s"}})
	return true
}
func addJob(c *config.Config, in *bufio.Reader) bool {
	id := ask(in, "任务 ID", "")
	if id == "" {
		return false
	}
	name := ask(in, "任务名称", id)
	storage := ask(in, "存储 ID", "")
	raw := ask(in, "备份目录，逗号分隔", "/etc,/home")
	calendar := ask(in, "systemd OnCalendar", "*-*-* 02:30:00")
	paths := []string{}
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	c.Jobs = append(c.Jobs, config.Job{ID: id, Name: name, Enabled: true, StorageID: storage, Sources: config.Sources{Paths: paths, StrictPaths: true}, Schedule: config.Schedule{Enabled: true, Type: "calendar", Expression: calendar, Persistent: true, RandomizedDelay: "10m", GracePeriod: "45m", Timeout: "6h"}, Retention: config.Retention{KeepLast: 3, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12, KeepYearly: 3, ForgetAfterBackup: true, PruneSchedule: "weekly"}, Verification: config.Verification{MetadataAfterBackup: true, StandardSchedule: "weekly"}, Restic: config.Restic{Compression: "auto"}, Monitoring: config.JobMonitoring{Report: true, Heartbeat: true}})
	return true
}
