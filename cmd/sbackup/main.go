package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sbackup/internal/backup"
	"sbackup/internal/config"
	"sbackup/internal/executor"
	"sbackup/internal/notification"
	"sbackup/internal/report"
	"sbackup/internal/repository"
	"sbackup/internal/schedule"
	"sbackup/internal/store"
	"sbackup/internal/terminal"
	"sbackup/internal/tui"
	"sbackup/pkg/reportprotocol"
	"strings"
	"time"
)

var version = "dev"

func main() {
	path := os.Getenv("SBACKUP_CONFIG")
	if path == "" {
		path = config.DefaultPath
	}
	args := os.Args[1:]
	if len(args) >= 2 && (args[0] == "--config" || args[0] == "-config") {
		path, args = args[1], args[2:]
	}
	if len(args) == 0 {
		if !terminal.IsTerminal(os.Stdin.Fd()) {
			if _, err := os.Stat(path); err != nil {
				usage()
				return
			}
			c, err := load(path)
			if err != nil {
				fatal(err, 2)
			}
			fmt.Printf("主机 %s，任务 %d，存储 %d\n", c.Global.DisplayName, len(c.Jobs), len(c.Storages))
			return
		}
		c, err := load(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		runJob := func(ctx context.Context, jobID string, scheduled bool, mode string) (store.Run, error) {
			st, err := store.Open(c.Global.StateDB)
			if err != nil {
				return store.Run{}, err
			}
			defer st.Close()
			return serviceFor(c, st).RunWithMode(ctx, jobID, scheduled, mode)
		}
		if err := tui.Run(c, path, func() error { return config.Save(path, c) }, runJob); err != nil {
			fatal(err, 1)
		}
		return
	}
	cmd := args[0]
	switch cmd {
	case "version", "--version", "-version":
		fmt.Println("sbackup", version)
	case "init":
		force := len(args) > 1 && args[1] == "--force"
		if _, err := os.Stat(path); err == nil && !force {
			fatal(fmt.Errorf("配置已存在: %s（如确需覆盖，请使用 init --force）", path), 2)
		}
		c := config.Default()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			fatal(err, 1)
		}
		if err := config.Save(path, &c); err != nil {
			fatal(err, 1)
		}
		fmt.Println("已创建", path)
	case "config":
		configCmd(path, args[1:])
	case "status":
		statusCmd(path, args[1:])
	case "job":
		jobCmd(path, args[1:])
	case "storage":
		storageCmd(path, args[1:])
	case "snapshot":
		snapshotCmd(path, args[1:])
	case "restore":
		restoreCmd(path, args[1:])
	case "verify":
		verifyCmd(path, args[1:])
	case "prune":
		pruneCmd(path, args[1:])
	case "retention":
		retentionCmd(path, args[1:])
	case "schedule":
		scheduleCmd(path, args[1:])
	case "logs":
		logsCmd(path, args[1:])
	case "notification":
		notificationCmd(path, args[1:])
	case "maintenance":
		maintenanceCmd(path)
	case "doctor":
		doctorCmd(path)
	case "monitor":
		monitorCmd(path, args[1:])
	default:
		usage()
		os.Exit(2)
	}
}
func usage() {
	fmt.Println("用法: sbackup [--config PATH] <version|init|config|status|job|storage|snapshot|restore|verify|retention|prune|schedule|notification|monitor|logs|maintenance|doctor>")
}
func load(path string) (*config.Config, error) { return config.Load(path) }
func svc(path string) (*config.Config, *store.Store, *backup.Service) {
	c, err := load(path)
	if err != nil {
		fatal(err, 2)
	}
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		fatal(err, 1)
	}
	s := serviceFor(c, st)
	return c, st, s
}

func serviceFor(c *config.Config, st *store.Store) *backup.Service {
	s := &backup.Service{Config: c, Store: st}
	notify := &notification.Service{Config: c, Store: st}
	rep := &report.Client{Config: c, Store: st, Version: version}
	s.OnRunStarted = func(j config.Job, r store.Run) {
		if c.Monitoring.Enabled && j.Monitoring.Report {
			if err := rep.SendOrQueue(context.Background(), rep.EventForRun(j, r, "run.started")); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
	s.OnRunFinished = func(j config.Job, r store.Run) {
		body := fmt.Sprintf("主机: %s\n任务: %s\n状态: %s\n快照: %s\n耗时: %s\n错误: %s", c.Global.DisplayName, j.Name, r.Status, r.SnapshotID, time.Duration(r.DurationMS)*time.Millisecond, r.ErrorSummary)
		e := notification.Event{Title: "SBackup " + r.Status, Body: body, Status: r.Status, Node: c.Global.DisplayName, JobID: j.ID, JobName: j.Name, RunID: r.ID, Time: r.FinishedAt}
		ids := j.Notifications.OnSuccess
		if previous, err := st.LastCompletedBefore(j.ID, r.ID); err == nil && (previous.Status == "failed" || previous.Status == "warning") && r.Status == "success" {
			ids = j.Notifications.OnRecovery
			e.Title = "SBackup recovered"
		}
		if r.Status == "warning" {
			ids = j.Notifications.OnWarning
		} else if r.Status == "failed" {
			ids = j.Notifications.OnFailure
		}
		if err := notify.SendIDs(context.Background(), ids, e); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		if c.Monitoring.Enabled && j.Monitoring.Report {
			if err := rep.SendOrQueue(context.Background(), rep.EventForRun(j, r, "run.completed")); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
	return s
}
func configCmd(path string, args []string) {
	if len(args) > 0 && args[0] == "validate" {
		c, err := load(path)
		if err != nil {
			fatal(err, 2)
		}
		if err := c.Validate(); err != nil {
			fatal(err, 2)
		}
		fmt.Println("配置有效")
		return
	}
	if len(args) > 0 && args[0] == "export" {
		c, err := load(path)
		if err != nil {
			fatal(err, 2)
		}
		redactConfig(c)
		b, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			fatal(err, 1)
		}
		fmt.Println(string(b))
		return
	}
	fmt.Println(path)
}
func statusCmd(path string, args []string) {
	c, st, _ := svc(path)
	defer st.Close()
	if len(args) > 0 && args[0] == "--json" {
		rs, err := st.RecentRuns("", 20)
		if err != nil {
			fatal(err, 1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(rs); err != nil {
			fatal(err, 1)
		}
		return
	}
	fmt.Printf("主机 %s，任务 %d，存储 %d\n", c.Global.DisplayName, len(c.Jobs), len(c.Storages))
	for _, j := range c.Jobs {
		r, err := st.LastRun(j.ID)
		if err != nil {
			fmt.Printf("- %s: 尚无记录\n", j.Name)
		} else {
			fmt.Printf("- %s: %s %s\n", j.Name, r.Status, r.FinishedAt.Format(time.RFC3339))
		}
	}
}
func jobCmd(path string, args []string) {
	if len(args) < 1 {
		fmt.Println("job list|show|run|run-all|enable|disable|remove")
		return
	}
	c, st, s := svc(path)
	defer st.Close()
	switch args[0] {
	case "list":
		for _, j := range c.Jobs {
			fmt.Printf("%s\t%v\t%s\n", j.ID, j.Enabled, j.Name)
		}
	case "show":
		if len(args) < 2 {
			fatal(fmt.Errorf("缺少任务 ID"), 2)
		}
		j, ok := c.Job(args[1])
		if !ok {
			fatal(fmt.Errorf("任务不存在"), 2)
		}
		b, _ := json.MarshalIndent(j, "", "  ")
		fmt.Println(string(b))
	case "run":
		if len(args) < 2 {
			fatal(fmt.Errorf("缺少任务 ID"), 2)
		}
		scheduled := false
		mode := ""
		opts := args[2:]
		for i := 0; i < len(opts); i++ {
			arg := opts[i]
			if arg == "--scheduled" {
				scheduled = true
				continue
			}
			if arg == "--mode" {
				if i+1 >= len(opts) {
					fatal(fmt.Errorf("--mode 缺少值（incremental 或 full）"), 2)
				}
				i++
				mode = opts[i]
				continue
			}
			fatal(fmt.Errorf("未知参数 %s", arg), 2)
		}
		r, err := s.RunWithMode(context.Background(), args[1], scheduled, mode)
		fmt.Println(r.Status, r.SnapshotID)
		if err != nil {
			os.Exit(4)
		}
	case "run-all":
		failed := false
		for _, j := range c.Jobs {
			if j.Enabled {
				r, err := s.Run(context.Background(), j.ID, false)
				fmt.Println(j.ID, r.Status)
				if err != nil {
					failed = true
				}
			}
		}
		if failed {
			fatal(fmt.Errorf("一个或多个任务执行失败"), 4)
		}
	case "enable", "disable":
		if len(args) < 2 {
			fatal(fmt.Errorf("缺少任务 ID"), 2)
		}
		j, ok := c.Job(args[1])
		if !ok {
			fatal(fmt.Errorf("任务不存在"), 2)
		}
		j.Enabled = args[0] == "enable"
		if err := config.Save(path, c); err != nil {
			fatal(err, 1)
		}
		bin, _ := os.Executable()
		unit := "sbackup-job-" + j.ID + ".timer"
		if j.Enabled && j.Schedule.Enabled {
			if err := schedule.Install(*j, bin, path, "/etc/systemd/system"); err != nil {
				fatal(fmt.Errorf("任务状态已保存，但定时器生成失败: %w", err), 1)
			}
			if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
				fatal(fmt.Errorf("任务状态已保存，但 systemd reload 失败: %w: %s", err, strings.TrimSpace(string(output))), 1)
			}
			if output, err := exec.Command("systemctl", "enable", "--now", unit).CombinedOutput(); err != nil {
				fatal(fmt.Errorf("任务状态已保存，但定时器启动失败: %w: %s", err, strings.TrimSpace(string(output))), 1)
			}
		} else {
			_, _ = exec.Command("systemctl", "disable", "--now", unit).CombinedOutput()
			for _, suffix := range []string{".timer", ".service"} {
				_ = os.Remove(filepath.Join("/etc/systemd/system", "sbackup-job-"+j.ID+suffix))
			}
			_ = exec.Command("systemctl", "daemon-reload").Run()
		}
		fmt.Println("任务状态和定时器已更新")
	case "remove":
		if len(args) < 2 {
			fatal(fmt.Errorf("缺少任务 ID"), 2)
		}
		if !hasArg(args[2:], "--force") {
			fatal(fmt.Errorf("删除任务不会删除仓库快照；如确认请使用 --force"), 8)
		}
		removeHistory := hasArg(args[2:], "--delete-history")
		for i := range c.Jobs {
			if c.Jobs[i].ID == args[1] {
				jobID := c.Jobs[i].ID
				c.Jobs = append(c.Jobs[:i], c.Jobs[i+1:]...)
				if err := config.Save(path, c); err != nil {
					fatal(err, 1)
				}
				unit := "sbackup-job-" + jobID + ".timer"
				_, _ = exec.Command("systemctl", "disable", "--now", unit).CombinedOutput()
				for _, suffix := range []string{".timer", ".service"} {
					_ = os.Remove(filepath.Join("/etc/systemd/system", "sbackup-job-"+jobID+suffix))
				}
				_ = exec.Command("systemctl", "daemon-reload").Run()
				if removeHistory {
					if err := st.DeleteJobRuns(jobID, true, c.Global.LogDir); err != nil {
						fatal(fmt.Errorf("任务已删除，但清理历史失败: %w", err), 1)
					}
				}
				fmt.Println("任务已删除；Restic 仓库快照未删除")
				return
			}
		}
		fatal(fmt.Errorf("任务不存在"), 2)
	default:
		fatal(fmt.Errorf("未知 job 子命令"), 2)
	}
}
func storageCmd(path string, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("用法: storage list|test|init|password [storage-id]"), 2)
	}
	c, st, _ := svc(path)
	defer st.Close()
	if args[0] == "list" {
		for _, x := range c.Storages {
			fmt.Printf("%s\t%s\t%s\n", x.ID, x.Type, x.Name)
		}
		return
	}
	if len(args) < 2 {
		fatal(fmt.Errorf("缺少存储 ID"), 2)
	}
	x, ok := c.Storage(args[1])
	if !ok {
		fatal(fmt.Errorf("存储不存在"), 2)
	}
	switch args[0] {
	case "test":
		if err := repository.Test(context.Background(), c, *x, nil); err != nil {
			fatal(err, 3)
		}
		fmt.Println("存储连接正常")
	case "init":
		if _, err := os.Stat(x.PasswordFile); os.IsNotExist(err) {
			if !terminal.IsTerminal(os.Stdin.Fd()) {
				fatal(fmt.Errorf("仓库密码尚未创建；请先运行 storage password %s --generate，或交互运行 storage init %s", x.ID, x.ID), 2)
			}
			if err := interactiveRepositoryPassword(*x, bufio.NewReader(os.Stdin)); err != nil {
				fatal(err, 1)
			}
		}
		if err := repository.Init(context.Background(), c, *x, nil); err != nil {
			fatal(err, 3)
		}
		fmt.Println("仓库初始化完成")
	case "password":
		fs := flag.NewFlagSet("storage password", flag.ExitOnError)
		generate := fs.Bool("generate", false, "生成随机密码")
		stdin := fs.Bool("stdin", false, "从标准输入读取自定义密码")
		_ = fs.Parse(args[2:])
		if *generate && *stdin {
			fatal(fmt.Errorf("--generate 与 --stdin 不能同时使用"), 2)
		}
		var password string
		var err error
		if *stdin {
			if terminal.IsTerminal(os.Stdin.Fd()) {
				password, err = terminal.ReadPassword("输入自定义 Restic 密码（至少 16 个字符）: ", nil)
			} else {
				password, err = bufio.NewReader(os.Stdin).ReadString('\n')
				password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
			}
		} else if !*generate {
			if !terminal.IsTerminal(os.Stdin.Fd()) {
				fatal(fmt.Errorf("非交互模式必须指定 --generate 或 --stdin"), 2)
			}
			err = interactiveRepositoryPassword(*x, bufio.NewReader(os.Stdin))
			if err != nil {
				fatal(err, 1)
			}
			return
		}
		if err != nil {
			fatal(err, 1)
		}
		created, generated, err := repository.CreatePasswordFile(x.PasswordFile, password)
		if err != nil {
			fatal(err, 1)
		}
		showRepositoryPasswordResult(x.PasswordFile, created, generated)
	default:
		fatal(fmt.Errorf("未知 storage 子命令"), 2)
	}
}

func interactiveRepositoryPassword(storage config.Storage, reader *bufio.Reader) error {
	fmt.Printf("\n为仓库 %s 配置 Restic 加密密码。\n", storage.Name)
	fmt.Println("1. 自动生成 256 位随机密码（推荐）")
	fmt.Println("2. 输入自定义密码")
	fmt.Print("请选择 [1]: ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)
	var password string
	if choice == "2" {
		first, err := terminal.ReadPassword("输入密码（至少 16 个字符）: ", reader)
		if err != nil {
			return err
		}
		second, err := terminal.ReadPassword("再次输入密码: ", reader)
		if err != nil {
			return err
		}
		if first != second {
			return fmt.Errorf("两次输入的密码不一致")
		}
		password = first
	} else if choice != "" && choice != "1" {
		return fmt.Errorf("无效选择")
	}
	created, generated, err := repository.CreatePasswordFile(storage.PasswordFile, password)
	if err != nil {
		return err
	}
	showRepositoryPasswordResult(storage.PasswordFile, created, generated)
	return nil
}

func showRepositoryPasswordResult(path, password string, generated bool) {
	fmt.Println("\nRestic 仓库密码已安全写入:", path)
	if generated {
		fmt.Println("随机密码（仅本次显示）:")
		fmt.Println(password)
	} else {
		fmt.Println("自定义密码已保存，程序不会再次显示。")
	}
	fmt.Println("重要：请立即把此密码离线保存到密码管理器、加密 U 盘或纸质应急记录。")
	fmt.Println("丢失密码后，任何人（包括 SBackup 作者）都无法恢复仓库数据。")
}
func snapshotCmd(path string, args []string) {
	subcommand := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	job := fs.String("job", "", "")
	snapshot := fs.String("snapshot", "", "")
	pathFilter := fs.String("path", "", "")
	_ = fs.Parse(args)
	if *job == "" {
		fatal(fmt.Errorf("需要 --job"), 2)
	}
	_, st, s := svc(path)
	defer st.Close()
	ss, err := s.Snapshots(context.Background(), *job)
	if err != nil {
		fatal(err, 3)
	}
	if subcommand == "list" {
		for _, x := range ss {
			fmt.Printf("%s\t%s\t%s\n", x.ShortID, x.Time, x.Hostname)
		}
		return
	}
	if (subcommand != "show" && subcommand != "files") || *snapshot == "" {
		fatal(fmt.Errorf("用法: snapshot show|files --job ID --snapshot ID"), 2)
	}
	for _, x := range ss {
		if x.ID != *snapshot && x.ShortID != *snapshot {
			continue
		}
		if subcommand == "show" {
			b, _ := json.MarshalIndent(x, "", "  ")
			fmt.Println(string(b))
			return
		}
		files, err := s.SnapshotFiles(context.Background(), *job, x.ID, *pathFilter)
		if err != nil {
			fatal(err, 3)
		}
		for _, file := range files {
			fmt.Println(file)
		}
		return
	}
	fatal(fmt.Errorf("快照不存在: %s", *snapshot), 3)
}
func restoreCmd(path string, args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	job := fs.String("job", "", "")
	snap := fs.String("snapshot", "latest", "")
	target := fs.String("target", "", "")
	includes := stringListFlag{}
	fs.Var(&includes, "include", "仅恢复指定路径，可重复使用")
	_ = fs.Parse(args)
	if *job == "" || *target == "" {
		fatal(fmt.Errorf("需要 --job 和 --target"), 2)
	}
	_, st, s := svc(path)
	defer st.Close()
	err := s.Restore(context.Background(), *job, *snap, *target, includes, nil)
	if err != nil {
		fatal(err, 5)
	}
	fmt.Println("恢复完成")
}
func verifyCmd(path string, args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	job := fs.String("job", "", "")
	level := fs.String("level", "standard", "")
	_ = fs.Parse(args)
	if *job == "" {
		fatal(fmt.Errorf("需要 --job"), 2)
	}
	_, st, s := svc(path)
	defer st.Close()
	if err := s.Verify(context.Background(), *job, *level, nil); err != nil {
		fatal(err, 6)
	}
	fmt.Println("验证完成")
}
func pruneCmd(path string, args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	job := fs.String("job", "", "")
	_ = fs.Parse(args)
	if *job == "" {
		fatal(fmt.Errorf("需要 --job"), 2)
	}
	_, st, s := svc(path)
	defer st.Close()
	if err := s.Forget(context.Background(), *job, true, nil); err != nil {
		fatal(err, 4)
	}
	fmt.Println("清理完成")
}

func retentionCmd(path string, args []string) {
	if len(args) < 1 || args[0] != "apply" {
		fatal(fmt.Errorf("用法: retention apply --job <job-id>"), 2)
	}
	fs := flag.NewFlagSet("retention apply", flag.ExitOnError)
	job := fs.String("job", "", "")
	_ = fs.Parse(args[1:])
	if *job == "" {
		fatal(fmt.Errorf("需要 --job"), 2)
	}
	_, st, s := svc(path)
	defer st.Close()
	if err := s.Forget(context.Background(), *job, false, nil); err != nil {
		fatal(err, 4)
	}
	fmt.Println("保留策略已应用")
}

func logsCmd(path string, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("用法: logs list [--job ID] [--limit N] 或 logs show <run-id>"), 2)
	}
	_, st, _ := svc(path)
	defer st.Close()
	if args[0] == "list" {
		fs := flag.NewFlagSet("logs list", flag.ExitOnError)
		job := fs.String("job", "", "")
		limit := fs.Int("limit", 100, "")
		_ = fs.Parse(args[1:])
		runs, err := st.RecentRuns(*job, *limit)
		if err != nil {
			fatal(err, 1)
		}
		for _, r := range runs {
			finished := "-"
			if !r.FinishedAt.IsZero() {
				finished = r.FinishedAt.Local().Format(time.RFC3339)
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.JobID, r.Status, r.StartedAt.Local().Format(time.RFC3339), finished, r.LogPath)
		}
		return
	}
	if args[0] != "show" || len(args) < 2 {
		fatal(fmt.Errorf("用法: logs show <run-id>"), 2)
	}
	runs, err := st.RecentRuns("", 10000)
	if err != nil {
		fatal(err, 1)
	}
	for _, r := range runs {
		if r.ID != args[1] {
			continue
		}
		fmt.Printf("运行: %s\n任务: %s\n状态: %s\n开始: %s\n结束: %s\n耗时: %s\n快照: %s\n错误: %s\n日志: %s\n\n", r.ID, r.JobID, r.Status, r.StartedAt.Local().Format(time.RFC3339), formatOptionalTime(r.FinishedAt), time.Duration(r.DurationMS)*time.Millisecond, r.SnapshotID, r.ErrorSummary, r.LogPath)
		b, err := os.ReadFile(r.LogPath)
		if err != nil {
			fatal(err, 1)
		}
		fmt.Print(string(b))
		return
	}
	fatal(fmt.Errorf("运行记录不存在: %s", args[1]), 2)
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format(time.RFC3339)
}

func notificationCmd(path string, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("用法: notification list|test <id>"), 2)
	}
	c, st, _ := svc(path)
	defer st.Close()
	if args[0] == "list" {
		for _, n := range c.Notifications {
			fmt.Printf("%s\t%v\t%s\t%s\n", n.ID, n.Enabled, n.Type, n.Name)
		}
		return
	}
	if args[0] != "test" || len(args) < 2 {
		fatal(fmt.Errorf("用法: notification test <id>"), 2)
	}
	n := &notification.Service{Config: c, Store: st}
	err := n.Send(context.Background(), args[1], notification.Event{Title: "SBackup test", Body: "通知通道测试成功", Status: "test", Node: c.Global.DisplayName, Time: time.Now().UTC()})
	if err != nil {
		fatal(err, 1)
	}
	fmt.Println("通知发送成功")
}

func monitorCmd(path string, args []string) {
	if len(args) < 1 {
		fatal(fmt.Errorf("用法: monitor test|heartbeat|flush|enable|disable"), 2)
	}
	if args[0] == "flush" {
		maintenanceCmd(path)
		return
	}
	c, st, _ := svc(path)
	defer st.Close()
	switch args[0] {
	case "enable", "disable":
		c.Monitoring.Enabled = args[0] == "enable"
		if err := config.Save(path, c); err != nil {
			fatal(err, 1)
		}
	case "test":
		r := &report.Client{Config: c, Store: st, Version: version}
		if err := r.Test(context.Background()); err != nil {
			fatal(err, 1)
		}
		fmt.Println("监控上报成功")
	case "heartbeat":
		r := &report.Client{Config: c, Store: st, Version: version}
		if err := r.SendOrQueue(context.Background(), r.Heartbeat()); err != nil {
			fatal(err, 1)
		}
	default:
		fatal(fmt.Errorf("未知 monitor 子命令"), 2)
	}
}

func scheduleCmd(path string, args []string) {
	if len(args) < 1 || args[0] != "install" {
		fatal(fmt.Errorf("用法: schedule install <job-id> 或 schedule install --job <job-id>"), 2)
	}
	fs := flag.NewFlagSet("schedule install", flag.ExitOnError)
	jobFlag := fs.String("job", "", "")
	_ = fs.Parse(args[1:])
	jobID := *jobFlag
	if jobID == "" && fs.NArg() > 0 {
		jobID = fs.Arg(0)
	}
	if jobID == "" {
		fatal(fmt.Errorf("缺少任务 ID"), 2)
	}
	c, err := load(path)
	if err != nil {
		fatal(err, 2)
	}
	j, ok := c.Job(jobID)
	if !ok {
		fatal(fmt.Errorf("任务不存在"), 2)
	}
	bin, _ := os.Executable()
	if err := schedule.Install(*j, bin, path, "/etc/systemd/system"); err != nil {
		fatal(err, 1)
	}
	fmt.Println("已生成 systemd 单元")
}
func maintenanceCmd(path string) {
	c, st, _ := svc(path)
	defer st.Close()
	items, err := st.DueOutbox(100)
	if err != nil {
		fatal(err, 1)
	}
	rep := &report.Client{Config: c, Store: st, Version: version}
	notify := &notification.Service{Config: c, Store: st}
	for _, o := range items {
		var err error
		if o.Kind == "report" {
			var e reportprotocol.Event
			if decodeErr := json.Unmarshal(o.Payload, &e); decodeErr == nil {
				err = rep.Send(context.Background(), e)
			} else {
				err = fmt.Errorf("无效的监控 outbox: %w", decodeErr)
			}
		} else if o.Kind == "notification" {
			var e notification.Event
			if decodeErr := json.Unmarshal(o.Payload, &e); decodeErr == nil {
				err = notify.Send(context.Background(), o.DestinationID, e)
			} else {
				err = fmt.Errorf("无效的通知 outbox: %w", decodeErr)
			}
		} else {
			err = fmt.Errorf("未知 outbox 类型 %q", o.Kind)
		}
		if err == nil {
			if err := st.CompleteOutbox(o.ID); err != nil {
				fatal(err, 1)
			}
		} else {
			if failErr := st.FailOutbox(o.ID, err.Error(), o.Attempts+1); failErr != nil {
				fatal(failErr, 1)
			}
		}
	}
	if c.Monitoring.Enabled && c.Monitoring.HeartbeatEnabled {
		if err := rep.SendOrQueue(context.Background(), rep.Heartbeat()); err != nil {
			fatal(err, 1)
		}
	}
	retention := time.Duration(0)
	if c.Monitoring.EventRetention != "" {
		retention, err = config.ParseDuration(c.Monitoring.EventRetention)
		if err != nil {
			fatal(err, 2)
		}
	}
	if err := st.Prune(10000, c.Monitoring.MaxPendingEvents, retention); err != nil {
		fatal(err, 1)
	}
	fmt.Printf("处理 %d 个待发送事件\n", len(items))
}
func doctorCmd(path string) {
	c, err := load(path)
	if err != nil {
		fatal(err, 2)
	}
	fmt.Println("配置有效")
	failed := false
	required := []string{c.Tools.ResticPath}
	for _, storage := range c.Storages {
		if storage.Type == "webdav" {
			required = append(required, c.Tools.RclonePath)
		}
	}
	for _, d := range c.Databases {
		switch d.Type {
		case "sqlite":
			required = append(required, c.Tools.SQLitePath)
		case "postgres":
			required = append(required, c.Tools.PGDumpPath)
		case "mysql":
			required = append(required, c.Tools.MySQLDumpPath)
		}
	}
	for _, p := range unique(required) {
		resolved, err := exec.LookPath(p)
		if err != nil {
			if filepath.Base(p) == "rclone" {
				fmt.Printf("缺少 rclone；返回 SBackup 菜单选择 WebDAV 会自动安装\n")
			} else {
				fmt.Printf("缺少运行时命令 %s（请运行 /usr/local/share/sbackup/scripts/install-runtime-tools.sh）\n", p)
			}
			failed = true
			continue
		}
		name := filepath.Base(resolved)
		version, err := externalVersion(resolved)
		if err != nil {
			fmt.Printf("%s 已找到但无法读取版本：%v\n", name, err)
			failed = true
			continue
		}
		fmt.Printf("运行时 %-8s %s (%s)\n", name, version, resolved)
	}
	for _, p := range secretPaths(c) {
		if info, err := os.Stat(p); err != nil {
			fmt.Printf("密钥文件不可用 %s: %v\n", p, err)
			failed = true
		} else if info.Mode().Perm()&0077 != 0 {
			fmt.Printf("密钥文件权限过宽 %s: %o\n", p, info.Mode().Perm())
			failed = true
		}
	}
	if failed {
		os.Exit(3)
	}
}

func externalVersion(path string) (string, error) {
	var args []string
	switch filepath.Base(path) {
	case "restic":
		args = []string{"version"}
	case "rclone":
		args = []string{"version"}
	default:
		args = []string{"--version"}
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, executor.Redact(string(out)))
	}
	line := string(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 160 {
		line = line[:160]
	}
	return line, nil
}
func unique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func secretPaths(c *config.Config) []string {
	out := []string{}
	for _, s := range c.Storages {
		out = append(out, s.PasswordFile)
		if s.S3 != nil && s.S3.CredentialFile != "" {
			out = append(out, s.S3.CredentialFile)
		}
	}
	for _, d := range c.Databases {
		if d.CredentialFile != "" {
			out = append(out, d.CredentialFile)
		}
	}
	for _, n := range c.Notifications {
		if n.SecretFile != "" {
			out = append(out, n.SecretFile)
		}
	}
	if c.Monitoring.Enabled {
		out = append(out, c.Monitoring.KeyFile)
	}
	return unique(out)
}
func fatal(err error, code int) { fmt.Fprintln(os.Stderr, err); os.Exit(code) }

func hasArg(args []string, wanted string) bool {
	for _, arg := range args {
		if arg == wanted {
			return true
		}
	}
	return false
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("include 不能为空")
	}
	*f = append(*f, value)
	return nil
}

func redactConfig(c *config.Config) {
	for i := range c.Storages {
		c.Storages[i].PasswordFile = "<redacted-file>"
		if c.Storages[i].S3 != nil && c.Storages[i].S3.CredentialFile != "" {
			c.Storages[i].S3.CredentialFile = "<redacted-file>"
		}
	}
	for i := range c.Databases {
		if c.Databases[i].CredentialFile != "" {
			c.Databases[i].CredentialFile = "<redacted-file>"
		}
	}
	for i := range c.Notifications {
		if c.Notifications[i].SecretFile != "" {
			c.Notifications[i].SecretFile = "<redacted-file>"
		}
	}
	if c.Monitoring.KeyFile != "" {
		c.Monitoring.KeyFile = "<redacted-file>"
	}
}
