package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"sbackup/internal/config"
	"sbackup/internal/executor"
	"sbackup/internal/store"
)

func historyMenu(c *config.Config, in *bufio.Reader) {
	for {
		printHeader("备份历史与运行日志")
		printMenuItem("1", "查看全部运行记录", "最近 50 次")
		printMenuItem("2", "按任务查看记录", "筛选某个备份任务")
		printMenuItem("3", "仅查看失败和警告", "快速定位异常")
		printMenuItem("0", "返回主菜单", "")
		printHint("每次运行都会写入状态数据库，并生成独立 JSONL 日志文件。")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			browseRuns(c, in, "", false)
		case "2":
			job, ok := selectJob(c, in)
			if ok {
				browseRuns(c, in, job.ID, false)
			}
		case "3":
			browseRuns(c, in, "", true)
		case "0", "":
			return
		default:
			printWarning("无效选择，请输入菜单编号。")
		}
	}
}

func browseRuns(c *config.Config, in *bufio.Reader, jobID string, onlyProblems bool) {
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		printFailure("无法打开运行记录: " + err.Error())
		return
	}
	defer st.Close()
	runs, err := st.RecentRuns(jobID, 50)
	if err != nil {
		printFailure("读取运行记录失败: " + err.Error())
		return
	}
	if onlyProblems {
		filtered := runs[:0]
		for _, run := range runs {
			if run.Status == "failed" || run.Status == "warning" {
				filtered = append(filtered, run)
			}
		}
		runs = filtered
	}
	if len(runs) == 0 {
		printWarning("没有符合条件的运行记录。")
		pause(in)
		return
	}

	printHeader("运行记录")
	fmt.Println("  编号  开始时间          任务             结果      耗时       新增数据")
	fmt.Println(styled(ansiDim, "  ───────────────────────────────────────────────────────────────"))
	for i, run := range runs {
		fmt.Printf("  %-4d  %-16s  %s  %s  %-9s  %s\n", i+1, localTime(run.StartedAt), padDisplay(jobDisplayName(c, run.JobID), 16), statusCell(run.Status, 8), durationLabel(run.DurationMS), byteSize(run.BytesAdded))
	}
	fmt.Print("\n  输入编号查看完整结果和日志，直接回车返回 › ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(runs) {
		return
	}
	showRunDetail(c, in, runs[n-1])
}

func showRunDetail(c *config.Config, in *bufio.Reader, run store.Run) {
	printHeader("单次备份详情")
	field("运行 ID", run.ID)
	field("任务", jobDisplayName(c, run.JobID)+" ("+run.JobID+")")
	field("结果", statusLabel(run.Status))
	field("阶段", run.Phase)
	field("开始时间", timeText(run.StartedAt))
	field("结束时间", timeText(run.FinishedAt))
	field("运行方式", scheduledLabel(run))
	field("耗时", durationLabel(run.DurationMS))
	field("快照", emptyLabel(run.SnapshotID))
	field("文件统计", fmt.Sprintf("新增 %d / 变更 %d / 未变更 %d", run.FilesNew, run.FilesChanged, run.FilesUnmodified))
	field("处理数据", byteSize(run.BytesProcessed))
	field("仓库新增", byteSize(run.BytesAdded))
	if run.Warning != "" {
		field("警告", styled(ansiYellow, run.Warning))
	}
	if run.ErrorCode != "" {
		field("错误代码", styled(ansiRed, run.ErrorCode))
	}
	if run.ErrorSummary != "" {
		field("错误摘要", styled(ansiRed, run.ErrorSummary))
	}
	field("日志文件", emptyLabel(run.LogPath))

	fmt.Println("\n  1  查看本次运行日志")
	fmt.Println("  0  返回")
	fmt.Print("\n  请选择 › ")
	choice, _ := in.ReadString('\n')
	if strings.TrimSpace(choice) == "1" {
		showRunLog(in, run)
	}
}

func showRunLog(in *bufio.Reader, run store.Run) {
	if run.LogPath == "" {
		printWarning("这次运行没有关联日志文件。")
		pause(in)
		return
	}
	f, err := os.Open(run.LogPath)
	if err != nil {
		printFailure("无法读取日志: " + err.Error())
		pause(in)
		return
	}
	defer f.Close()
	entries := []executor.Event{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event executor.Event
		if json.Unmarshal(scanner.Bytes(), &event) == nil {
			entries = append(entries, event)
		}
	}
	if err := scanner.Err(); err != nil {
		printFailure("读取日志失败: " + err.Error())
		pause(in)
		return
	}
	printHeader("运行日志")
	start := 0
	if len(entries) > 200 {
		start = len(entries) - 200
		printHint(fmt.Sprintf("日志共 %d 条，当前显示最后 200 条；完整文件位于 %s", len(entries), run.LogPath))
	}
	for _, event := range entries[start:] {
		level := strings.ToUpper(event.Level)
		switch event.Level {
		case "error":
			level = styled(ansiRed, level)
		case "warning", "warn":
			level = styled(ansiYellow, level)
		default:
			level = styled(ansiGreen, level)
		}
		fmt.Printf("  %s  %-5s  %-20s  %s\n", event.Time.Local().Format("01-02 15:04:05"), level, event.Phase, event.Message)
	}
	if len(entries) == 0 {
		printWarning("日志文件为空或不包含可识别的 JSONL 事件。")
	}
	pause(in)
}

func recentRunCounts(c *config.Config) (success, warning, failed, running int) {
	st, err := store.Open(c.Global.StateDB)
	if err != nil {
		return
	}
	defer st.Close()
	runs, err := st.RecentRuns("", 100)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, run := range runs {
		if run.StartedAt.Before(cutoff) {
			continue
		}
		switch run.Status {
		case "success":
			success++
		case "warning":
			warning++
		case "failed":
			failed++
		case "running":
			running++
		}
	}
	return
}

func jobDisplayName(c *config.Config, id string) string {
	if job, ok := c.Job(id); ok {
		return truncate(job.Name, 15)
	}
	return truncate(id+" (已删除)", 15)
}

func localTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func timeText(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04:05 MST")
}

func durationLabel(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Second).String()
}

func byteSize(value int64) string {
	if value < 0 {
		return "-"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", value, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func field(label, value string) { fmt.Printf("  %s %s\n", padDisplay(label+":", 14), value) }
func emptyLabel(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
func scheduledLabel(run store.Run) string {
	if run.ScheduledAt.IsZero() {
		return "手动运行"
	}
	return "自动计划"
}
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
func pause(in *bufio.Reader) {
	fmt.Print("\n  按回车键返回 › ")
	_, _ = in.ReadString('\n')
}
