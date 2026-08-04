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

type RunJobFunc func(context.Context, string, bool, string) (store.Run, error)

func Run(c *config.Config, path string, save func() error, runJob RunJobFunc) error {
	configureStyle()
	in := bufio.NewReader(os.Stdin)
	for {
		printHeader("SBackup 备份管理中心")
		success, warning, failed, running := recentRunCounts(c)
		fmt.Printf("  主机: %s    任务: %d    存储: %d\n", styled(ansiBold, c.Global.DisplayName), len(c.Jobs), len(c.Storages))
		fmt.Printf("  最近 24 小时: %s %d  %s %d  %s %d", styled(ansiGreen, "成功"), success, styled(ansiYellow, "警告"), warning, styled(ansiRed, "失败"), failed)
		if running > 0 {
			fmt.Printf("  %s %d", styled(ansiBlue, "运行中"), running)
		}
		fmt.Println()
		fmt.Println(styled(ansiDim, "  ──────────────────────────────────────────────────"))
		printMenuItem("1", "立即运行备份", "手动选择任务与备份模式")
		printMenuItem("2", "备份历史与日志", "查看时间、结果、统计和详细日志")
		printMenuItem("3", "浏览与恢复备份", "安全恢复到独立目录")
		printMenuItem("4", "任务与计划管理", "详情、编辑、启停、删除")
		printMenuItem("5", "存储与仓库管理", "连接测试与初始化")
		printMenuItem("6", "快速创建备份", "新建存储、仓库和任务")
		printMenuItem("7", "监控端设置", "连接状态监控服务")
		printMenuItem("8", "系统设置与卸载", "配置校验、诊断和卸载")
		printMenuItem("0", "退出", "")
		printHint("可设置 NO_COLOR=1 关闭颜色；直接回车退出当前菜单。")
		fmt.Print("\n  请选择 › ")
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return nil
		}
		changed := false
		switch strings.TrimSpace(line) {
		case "1":
			runBackup(c, in, runJob)
			changed = false
		case "2":
			historyMenu(c, in)
			changed = false
		case "3":
			restoreWizard(c, in)
			changed = false
		case "4":
			changed = taskMenu(c, path, in, save, runJob)
		case "5":
			storageMenu(c, in)
			changed = false
		case "6":
			quickSetup(c, path, in, save, runJob)
		case "7":
			changed = monitor(c, in)
		case "8":
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
	fmt.Println("\n  请选择备份任务:")
	for i := range c.Jobs {
		fmt.Printf("  %d  %s %s %s\n", i+1, padDisplay(truncate(c.Jobs[i].Name, 18), 20), padDisplay(modeLabel(c.Jobs[i].Restic.BackupMode), 12), scheduleSummary(c.Jobs[i].Schedule))
	}
	fmt.Print("  输入编号，直接回车返回 › ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(c.Jobs) {
		return nil, false
	}
	return &c.Jobs[n-1], true
}

func quickSetup(c *config.Config, configPath string, in *bufio.Reader, save func() error, runJob RunJobFunc) {
	fmt.Println("\n快速设置会依次完成：存储、加密密码、仓库初始化、备份目录、备份模式和自动计划。")
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
			fmt.Println("自动备份计划已启用:", scheduleSummary(job.Schedule))
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
	scheduleCfg, ok := askSchedule(in, config.Schedule{Enabled: true, Type: "calendar", Expression: "*-*-* 02:30:00", Persistent: true, RandomizedDelay: "10m", GracePeriod: "45m", Timeout: "6h"})
	if !ok {
		return config.Job{}, false
	}
	backupMode := askBackupMode(in, "incremental")
	return config.Job{ID: id, Name: name, Enabled: true, StorageID: storageID, Sources: config.Sources{Paths: paths, StrictPaths: true}, Schedule: scheduleCfg, Retention: config.Retention{KeepLast: 3, KeepDaily: 14, KeepWeekly: 8, KeepMonthly: 12, KeepYearly: 3, ForgetAfterBackup: true, PruneSchedule: "weekly"}, Verification: config.Verification{MetadataAfterBackup: true, StandardSchedule: "weekly"}, Restic: config.Restic{Compression: "auto", BackupMode: backupMode}, Monitoring: config.JobMonitoring{Report: true, Heartbeat: true}}, true
}

func askSchedule(in *bufio.Reader, current config.Schedule) (config.Schedule, bool) {
	fmt.Println("\n自动备份计划:")
	fmt.Println("  1  每天一个时间点")
	fmt.Println("  2  每天多个时间点")
	fmt.Println("  3  固定间隔运行")
	fmt.Println("  4  暂不启用自动备份")
	defaultChoice := "1"
	if !current.Enabled {
		defaultChoice = "4"
	} else if current.Type == "interval" {
		defaultChoice = "3"
	} else if strings.Contains(current.Expression, ";") {
		defaultChoice = "2"
	}
	fmt.Printf("  请选择 [%s] › ", defaultChoice)
	choice, _ := in.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = defaultChoice
	}
	s := current
	s.Persistent = true
	if s.RandomizedDelay == "" {
		s.RandomizedDelay = "10m"
	}
	if s.GracePeriod == "" {
		s.GracePeriod = "45m"
	}
	if s.Timeout == "" {
		s.Timeout = "6h"
	}
	s.Enabled = true
	switch choice {
	case "1":
		value := ask(in, "每天备份时间（HH:MM）", firstScheduleClock(current, "02:30"))
		if !validClock(value) {
			fmt.Println("备份时间格式无效，请使用 HH:MM，例如 02:30。")
			return config.Schedule{}, false
		}
		s.Type, s.Expression = "calendar", "*-*-* "+value+":00"
	case "2":
		value := ask(in, "每天备份时间，多个用逗号分隔", scheduleClocksDefault(current, "02:30,12:30,22:30"))
		expressions, err := clocksToExpressions(value)
		if err != nil {
			fmt.Println(err)
			return config.Schedule{}, false
		}
		s.Type, s.Expression = "calendar", strings.Join(expressions, ";")
	case "3":
		value := ask(in, "备份间隔（如 30m、2h、12h）", intervalDefault(current, "6h"))
		if d, err := time.ParseDuration(value); err != nil || d < time.Minute {
			fmt.Println("间隔格式无效，最短为 1m，例如 30m、2h、12h。")
			return config.Schedule{}, false
		}
		s.Type, s.Expression = "interval", value
	case "4":
		s.Enabled = false
		if s.Expression == "" {
			s.Type, s.Expression = "calendar", "*-*-* 02:30:00"
		}
	default:
		fmt.Println("无效选择。")
		return config.Schedule{}, false
	}
	return s, true
}

func askBackupMode(in *bufio.Reader, current string) string {
	if current == "" {
		current = "incremental"
	}
	fmt.Println("\n默认备份模式:")
	fmt.Println("  1  智能增量（推荐，更快；每个快照仍可完整恢复）")
	fmt.Println("  2  强制全量扫描（重新读取全部文件，数据仍会去重）")
	defaultChoice := "1"
	if current == "full" {
		defaultChoice = "2"
	}
	fmt.Printf("  请选择 [%s] › ", defaultChoice)
	choice, _ := in.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = defaultChoice
	}
	if choice == "2" {
		return "full"
	}
	return "incremental"
}

func clocksToExpressions(raw string) ([]string, error) {
	values := splitValues(raw)
	if len(values) == 0 {
		return nil, fmt.Errorf("至少需要一个备份时间。")
	}
	expressions := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !validClock(value) {
			return nil, fmt.Errorf("时间 %q 无效，请使用 HH:MM。", value)
		}
		if !seen[value] {
			expressions = append(expressions, "*-*-* "+value+":00")
			seen[value] = true
		}
	}
	return expressions, nil
}

func firstScheduleClock(s config.Schedule, fallback string) string {
	if s.Type == "calendar" || s.Type == "" {
		parts := strings.Fields(strings.Split(s.Expression, ";")[0])
		if len(parts) > 0 {
			clock := strings.TrimSuffix(parts[len(parts)-1], ":00")
			if validClock(clock) {
				return clock
			}
		}
	}
	return fallback
}

func intervalDefault(s config.Schedule, fallback string) string {
	if s.Type == "interval" && s.Expression != "" {
		return s.Expression
	}
	return fallback
}

func scheduleClocksDefault(s config.Schedule, fallback string) string {
	if s.Type != "calendar" && s.Type != "" {
		return fallback
	}
	values := strings.Split(s.Expression, ";")
	clocks := make([]string, 0, len(values))
	for _, expression := range values {
		parts := strings.Fields(expression)
		if len(parts) == 0 {
			continue
		}
		clock := strings.TrimSuffix(parts[len(parts)-1], ":00")
		if validClock(clock) {
			clocks = append(clocks, clock)
		}
	}
	if len(clocks) == 0 {
		return fallback
	}
	return strings.Join(clocks, ",")
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
		fmt.Println("\n本次备份模式:")
		fmt.Printf("  1  使用任务默认值（%s）\n", modeLabel(job.Restic.BackupMode))
		fmt.Println("  2  智能增量")
		fmt.Println("  3  强制全量扫描")
		fmt.Print("  请选择 [1] › ")
		choice, _ := in.ReadString('\n')
		mode := ""
		switch strings.TrimSpace(choice) {
		case "", "1":
		case "2":
			mode = "incremental"
		case "3":
			mode = "full"
		default:
			fmt.Println("无效选择。")
			return
		}
		runOneMode(c, job.ID, mode, runJob)
	}
}

func runOne(c *config.Config, jobID string, runJob RunJobFunc) {
	runOneMode(c, jobID, "", runJob)
}

func runOneMode(c *config.Config, jobID, mode string, runJob RunJobFunc) {
	if runJob != nil {
		run, err := runJob(context.Background(), jobID, false, mode)
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
	run, err := (&backup.Service{Config: c, Store: st}).RunWithMode(context.Background(), jobID, false, mode)
	if err != nil {
		fmt.Println("备份失败:", err)
		return
	}
	fmt.Println("备份完成，快照:", run.SnapshotID)
}

func taskMenu(c *config.Config, configPath string, in *bufio.Reader, save func() error, runJob RunJobFunc) bool {
	for {
		printHeader("任务与计划管理")
		if len(c.Jobs) == 0 {
			fmt.Println("  尚未创建任务，可先从主菜单使用“快速创建备份”。")
		} else {
			for i, job := range c.Jobs {
				state := "已禁用"
				if job.Enabled {
					state = "已启用"
				}
				fmt.Printf("  %d  %s %s %s %s\n", i+1, padDisplay(truncate(job.Name, 18), 20), padDisplay(state, 8), padDisplay(modeLabel(job.Restic.BackupMode), 12), scheduleSummary(job.Schedule))
			}
		}
		fmt.Println()
		printMenuItem("1", "查看任务完整配置", "来源、排除、计划、保留和存储")
		printMenuItem("2", "编辑任务计划和模式", "修改频率与增量/全量")
		printMenuItem("3", "启用 / 禁用任务", "暂停任务不会删除配置和快照")
		printMenuItem("4", "数据库来源管理", "新增 PostgreSQL / MySQL / SQLite")
		printMenuItem("5", "为已有存储新建任务", "添加新的备份范围")
		printMenuItem("6", "重新安装任务定时器", "修复或刷新 systemd timer")
		printMenuItem("7", "删除某个备份任务", "保留远端仓库快照，可选清理本地历史")
		printMenuItem("0", "返回主菜单", "")
		printHint("删除任务只移除本机配置和定时器，不会删除 Restic 仓库中的备份。")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			job, ok := selectJob(c, in)
			if ok {
				showJobDetail(c, in, *job)
			}
		case "2":
			job, ok := selectJob(c, in)
			if ok {
				editJobMenu(c, configPath, in, save, job)
			}
		case "3":
			if changedJob, changed := jobs(c, in); changed {
				if err := save(); err != nil {
					fmt.Println("保存失败:", err)
				} else {
					if changedJob.Enabled && changedJob.Schedule.Enabled {
						if err := installJobSchedule(*changedJob, configPath); err != nil {
							printWarning("任务已启用，但定时器安装失败: " + err.Error())
						} else {
							printSuccess("任务已启用，定时器已启动。")
						}
					} else if err := disableJobSchedule(changedJob.ID); err != nil {
						printWarning("任务已禁用，但定时器清理失败: " + err.Error())
					} else {
						printSuccess("任务已禁用，定时器已停止。")
					}
				}
			}
		case "4":
			databaseMenu(c, configPath, in, save)
		case "5":
			storage, ok := selectStorage(c, in)
			if ok {
				finishJobSetup(c, configPath, storage.ID, in, save, runJob)
			}
		case "6":
			job, ok := selectJob(c, in)
			if ok && job.Schedule.Enabled {
				if err := installJobSchedule(*job, configPath); err != nil {
					fmt.Println("安装失败:", err)
				} else {
					fmt.Println("定时器已安装并启动。")
				}
			}
		case "7":
			if deleteJobWizard(c, configPath, in, save) {
				printSuccess("任务已从本机配置中删除。")
			}
		case "0", "":
			return false
		default:
			fmt.Println("无效选择。")
		}
	}
}

func editJobMenu(c *config.Config, configPath string, in *bufio.Reader, save func() error, job *config.Job) {
	for {
		printHeader("编辑任务 · " + job.Name)
		fmt.Printf("  当前: %s · %s · %s\n\n", modeLabel(job.Restic.BackupMode), scheduleSummary(job.Schedule), storageDisplayName(c, job.StorageID))
		printMenuItem("1", "显示名称", job.Name)
		printMenuItem("2", "备份目录", strings.Join(job.Sources.Paths, ", "))
		printMenuItem("3", "数据库来源", valueSummary(job.Sources.Databases))
		printMenuItem("4", "排除规则", valueSummary(job.Excludes))
		printMenuItem("5", "目标存储", storageDisplayName(c, job.StorageID))
		printMenuItem("6", "自动计划与备份模式", scheduleSummary(job.Schedule))
		printMenuItem("7", "保留策略", retentionSummary(job.Retention))
		printMenuItem("8", "扫描和校验选项", scanOptionSummary(*job))
		printMenuItem("0", "完成并返回", "")
		printHint("任务 ID 创建后保持不变，以便关联历史记录和仓库快照。")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		if strings.TrimSpace(choice) == "0" || strings.TrimSpace(choice) == "" {
			return
		}
		old := *job
		old.Sources.Paths = append([]string{}, job.Sources.Paths...)
		old.Sources.Databases = append([]string{}, job.Sources.Databases...)
		old.Excludes = append([]string{}, job.Excludes...)
		old.Restic.ExtraTags = append([]string{}, job.Restic.ExtraTags...)
		old.Notifications.OnSuccess = append([]string{}, job.Notifications.OnSuccess...)
		old.Notifications.OnWarning = append([]string{}, job.Notifications.OnWarning...)
		old.Notifications.OnFailure = append([]string{}, job.Notifications.OnFailure...)
		old.Notifications.OnRecovery = append([]string{}, job.Notifications.OnRecovery...)
		updateTimer := false
		switch strings.TrimSpace(choice) {
		case "1":
			job.Name = ask(in, "显示名称", job.Name)
		case "2":
			values := splitValues(ask(in, "备份目录，多个用逗号分隔", strings.Join(job.Sources.Paths, ",")))
			if len(values) == 0 {
				printWarning("至少需要一个目录或数据库来源。")
				continue
			}
			job.Sources.Paths = values
		case "3":
			showAvailableDatabases(c)
			job.Sources.Databases = splitValues(ask(in, "数据库 ID，多个用逗号分隔；输入 - 清空", valueEditDefault(job.Sources.Databases)))
			if len(job.Sources.Databases) == 1 && job.Sources.Databases[0] == "-" {
				job.Sources.Databases = nil
			}
		case "4":
			job.Excludes = splitValues(ask(in, "排除规则，多个用逗号分隔；输入 - 清空", valueEditDefault(job.Excludes)))
			if len(job.Excludes) == 1 && job.Excludes[0] == "-" {
				job.Excludes = nil
			}
		case "5":
			storage, ok := selectStorage(c, in)
			if !ok {
				continue
			}
			job.StorageID = storage.ID
		case "6":
			scheduleCfg, ok := askSchedule(in, job.Schedule)
			if !ok {
				continue
			}
			job.Schedule = scheduleCfg
			job.Restic.BackupMode = askBackupMode(in, job.Restic.BackupMode)
			updateTimer = true
		case "7":
			job.Retention = askRetention(in, job.Retention)
		case "8":
			editScanOptions(in, job)
		default:
			printWarning("无效选择，请输入菜单编号。")
			continue
		}
		if err := c.Validate(); err != nil {
			*job = old
			printFailure("修改无效，已自动回滚: " + err.Error())
			continue
		}
		if err := save(); err != nil {
			*job = old
			printFailure("保存失败，已自动回滚: " + err.Error())
			continue
		}
		printSuccess("任务配置已保存。")
		if updateTimer {
			if job.Schedule.Enabled && job.Enabled {
				if err := installJobSchedule(*job, configPath); err != nil {
					printWarning("配置已保存，但定时器更新失败: " + err.Error())
				} else {
					printSuccess("systemd 定时器已同步更新。")
				}
			} else if err := disableJobSchedule(job.ID); err != nil {
				printWarning("配置已保存，但旧定时器清理失败: " + err.Error())
			}
		}
	}
}

func askRetention(in *bufio.Reader, current config.Retention) config.Retention {
	fmt.Println("\n  输入非负整数；0 表示不保留该周期。")
	current.KeepLast = askNonNegativeInt(in, "保留最近快照", current.KeepLast)
	current.KeepHourly = askNonNegativeInt(in, "每小时保留", current.KeepHourly)
	current.KeepDaily = askNonNegativeInt(in, "每天保留", current.KeepDaily)
	current.KeepWeekly = askNonNegativeInt(in, "每周保留", current.KeepWeekly)
	current.KeepMonthly = askNonNegativeInt(in, "每月保留", current.KeepMonthly)
	current.KeepYearly = askNonNegativeInt(in, "每年保留", current.KeepYearly)
	current.ForgetAfterBackup = askYesNo(in, "每次备份后应用保留策略", current.ForgetAfterBackup)
	return current
}

func askNonNegativeInt(in *bufio.Reader, label string, current int) int {
	value := ask(in, label, strconv.Itoa(current))
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		printWarning("输入无效，保留原值。")
		return current
	}
	return n
}

func askYesNo(in *bufio.Reader, label string, current bool) bool {
	def := "n"
	if current {
		def = "y"
	}
	fmt.Printf("%s [y/n, %s]: ", label, def)
	value, _ := in.ReadString('\n')
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return current
	}
	return value == "y" || value == "yes"
}

func editScanOptions(in *bufio.Reader, job *config.Job) {
	fmt.Println("\n  扫描与校验选项:")
	job.Sources.StrictPaths = askYesNo(in, "来源不可读时立即失败", job.Sources.StrictPaths)
	job.Sources.OneFileSystem = askYesNo(in, "限制在同一文件系统", job.Sources.OneFileSystem)
	job.Verification.MetadataAfterBackup = askYesNo(in, "备份后执行元数据校验", job.Verification.MetadataAfterBackup)
	job.Restic.Compression = ask(in, "压缩模式（auto/off/max）", defaultString(job.Restic.Compression, "auto"))
}

func showAvailableDatabases(c *config.Config) {
	if len(c.Databases) == 0 {
		printHint("当前没有配置数据库来源；可输入 - 保持为空。")
		return
	}
	fmt.Println("  可用数据库来源:")
	for _, database := range c.Databases {
		fmt.Printf("  - %s  %s (%s)\n", database.ID, database.Name, database.Type)
	}
}

func valueSummary(values []string) string {
	if len(values) == 0 {
		return "未配置"
	}
	if len(values) == 1 {
		return truncate(values[0], 28)
	}
	return fmt.Sprintf("%d 项", len(values))
}

func valueEditDefault(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func scanOptionSummary(job config.Job) string {
	parts := []string{}
	if job.Sources.StrictPaths {
		parts = append(parts, "严格来源")
	}
	if job.Sources.OneFileSystem {
		parts = append(parts, "单文件系统")
	}
	if job.Verification.MetadataAfterBackup {
		parts = append(parts, "备份后校验")
	}
	if len(parts) == 0 {
		return "默认"
	}
	return strings.Join(parts, " / ")
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func showJobDetail(c *config.Config, in *bufio.Reader, job config.Job) {
	printHeader("任务完整配置")
	field("名称", job.Name)
	field("任务 ID", job.ID)
	field("状态", map[bool]string{true: styled(ansiGreen, "已启用"), false: styled(ansiYellow, "已禁用")}[job.Enabled])
	field("存储", storageDisplayName(c, job.StorageID)+" ("+job.StorageID+")")
	field("备份模式", modeLabel(job.Restic.BackupMode))
	field("自动计划", scheduleSummary(job.Schedule))
	field("计划持久化", yesNo(job.Schedule.Persistent))
	field("随机延迟", emptyLabel(job.Schedule.RandomizedDelay))
	field("宽限时间", emptyLabel(job.Schedule.GracePeriod))
	field("任务超时", emptyLabel(job.Schedule.Timeout))
	field("仅本文件系统", yesNo(job.Sources.OneFileSystem))
	field("严格检查来源", yesNo(job.Sources.StrictPaths))
	printValues("备份目录", job.Sources.Paths)
	printValues("数据库来源", job.Sources.Databases)
	printValues("排除规则", job.Excludes)
	field("保留策略", retentionSummary(job.Retention))
	field("备份后清理", yesNo(job.Retention.ForgetAfterBackup))
	field("备份后校验", yesNo(job.Verification.MetadataAfterBackup))
	field("标准校验计划", emptyLabel(job.Verification.StandardSchedule))
	field("完整校验计划", emptyLabel(job.Verification.FullSchedule))
	field("完整读取比例", emptyLabel(job.Verification.FullReadDataSubset))
	field("压缩模式", emptyLabel(job.Restic.Compression))
	if job.Restic.ReadConcurrency > 0 {
		field("读取并发", strconv.Itoa(job.Restic.ReadConcurrency))
	}
	if len(job.Restic.ExtraTags) > 0 {
		printValues("额外标签", job.Restic.ExtraTags)
	}
	if job.Restic.PackSizeMB > 0 {
		field("数据包大小", fmt.Sprintf("%d MiB", job.Restic.PackSizeMB))
	}
	printValues("成功通知", job.Notifications.OnSuccess)
	printValues("警告通知", job.Notifications.OnWarning)
	printValues("失败通知", job.Notifications.OnFailure)
	printValues("恢复通知", job.Notifications.OnRecovery)
	field("监控上报", yesNo(job.Monitoring.Report))
	field("监控心跳", yesNo(job.Monitoring.Heartbeat))
	pause(in)
}

func deleteJobWizard(c *config.Config, configPath string, in *bufio.Reader, save func() error) bool {
	job, ok := selectJob(c, in)
	if !ok {
		return false
	}
	printHeader("删除备份任务")
	printWarning("即将删除本机任务配置: " + job.Name + " (" + job.ID + ")")
	fmt.Println("  将删除: 任务配置、对应 systemd 定时器")
	fmt.Println("  不会删除: Restic 仓库、仓库快照、仓库密码、存储配置")
	fmt.Println("\n  本地运行历史和日志:")
	fmt.Println("  1  保留历史和日志（推荐，之后仍可审计）")
	fmt.Println("  2  同时删除该任务的本地历史和日志")
	fmt.Print("  请选择 [1] › ")
	choice, _ := in.ReadString('\n')
	removeHistory := strings.TrimSpace(choice) == "2"
	fmt.Printf("\n  输入任务 ID %s 确认删除 › ", styled(ansiBold+ansiRed, job.ID))
	confirm, _ := in.ReadString('\n')
	if strings.TrimSpace(confirm) != job.ID {
		printWarning("确认内容不匹配，已取消删除。")
		return false
	}
	jobID := job.ID
	oldJobs := append([]config.Job{}, c.Jobs...)
	for i := range c.Jobs {
		if c.Jobs[i].ID == jobID {
			c.Jobs = append(c.Jobs[:i], c.Jobs[i+1:]...)
			break
		}
	}
	if err := save(); err != nil {
		c.Jobs = oldJobs
		printFailure("保存配置失败: " + err.Error())
		return false
	}
	if err := disableJobSchedule(jobID); err != nil {
		printWarning("任务已删除，但定时器清理失败: " + err.Error())
	}
	if removeHistory {
		st, err := store.Open(c.Global.StateDB)
		if err != nil {
			printWarning("任务已删除，但无法打开状态库清理历史: " + err.Error())
		} else {
			if err := st.DeleteJobRuns(jobID, true, c.Global.LogDir); err != nil {
				printWarning("任务已删除，但清理历史失败: " + err.Error())
			}
			_ = st.Close()
		}
	}
	_ = configPath
	return true
}

func storageDisplayName(c *config.Config, id string) string {
	if storage, ok := c.Storage(id); ok {
		return storage.Name
	}
	return "未知存储"
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func printValues(label string, values []string) {
	if len(values) == 0 {
		field(label, "-")
		return
	}
	field(label, values[0])
	for _, value := range values[1:] {
		fmt.Printf("  %s %s\n", strings.Repeat(" ", 14), value)
	}
}

func retentionSummary(r config.Retention) string {
	parts := []string{}
	for _, item := range []struct {
		label string
		value int
	}{{"最近", r.KeepLast}, {"每小时", r.KeepHourly}, {"每天", r.KeepDaily}, {"每周", r.KeepWeekly}, {"每月", r.KeepMonthly}, {"每年", r.KeepYearly}} {
		if item.value > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", item.label, item.value))
		}
	}
	if r.KeepWithin != "" {
		parts = append(parts, "期限 "+r.KeepWithin)
	}
	return strings.Join(parts, " / ")
}

func modeLabel(mode string) string {
	if mode == "full" {
		return "全量扫描"
	}
	return "智能增量"
}

func disableJobSchedule(jobID string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("关闭系统定时器需要 sudo")
	}
	unit := "sbackup-job-" + jobID + ".timer"
	if _, err := exec.LookPath("systemctl"); err == nil {
		output, stopErr := exec.Command("systemctl", "disable", "--now", unit).CombinedOutput()
		if stopErr != nil && !strings.Contains(string(output), "does not exist") && !strings.Contains(string(output), "not loaded") {
			return fmt.Errorf("%w: %s", stopErr, strings.TrimSpace(string(output)))
		}
	}
	for _, suffix := range []string{".timer", ".service"} {
		path := filepath.Join("/etc/systemd/system", "sbackup-job-"+jobID+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		return exec.Command("systemctl", "daemon-reload").Run()
	}
	return nil
}

func scheduleSummary(s config.Schedule) string {
	if !s.Enabled {
		return "手动运行"
	}
	if s.Type == "interval" {
		return "每隔 " + s.Expression
	}
	values := strings.Split(s.Expression, ";")
	clocks := make([]string, 0, len(values))
	for _, expression := range values {
		parts := strings.Fields(expression)
		if len(parts) > 0 {
			clocks = append(clocks, strings.TrimSuffix(parts[len(parts)-1], ":00"))
		}
	}
	if len(clocks) > 0 {
		return "每天 " + strings.Join(clocks, " / ")
	}
	return s.Expression
}

func advancedMenu(c *config.Config, configPath string, in *bufio.Reader, save func() error, runJob RunJobFunc) bool {
	for {
		printHeader("系统设置与卸载")
		printMenuItem("1", "查看运行状态总览", "任务最近结果与计划")
		printMenuItem("2", "校验当前配置", "检查引用、路径和参数")
		printMenuItem("3", "运行系统诊断", "调用 sbackup doctor")
		printMenuItem("4", "查看配置和数据目录", "了解备份与迁移范围")
		printMenuItem("5", "卸载 SBackup", "保留或清理本机数据")
		printMenuItem("0", "返回主菜单", "")
		printHint("卸载不会删除任何本地或远端 Restic 仓库，也不会卸载 Restic/rclone。")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			show(c)
			pause(in)
		case "2":
			if err := c.Validate(); err != nil {
				printFailure("配置错误: " + err.Error())
			} else {
				printSuccess("配置有效。")
			}
			pause(in)
		case "3":
			runDoctor(in, configPath)
		case "4":
			showProjectPaths(c, configPath, in)
		case "5":
			uninstallWizard(in)
		case "0", "":
			return false
		default:
			printWarning("无效选择，请输入菜单编号。")
		}
	}
}

func runDoctor(in *bufio.Reader, configPath string) {
	bin, err := os.Executable()
	if err != nil {
		printFailure("无法定位当前程序: " + err.Error())
		pause(in)
		return
	}
	fmt.Println("\n  正在运行系统诊断……")
	cmd := exec.Command(bin, "--config", configPath, "doctor")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		printFailure("诊断发现问题或执行失败: " + err.Error())
	} else {
		printSuccess("系统诊断通过。")
	}
	pause(in)
}

func showProjectPaths(c *config.Config, configPath string, in *bufio.Reader) {
	printHeader("配置和数据目录")
	field("程序", executablePath())
	field("配置文件", configPath)
	field("状态数据库", c.Global.StateDB)
	field("日志目录", c.Global.LogDir)
	field("临时目录", c.Global.TempDir)
	field("安装资源", "/usr/local/share/sbackup")
	printHint("灾难恢复至少要保存配置文件、/etc/sbackup/secrets、rclone.conf 和仓库密码。")
	pause(in)
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return "未知"
	}
	return path
}

func uninstallWizard(in *bufio.Reader) {
	printHeader("卸载 SBackup")
	if os.Geteuid() != 0 {
		printFailure("卸载需要 root 权限，请使用 sudo sbackup。")
		pause(in)
		return
	}
	script := "/usr/local/share/sbackup/scripts/uninstall.sh"
	if _, err := os.Stat(script); err != nil {
		printFailure("未找到卸载脚本: " + script)
		pause(in)
		return
	}
	fmt.Println("  1  仅卸载程序（保留配置、密钥、历史和日志）")
	fmt.Println("  2  彻底清理本机项目数据（永久删除配置、密钥、历史和日志）")
	fmt.Println("  0  取消")
	printWarning("两种方式都不会删除 Restic 仓库和其中的快照。")
	fmt.Print("\n  请选择 › ")
	choice, _ := in.ReadString('\n')
	choice = strings.TrimSpace(choice)
	if choice != "1" && choice != "2" {
		printWarning("已取消卸载。")
		return
	}
	confirmation := "UNINSTALL"
	args := []string{}
	if choice == "2" {
		confirmation = "PURGE SBACKUP"
		args = append(args, "--purge", "--yes")
	}
	fmt.Printf("  输入 %s 确认 › ", styled(ansiBold+ansiRed, confirmation))
	line, _ := in.ReadString('\n')
	if strings.TrimSpace(line) != confirmation {
		printWarning("确认内容不匹配，已取消卸载。")
		return
	}
	cmd := exec.Command(script, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		printFailure("卸载失败: " + err.Error())
		pause(in)
		return
	}
	printSuccess("卸载完成。当前菜单进程退出后，命令将不可再用。")
	os.Exit(0)
}

func storageMenu(c *config.Config, in *bufio.Reader) {
	for {
		printHeader("存储与仓库管理")
		if len(c.Storages) == 0 {
			fmt.Println("  尚未配置存储，请先使用“快速创建备份”。")
		} else {
			for i, storage := range c.Storages {
				fmt.Printf("  %d  %s %s %s\n", i+1, padDisplay(truncate(storage.Name, 20), 22), padDisplay(storage.Type, 10), storage.ID)
			}
		}
		fmt.Println("\n  1  测试存储连接")
		fmt.Println("  2  初始化已有仓库")
		fmt.Println("  0  返回")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			storage, ok := selectStorage(c, in)
			if !ok {
				continue
			}
			fmt.Println("正在测试存储连接……")
			if err := repository.Test(context.Background(), c, *storage, nil); err != nil {
				fmt.Println("连接失败:", err)
			} else {
				fmt.Println("存储连接正常。")
			}
		case "2":
			initializeRepository(c, in)
		case "0", "":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
}

func selectStorage(c *config.Config, in *bufio.Reader) (*config.Storage, bool) {
	if len(c.Storages) == 0 {
		fmt.Println("尚未配置存储。")
		return nil, false
	}
	for i := range c.Storages {
		fmt.Printf("  %d  %s (%s)\n", i+1, c.Storages[i].Name, c.Storages[i].ID)
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
	printHeader("运行状态总览")
	fmt.Printf("  主机: %s\n  任务: %d\n  存储: %d\n  监控: %s\n", c.Global.DisplayName, len(c.Jobs), len(c.Storages), map[bool]string{true: "已连接", false: "未连接"}[c.Monitoring.Enabled])
	st, storeErr := store.Open(c.Global.StateDB)
	if storeErr == nil {
		defer st.Close()
	}
	for _, j := range c.Jobs {
		last := "尚无运行记录"
		if storeErr == nil {
			if run, runErr := st.LastRun(j.ID); runErr == nil {
				when := run.FinishedAt
				if when.IsZero() {
					when = run.StartedAt
				}
				last = fmt.Sprintf("%s · %s", run.Status, when.Local().Format("01-02 15:04"))
			}
		}
		fmt.Printf("  - %s [%s] %s %s\n", padDisplay(truncate(j.Name, 18), 20), map[bool]string{true: "启用", false: "禁用"}[j.Enabled], padDisplay(modeLabel(j.Restic.BackupMode), 12), scheduleSummary(j.Schedule))
		fmt.Println("    最近:", last)
	}
}
func jobs(c *config.Config, in *bufio.Reader) (*config.Job, bool) {
	for i, j := range c.Jobs {
		fmt.Printf("%d. %s [%v]\n", i+1, j.Name, j.Enabled)
	}
	fmt.Print("输入编号切换启用状态，直接回车返回: ")
	x, _ := in.ReadString('\n')
	n, _ := strconv.Atoi(strings.TrimSpace(x))
	if n > 0 && n <= len(c.Jobs) {
		c.Jobs[n-1].Enabled = !c.Jobs[n-1].Enabled
		return &c.Jobs[n-1], true
	}
	return nil, false
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
