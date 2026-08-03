package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sbackup/internal/backup"
	"sbackup/internal/config"
	"sbackup/internal/repository"
	"sbackup/internal/terminal"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Run(c *config.Config, path string, save func() error) error {
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nSBackup")
		fmt.Println("1. 查看配置摘要")
		fmt.Println("2. 查看和恢复备份")
		fmt.Println("3. 管理任务")
		fmt.Println("4. 管理存储")
		fmt.Println("5. 初始化仓库和密码")
		fmt.Println("6. 添加本地存储")
		fmt.Println("7. 添加 WebDAV 存储")
		fmt.Println("8. 添加目录备份任务")
		fmt.Println("9. 管理监控上报")
		fmt.Println("10. 校验配置")
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
			restoreWizard(c, in)
			changed = false
		case "3":
			changed = jobs(c, in)
		case "4":
			storages(c, in)
			changed = false
		case "5":
			initializeRepository(c, in)
			changed = false
		case "6":
			changed = addLocalStorage(c, in)
		case "7":
			changed = addWebDAV(c, in)
		case "8":
			changed = addJob(c, in)
		case "9":
			changed = monitor(c, in)
		case "10":
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

func selectJob(c *config.Config, in *bufio.Reader) (*config.Job, bool) {
	if len(c.Jobs) == 0 {
		fmt.Println("尚未配置备份任务，请先添加任务。")
		return nil, false
	}
	fmt.Println("\n请选择备份任务:")
	for i := range c.Jobs {
		fmt.Printf("%d. %s (%s)\n", i+1, c.Jobs[i].Name, c.Jobs[i].ID)
	}
	fmt.Print("输入编号，直接回车返回: ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(c.Jobs) {
		return nil, false
	}
	return &c.Jobs[n-1], true
}

func restoreWizard(c *config.Config, in *bufio.Reader) {
	job, ok := selectJob(c, in)
	if !ok {
		return
	}
	service := backup.Service{Config: c}
	fmt.Println("正在读取快照列表……")
	snapshots, err := service.Snapshots(context.Background(), job.ID)
	if err != nil {
		fmt.Println("读取快照失败:", err)
		return
	}
	if len(snapshots) == 0 {
		fmt.Println("这个任务还没有可恢复的快照。")
		return
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Time > snapshots[j].Time })
	fmt.Println("\n可用快照（1 为最新）:")
	for i := range snapshots {
		fmt.Printf("%d. %s  %s  主机:%s\n", i+1, snapshots[i].ShortID, snapshots[i].Time, snapshots[i].Hostname)
	}
	fmt.Print("选择快照 [1]: ")
	line, _ := in.ReadString('\n')
	choice := 1
	if strings.TrimSpace(line) != "" {
		choice, err = strconv.Atoi(strings.TrimSpace(line))
	}
	if err != nil || choice < 1 || choice > len(snapshots) {
		fmt.Println("无效的快照编号。")
		return
	}
	snapshot := snapshots[choice-1]

	fmt.Println("\n恢复范围:")
	fmt.Println("1. 恢复整个快照（推荐）")
	fmt.Println("2. 只恢复指定文件或目录")
	fmt.Print("请选择 [1]: ")
	line, _ = in.ReadString('\n')
	mode := strings.TrimSpace(line)
	includes := []string{}
	if mode == "2" {
		fmt.Println("可先查看快照内容。")
		fmt.Print("输入筛选关键词，直接回车显示前 100 项: ")
		filter, _ := in.ReadString('\n')
		files, listErr := service.SnapshotFiles(context.Background(), job.ID, snapshot.ID, strings.TrimSpace(filter))
		if listErr != nil {
			fmt.Println("读取文件列表失败:", listErr)
			return
		}
		for i, file := range files {
			if i >= 100 {
				fmt.Printf("……另有 %d 项未显示\n", len(files)-100)
				break
			}
			fmt.Println("-", file)
		}
		fmt.Print("输入要恢复的完整路径，多个路径用逗号分隔: ")
		raw, _ := in.ReadString('\n')
		for _, value := range strings.Split(raw, ",") {
			if value = strings.TrimSpace(value); value != "" {
				includes = append(includes, value)
			}
		}
		if len(includes) == 0 {
			fmt.Println("没有选择任何路径，已取消。")
			return
		}
	} else if mode != "" && mode != "1" {
		fmt.Println("无效选择。")
		return
	}

	defaultTarget := filepath.Join("/var/lib/sbackup/restores", job.ID+"-"+time.Now().Format("20060102-150405"))
	target := ask(in, "恢复到独立目录", defaultTarget)
	cleanTarget := filepath.Clean(target)
	if !filepath.IsAbs(cleanTarget) || cleanTarget == "/" {
		fmt.Println("恢复目标必须是非根目录的绝对路径。")
		return
	}
	fmt.Println("\n请确认恢复计划:")
	fmt.Println("任务:", job.Name)
	fmt.Printf("快照: %s (%s)\n", snapshot.ShortID, snapshot.Time)
	fmt.Println("目标:", cleanTarget)
	if len(includes) == 0 {
		fmt.Println("范围: 整个快照")
	} else {
		fmt.Println("范围:")
		for _, include := range includes {
			fmt.Println(" -", include)
		}
	}
	fmt.Println("不会覆盖原位置，也不会自动导入数据库。")
	fmt.Print("输入 YES 开始恢复: ")
	confirm, _ := in.ReadString('\n')
	if strings.TrimSpace(confirm) != "YES" {
		fmt.Println("已取消恢复。")
		return
	}
	fmt.Println("正在恢复，请勿中断……")
	if err := service.Restore(context.Background(), job.ID, snapshot.ID, cleanTarget, includes, nil); err != nil {
		fmt.Println("恢复失败:", err)
		return
	}
	fmt.Println("恢复完成:", cleanTarget)
	fmt.Println("请先检查文件内容和权限，再手工复制到业务目录。数据库备份文件需要由管理员单独导入测试实例。")
}

func initializeRepository(c *config.Config, in *bufio.Reader) {
	if len(c.Storages) == 0 {
		fmt.Println("尚未配置存储，请先添加存储。")
		return
	}
	fmt.Println("\n请选择存储:")
	for i := range c.Storages {
		fmt.Printf("%d. %s (%s)\n", i+1, c.Storages[i].Name, c.Storages[i].ID)
	}
	fmt.Print("输入编号，直接回车返回: ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(c.Storages) {
		return
	}
	storage := c.Storages[n-1]
	if _, err := os.Stat(storage.PasswordFile); os.IsNotExist(err) {
		fmt.Println("1. 自动生成 256 位随机密码（推荐）")
		fmt.Println("2. 输入自定义密码")
		fmt.Print("请选择 [1]: ")
		choice, _ := in.ReadString('\n')
		var password string
		if strings.TrimSpace(choice) == "2" {
			first, readErr := terminal.ReadPassword("输入密码（至少 16 个字符）: ", in)
			if readErr != nil {
				fmt.Println("读取密码失败:", readErr)
				return
			}
			second, readErr := terminal.ReadPassword("再次输入密码: ", in)
			if readErr != nil || first != second {
				fmt.Println("两次密码不一致，已取消。")
				return
			}
			password = first
		}
		created, generated, createErr := repository.CreatePasswordFile(storage.PasswordFile, password)
		if createErr != nil {
			fmt.Println("创建密码失败:", createErr)
			return
		}
		fmt.Println("密码文件:", storage.PasswordFile)
		if generated {
			fmt.Println("随机密码（仅本次显示）:")
			fmt.Println(created)
		}
		fmt.Println("请立即离线保存密码；丢失后无法恢复仓库。")
	}
	fmt.Print("输入 INIT 确认初始化仓库: ")
	confirm, _ := in.ReadString('\n')
	if strings.TrimSpace(confirm) != "INIT" {
		fmt.Println("已取消。")
		return
	}
	if err := repository.Init(context.Background(), c, storage, nil); err != nil {
		fmt.Println("初始化失败:", err)
		return
	}
	fmt.Println("仓库初始化完成。")
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

func addLocalStorage(c *config.Config, in *bufio.Reader) bool {
	id := ask(in, "存储 ID（小写字母、数字、横线）", "")
	if id == "" {
		return false
	}
	name := ask(in, "显示名称", id)
	repo := ask(in, "Restic 仓库目录", "/var/backups/sbackup/"+c.Global.Hostname+"/"+id)
	passwordFile := ask(in, "密码文件", "/etc/sbackup/secrets/repositories/"+id+".pass")
	c.Storages = append(c.Storages, config.Storage{ID: id, Name: name, Type: "local", RepositoryPath: repo, PasswordFile: passwordFile})
	fmt.Println("存储配置已添加。保存后请从主菜单选择“初始化仓库和密码”。")
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
