package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sbackup/internal/backup"
	"sbackup/internal/config"
	"sbackup/internal/report"
	"sbackup/internal/repository"
	runtimeutil "sbackup/internal/runtime"
	"sbackup/internal/schedule"
	"sbackup/internal/store"
	"sbackup/internal/terminal"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RunJobFunc func(context.Context, string, bool) (store.Run, error)

func Run(c *config.Config, path string, save func() error, runJob RunJobFunc) error {
	in := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nSBackup - 轻量备份")
		fmt.Println("1. 快速设置备份")
		fmt.Println("2. 立即运行备份")
		fmt.Println("3. 查看和恢复备份")
		fmt.Println("4. 查看状态")
		fmt.Println("5. 连接监控端")
		fmt.Println("6. 高级管理")
		fmt.Println("0. 退出")
		fmt.Print("请选择: ")
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return nil
		}
		changed := false
		switch strings.TrimSpace(line) {
		case "1":
			quickSetup(c, path, in, save, runJob)
		case "2":
			runBackup(c, in, runJob)
			changed = false
		case "3":
			restoreWizard(c, in)
			changed = false
		case "4":
			show(c)
			changed = false
		case "5":
			changed = monitor(c, in)
		case "6":
			changed = advancedMenu(c, path, in, save, runJob)
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

func quickSetup(c *config.Config, configPath string, in *bufio.Reader, save func() error, runJob RunJobFunc) {
	fmt.Println("\n快速设置会依次完成：存储、加密密码、仓库初始化、备份目录和每天自动备份。")
	fmt.Println("1. 本机或挂载磁盘（最简单）")
	fmt.Println("2. WebDAV 网盘")
	fmt.Print("请选择，直接回车返回: ")
	choice, _ := in.ReadString('\n')
	choice = strings.TrimSpace(choice)
	var storage config.Storage
	var ok bool
	switch choice {
	case "1":
		storage, ok = newLocalStorage(c, in)
	case "2":
		storage, ok = newWebDAVStorage(c, in)
	default:
		return
	}
	if !ok {
		return
	}
	if _, exists := c.Storage(storage.ID); exists {
		fmt.Println("存储 ID 已存在。")
		return
	}
	if err := validateNewStorage(c, storage); err != nil {
		fmt.Println("存储配置无效:", err)
		return
	}
	c.Storages = append(c.Storages, storage)
	if err := save(); err != nil {
		c.Storages = c.Storages[:len(c.Storages)-1]
		fmt.Println("保存存储失败:", err)
		return
	}
	if err := createRepositoryPassword(storage, in); err != nil {
		c.Storages = c.Storages[:len(c.Storages)-1]
		_ = save()
		fmt.Println("创建仓库密码失败:", err)
		return
	}
	if err := repository.Init(context.Background(), c, storage, nil); err != nil {
		fmt.Println("初始化仓库失败:", err)
		fmt.Println("存储配置和密码已保留，可修正网络或路径后从“高级管理”重新初始化。")
		return
	}
	fmt.Println("仓库初始化完成。")
	finishJobSetup(c, configPath, storage.ID, in, save, runJob)
}

func finishJobSetup(c *config.Config, configPath, storageID string, in *bufio.Reader, save func() error, runJob RunJobFunc) {
	job, ok := newJob(storageID, in)
	if !ok {
		fmt.Println("未创建备份任务；仓库配置已保留，可稍后继续设置。")
		return
	}
	if _, exists := c.Job(job.ID); exists {
		fmt.Println("任务 ID 已存在。")
		return
	}
	if err := validateNewJob(c, job); err != nil {
		fmt.Println("任务配置无效:", err)
		return
	}
	c.Jobs = append(c.Jobs, job)
	if err := save(); err != nil {
		c.Jobs = c.Jobs[:len(c.Jobs)-1]
		fmt.Println("保存任务失败:", err)
		return
	}
	if job.Schedule.Enabled {
		if err := installJobSchedule(job, configPath); err != nil {
			fmt.Println("任务已保存，但自动定时器安装失败:", err)
		} else {
			fmt.Println("每天自动备份已启用。")
		}
	}
	fmt.Print("现在立即执行第一次备份吗？[Y/n]: ")
	answer, _ := in.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(answer)) != "n" {
		runOne(c, job.ID, runJob)
	}
	fmt.Println("快速设置完成。以后直接运行 sudo sbackup 即可备份或恢复。")
}

func validateNewStorage(c *config.Config, storage config.Storage) error {
	probe := *c
	probe.Storages = append(append([]config.Storage{}, c.Storages...), storage)
	return probe.Validate()
}

func validateNewJob(c *config.Config, job config.Job) error {
	probe := *c
	probe.Jobs = append(append([]config.Job{}, c.Jobs...), job)
	return probe.Validate()
}

func newLocalStorage(c *config.Config, in *bufio.Reader) (config.Storage, bool) {
	id := ask(in, "存储名称（英文小写）", "local")
	name := ask(in, "显示名称", "本机备份")
	repo := ask(in, "备份仓库目录", "/var/backups/sbackup/"+c.Global.Hostname)
	passwordFile := "/etc/sbackup/secrets/repositories/" + id + ".pass"
	return config.Storage{ID: id, Name: name, Type: "local", RepositoryPath: repo, PasswordFile: passwordFile}, id != ""
}

func newWebDAVStorage(c *config.Config, in *bufio.Reader) (config.Storage, bool) {
	if err := runtimeutil.Ensure(c.Tools.RclonePath); err != nil {
		fmt.Println("无法准备 WebDAV 组件:", err)
		return config.Storage{}, false
	}
	id := ask(in, "存储名称（英文小写）", "webdav")
	name := ask(in, "显示名称", "WebDAV 备份")
	endpoint := ask(in, "WebDAV 地址", "")
	if endpoint == "" {
		return config.Storage{}, false
	}
	username := ask(in, "WebDAV 用户名", "")
	password, err := terminal.ReadPassword("WebDAV 密码: ", in)
	if err != nil || password == "" {
		fmt.Println("读取 WebDAV 密码失败。")
		return config.Storage{}, false
	}
	vendor := ask(in, "服务类型（other/nextcloud/owncloud/sharepoint）", "other")
	remote := "sbackup-" + id
	conf := "/etc/sbackup/rclone.conf"
	root := ask(in, "网盘中的备份目录", "/sbackup/"+c.Global.Hostname)
	passwordFile := "/etc/sbackup/secrets/repositories/" + id + ".pass"
	storage := config.Storage{ID: id, Name: name, Type: "webdav", PasswordFile: passwordFile, WebDAV: &config.WebDAVStorage{RemoteName: remote, URL: endpoint, Vendor: vendor, Username: username, RcloneConfig: conf, RemoteRoot: root, VerifyTLS: true, Transfers: 2, Checkers: 4, Timeout: "60s", Retries: 5, RetriesSleep: "10s"}}
	if err := validateNewStorage(c, storage); err != nil {
		fmt.Println("WebDAV 配置无效:", err)
		return config.Storage{}, false
	}
	if err := repository.ConfigureWebDAV(c.Tools.RclonePath, conf, remote, endpoint, vendor, username, password); err != nil {
		fmt.Println("创建 WebDAV 连接失败:", err)
		return config.Storage{}, false
	}
	return storage, true
}

func createRepositoryPassword(storage config.Storage, in *bufio.Reader) error {
	fmt.Println("\nRestic 会用密码加密所有备份。")
	fmt.Println("1. 自动生成随机密码（推荐）")
	fmt.Println("2. 自定义密码")
	fmt.Print("请选择 [1]: ")
	choice, _ := in.ReadString('\n')
	var password string
	if strings.TrimSpace(choice) == "2" {
		first, err := terminal.ReadPassword("输入密码（至少 16 个字符）: ", in)
		if err != nil {
			return err
		}
		second, err := terminal.ReadPassword("再次输入密码: ", in)
		if err != nil || first != second {
			return fmt.Errorf("两次密码不一致")
		}
		password = first
	}
	created, generated, err := repository.CreatePasswordFile(storage.PasswordFile, password)
	if err != nil {
		return err
	}
	if generated {
		fmt.Println("\n随机仓库密码（仅显示一次，请立即离线保存）:")
		fmt.Println(created)
	}
	fmt.Println("密码文件:", storage.PasswordFile)
	return nil
}

func newJob(storageID string, in *bufio.Reader) (config.Job, bool) {
	id := ask(in, "任务名称（英文小写）", "daily")
	name := ask(in, "显示名称", "每日备份")
	raw := ask(in, "需要备份的目录，多个用逗号分隔", "/etc,/home")
	paths := splitValues(raw)
	if id == "" || len(paths) == 0 {
		return config.Job{}, false
	}
	timeOfDay := ask(in, "每天备份时间（24 小时制）", "02:30")
	if !validClock(timeOfDay) {
		fmt.Println("备份时间格式无效，请使用 HH:MM，例如 02:30。")
		return config.Job{}, false
	}
	return config.Job{ID: id, Name: name, Enabled: true, StorageID: storageID, Sources: config.Sources{Paths: paths, StrictPaths: true}, Schedule: config.Schedule{Enabled: true, Type: "calendar", Expression: "*-*-* " + timeOfDay + ":00", Persistent: true, RandomizedDelay: "10m", GracePeriod: "45m", Timeout: "6h"}, Retention: config.Retention{KeepLast: 3, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12, KeepYearly: 3, ForgetAfterBackup: true, PruneSchedule: "weekly"}, Verification: config.Verification{MetadataAfterBackup: true, StandardSchedule: "weekly"}, Restic: config.Restic{Compression: "auto"}, Monitoring: config.JobMonitoring{Report: true, Heartbeat: true}}, true
}

func validClock(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour, errHour := strconv.Atoi(value[:2])
	minute, errMinute := strconv.Atoi(value[3:])
	return errHour == nil && errMinute == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60
}

func installJobSchedule(job config.Job, configPath string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("启用系统定时器需要 sudo")
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := schedule.Install(job, bin, configPath, "/etc/systemd/system"); err != nil {
		return err
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("系统没有 systemd，请自行设置定时任务")
	}
	unit := "sbackup-job-" + job.ID + ".timer"
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	if output, err := exec.Command("systemctl", "enable", "--now", unit).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runBackup(c *config.Config, in *bufio.Reader, runJob RunJobFunc) {
	job, ok := selectJob(c, in)
	if ok {
		runOne(c, job.ID, runJob)
	}
}

func runOne(c *config.Config, jobID string, runJob RunJobFunc) {
	if runJob != nil {
		run, err := runJob(context.Background(), jobID, false)
		if err != nil {
			fmt.Println("备份失败:", err)
			return
		}
		fmt.Println("备份完成，快照:", run.SnapshotID)
		return
	}
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		fmt.Println("打开状态库失败:", err)
		return
	}
	defer st.Close()
	run, err := (&backup.Service{Config: c, Store: st}).Run(context.Background(), jobID, false)
	if err != nil {
		fmt.Println("备份失败:", err)
		return
	}
	fmt.Println("备份完成，快照:", run.SnapshotID)
}

func advancedMenu(c *config.Config, configPath string, in *bufio.Reader, save func() error, runJob RunJobFunc) bool {
	fmt.Println("\n高级管理")
	fmt.Println("1. 管理任务启用状态")
	fmt.Println("2. 查看存储")
	fmt.Println("3. 初始化已有仓库")
	fmt.Println("4. 为已有存储添加备份任务")
	fmt.Println("5. 校验配置")
	fmt.Println("0. 返回")
	fmt.Print("请选择: ")
	choice, _ := in.ReadString('\n')
	switch strings.TrimSpace(choice) {
	case "1":
		return jobs(c, in)
	case "2":
		storages(c, in)
	case "3":
		initializeRepository(c, in)
	case "4":
		storage, ok := selectStorage(c, in)
		if ok {
			finishJobSetup(c, configPath, storage.ID, in, save, runJob)
		}
	case "5":
		if err := c.Validate(); err != nil {
			fmt.Println("配置错误:", err)
		} else {
			fmt.Println("配置有效")
		}
	}
	return false
}

func selectStorage(c *config.Config, in *bufio.Reader) (*config.Storage, bool) {
	if len(c.Storages) == 0 {
		fmt.Println("尚未配置存储。")
		return nil, false
	}
	for i := range c.Storages {
		fmt.Printf("%d. %s (%s)\n", i+1, c.Storages[i].Name, c.Storages[i].ID)
	}
	fmt.Print("选择存储，直接回车返回: ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(c.Storages) {
		return nil, false
	}
	return &c.Storages[n-1], true
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
	if c.Monitoring.Enabled {
		fmt.Println("当前已连接:", c.Monitoring.Endpoint)
		fmt.Println("1. 测试连接")
		fmt.Println("2. 重新配置")
		fmt.Println("3. 关闭监控上报")
		fmt.Println("0. 返回")
		fmt.Print("请选择: ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			if err := testMonitor(c); err != nil {
				fmt.Println("连接失败:", err)
			} else {
				fmt.Println("监控连接正常。")
			}
		case "2":
			return configureMonitor(c, in)
		case "3":
			c.Monitoring.Enabled = false
			fmt.Println("监控上报已关闭，备份功能不受影响。")
			return true
		}
		return false
	}
	return configureMonitor(c, in)
}

func configureMonitor(c *config.Config, in *bufio.Reader) bool {
	fmt.Println("\n连接监控端只会上报备份状态，不允许远程执行命令。")
	endpoint := ask(in, "监控地址", "https://monitor.example.com/api/v1/report")
	if endpoint != "" && !strings.HasSuffix(strings.TrimSuffix(endpoint, "/"), "/api/v1/report") {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/api/v1/report"
	}
	nodeID := ask(in, "节点 ID", c.Global.Hostname)
	secret, err := terminal.ReadPassword("监控端提供的 node secret: ", in)
	if err != nil || len(secret) < 32 {
		fmt.Println("node secret 至少需要 32 个字符。")
		return false
	}
	keyFile := "/etc/sbackup/secrets/monitor.key"
	oldSecret, oldSecretErr := os.ReadFile(keyFile)
	if err := writeSecret(keyFile, secret); err != nil {
		fmt.Println("保存监控密钥失败:", err)
		return false
	}
	old := c.Monitoring
	c.Monitoring = config.Monitoring{Enabled: true, Endpoint: endpoint, NodeID: nodeID, KeyFile: keyFile, KeyVersion: 1, ReportSystemInfo: true, HeartbeatEnabled: true, HeartbeatInterval: "5m", RequestTimeout: "10s", MaxPendingEvents: 10000, EventRetention: "30d"}
	if err := c.Validate(); err != nil {
		c.Monitoring = old
		restoreSecret(keyFile, oldSecret, oldSecretErr)
		fmt.Println("监控配置无效:", err)
		return false
	}
	if err := testMonitor(c); err != nil {
		c.Monitoring = old
		restoreSecret(keyFile, oldSecret, oldSecretErr)
		fmt.Println("监控连接测试失败，未启用:", err)
		return false
	}
	fmt.Println("监控端已连接。")
	return true
}

func testMonitor(c *config.Config) error {
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		return err
	}
	defer st.Close()
	return (&report.Client{Config: c, Store: st, Version: "menu"}).Test(context.Background())
}

func writeSecret(path, secret string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secret-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(secret + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func restoreSecret(path string, old []byte, oldErr error) {
	if oldErr == nil {
		_ = os.WriteFile(path, old, 0600)
	} else if os.IsNotExist(oldErr) {
		_ = os.Remove(path)
	}
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

func splitValues(raw string) []string {
	values := []string{}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}
