package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sbackup/internal/config"
	"sbackup/internal/database"
	"sbackup/internal/executor"
	"sbackup/internal/lock"
	"sbackup/internal/repository"
	runtimeutil "sbackup/internal/runtime"
	"sbackup/internal/store"
)

type Service struct {
	Config        *config.Config
	Store         *store.Store
	OnRunStarted  func(config.Job, store.Run)
	OnRunFinished func(config.Job, store.Run)
}
type Summary struct {
	MessageType         string  `json:"message_type"`
	SnapshotID          string  `json:"snapshot_id"`
	FilesNew            int64   `json:"files_new"`
	FilesChanged        int64   `json:"files_changed"`
	FilesUnmodified     int64   `json:"files_unmodified"`
	DataAdded           int64   `json:"data_added"`
	TotalBytesProcessed int64   `json:"total_bytes_processed"`
	TotalDuration       float64 `json:"total_duration"`
}
type Snapshot struct {
	ID, ShortID, Time, Hostname string
	Tags                        []string
	Paths                       []string
}

func (s *Service) SnapshotFiles(ctx context.Context, jobID, snapshot, pathFilter string) ([]string, error) {
	_, rt, err := s.runtime(jobID)
	if err != nil {
		return nil, err
	}
	args := append(repository.ResticBase(rt), "ls", snapshot, "--json")
	res := executor.Run(ctx, &executor.Logger{Quiet: true}, "snapshot-files", s.Config.Tools.ResticPath, args, rt.Env, func(string) {})
	if res.Err != nil {
		return nil, res.Err
	}
	var files []string
	for _, line := range strings.Split(res.Output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		name := record.Path
		if name == "" {
			name = record.Name
		}
		if pathFilter == "" || strings.Contains(name, pathFilter) {
			files = append(files, name)
		}
	}
	return files, nil
}

func (s *Service) Run(ctx context.Context, jobID string, scheduled bool) (store.Run, error) {
	return s.RunWithMode(ctx, jobID, scheduled, "")
}

// RunWithMode runs a job and optionally overrides its configured backup mode.
// Restic snapshots are always independently restorable. "full" forces a complete
// source scan while retaining Restic's content deduplication.
func (s *Service) RunWithMode(ctx context.Context, jobID string, scheduled bool, mode string) (store.Run, error) {
	j, ok := s.Config.Job(jobID)
	if !ok {
		return store.Run{}, fmt.Errorf("任务不存在: %s", jobID)
	}
	if !j.Enabled {
		return store.Run{}, fmt.Errorf("任务已禁用")
	}
	mode, err := backupMode(*j, mode)
	if err != nil {
		return store.Run{}, err
	}
	lk, err := lock.Acquire("job-" + j.ID)
	if err != nil {
		return store.Run{}, err
	}
	defer lk.Release()
	globalLock, err := lock.AcquireSlot("global", s.Config.Global.MaxParallelJobs)
	if err != nil {
		return store.Run{}, err
	}
	defer globalLock.Release()
	runID := store.NewID()
	logger, err := executor.NewLogger(s.Config.Global.LogDir, runID)
	if err != nil {
		return store.Run{}, err
	}
	defer logger.Close()
	r := store.Run{ID: runID, JobID: j.ID, Status: "running", Phase: "preflight", StartedAt: time.Now().UTC(), LogPath: logger.Path}
	if scheduled {
		r.ScheduledAt = r.StartedAt
	}
	if err := s.Store.SaveRun(r); err != nil {
		return store.Run{}, fmt.Errorf("保存运行状态: %w", err)
	}
	if s.OnRunStarted != nil {
		s.OnRunStarted(*j, r)
	}
	timeout := 6 * time.Hour
	if j.Schedule.Timeout != "" {
		if d, e := time.ParseDuration(j.Schedule.Timeout); e == nil {
			timeout = d
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fail := func(code string, e error) (store.Run, error) {
		r.Status = "failed"
		r.Phase = "completed"
		r.ErrorCode = code
		r.ErrorSummary = executor.Redact(e.Error())
		r.FinishedAt = time.Now().UTC()
		r.DurationMS = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
		if saveErr := s.Store.SaveRun(r); saveErr != nil {
			return r, fmt.Errorf("%w；且无法保存失败状态: %v", e, saveErr)
		}
		logger.Log("error", "completed", r.ErrorSummary)
		if s.OnRunFinished != nil {
			s.OnRunFinished(*j, r)
		}
		return r, e
	}
	storageCfg, ok := s.Config.Storage(j.StorageID)
	if !ok {
		return fail("CONFIG_ERROR", fmt.Errorf("存储不存在"))
	}
	if storageCfg.Type == "webdav" {
		if err := runtimeutil.Ensure(s.Config.Tools.RclonePath); err != nil {
			return fail("MISSING_RUNTIME", err)
		}
	}
	rt, err := repository.Build(s.Config, *storageCfg)
	if err != nil {
		return fail("CONFIG_ERROR", err)
	}
	for _, p := range j.Sources.Paths {
		if _, err := os.Stat(p); err != nil && j.Sources.StrictPaths {
			return fail("SOURCE_NOT_READABLE", fmt.Errorf("来源 %s: %w", p, err))
		}
	}
	if err := os.MkdirAll(s.Config.Global.TempDir, 0700); err != nil {
		return fail("TEMP_DIR_FAILED", err)
	}
	stage, err := os.MkdirTemp(s.Config.Global.TempDir, "run-"+runID+"-")
	if err != nil {
		return fail("TEMP_DIR_FAILED", err)
	}
	defer os.RemoveAll(stage)
	paths := append([]string{}, j.Sources.Paths...)
	if len(j.Sources.Databases) > 0 {
		r.Phase = "preparing_sources"
		if err := s.Store.SaveRun(r); err != nil {
			return fail("STATE_SAVE_FAILED", err)
		}
		dbDir := filepath.Join(stage, "databases")
		for _, id := range j.Sources.Databases {
			d, ok := s.Config.Database(id)
			if !ok || d == nil {
				return fail("CONFIG_ERROR", fmt.Errorf("数据库不存在: %s", id))
			}
			p, e := database.Dump(runCtx, s.Config, *d, dbDir, logger)
			if e != nil {
				return fail("DATABASE_DUMP_FAILED", e)
			}
			paths = append(paths, p)
		}
	}
	r.Phase = "backing_up"
	if err := s.Store.SaveRun(r); err != nil {
		return fail("STATE_SAVE_FAILED", err)
	}
	args := repository.ResticBase(rt)
	args = append(args, "backup", "--json", "--host", s.Config.Global.Hostname, "--tag", "sbackup-job="+j.ID, "--tag", "sbackup-mode="+mode)
	if mode == "full" {
		args = append(args, "--force")
	}
	if j.Restic.Compression != "" {
		args = append(args, "--compression", j.Restic.Compression)
	}
	if j.Restic.ReadConcurrency > 0 {
		args = append(args, "--read-concurrency", strconv.Itoa(j.Restic.ReadConcurrency))
	}
	if j.Restic.PackSizeMB > 0 {
		args = append(args, "--pack-size", strconv.Itoa(j.Restic.PackSizeMB))
	}
	if j.Sources.OneFileSystem {
		args = append(args, "--one-file-system")
	}
	for _, x := range j.Excludes {
		args = append(args, "--exclude", x)
	}
	for _, tag := range j.Restic.ExtraTags {
		args = append(args, "--tag", tag)
	}
	args = append(args, paths...)
	var summary Summary
	res := executor.Run(runCtx, logger, "backup", s.Config.Tools.ResticPath, args, rt.Env, func(line string) {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			if m["message_type"] == "summary" {
				_ = json.Unmarshal([]byte(line), &summary)
			} else if m["message_type"] == "status" {
				if p, ok := m["percent_done"].(float64); ok {
					logger.Log("info", "backup", fmt.Sprintf("进度 %.1f%%", p*100))
				}
			}
		} else {
			logger.Log("info", "backup", line)
		}
	})
	if res.Err != nil {
		if runCtx.Err() != nil {
			return fail("TASK_TIMEOUT", runCtx.Err())
		}
		return fail("RESTIC_BACKUP_FAILED", res.Err)
	}
	if summary.SnapshotID == "" {
		return fail("RESTIC_INVALID_OUTPUT", fmt.Errorf("Restic 未返回快照摘要"))
	}
	r.SnapshotID = summary.SnapshotID
	r.FilesNew = summary.FilesNew
	r.FilesChanged = summary.FilesChanged
	r.FilesUnmodified = summary.FilesUnmodified
	r.BytesAdded = summary.DataAdded
	r.BytesProcessed = summary.TotalBytesProcessed
	warnings := []string{}
	if j.Retention.ForgetAfterBackup {
		r.Phase = "retention"
		if err := s.Store.SaveRun(r); err != nil {
			return fail("STATE_SAVE_FAILED", err)
		}
		if err := s.Forget(runCtx, j.ID, false, logger); err != nil {
			warnings = append(warnings, "保留策略失败: "+err.Error())
		}
	}
	if j.Verification.MetadataAfterBackup {
		r.Phase = "verifying_metadata"
		if err := s.Store.SaveRun(r); err != nil {
			return fail("STATE_SAVE_FAILED", err)
		}
		if err := s.Verify(runCtx, j.ID, "metadata", logger); err != nil {
			warnings = append(warnings, "元数据验证失败: "+err.Error())
		}
	}
	r.FinishedAt = time.Now().UTC()
	r.DurationMS = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
	r.Phase = "completed"
	if len(warnings) > 0 {
		r.Status = "warning"
		r.Warning = executor.Redact(strings.Join(warnings, "; "))
	} else {
		r.Status = "success"
	}
	if err := s.Store.SaveRun(r); err != nil {
		return fail("STATE_SAVE_FAILED", err)
	}
	logger.Log("info", "completed", fmt.Sprintf("任务%s，快照 %s，新增 %d 字节", map[bool]string{true: "完成但有警告", false: "成功"}[r.Status == "warning"], r.SnapshotID, r.BytesAdded))
	if s.OnRunFinished != nil {
		s.OnRunFinished(*j, r)
	}
	return r, nil
}

func backupMode(job config.Job, override string) (string, error) {
	mode := override
	if mode == "" {
		mode = job.Restic.BackupMode
	}
	if mode == "" {
		mode = "incremental"
	}
	if mode != "incremental" && mode != "full" {
		return "", fmt.Errorf("无效备份模式 %q（可选 incremental 或 full）", mode)
	}
	return mode, nil
}

func (s *Service) runtime(jobID string) (*config.Job, repository.Runtime, error) {
	j, ok := s.Config.Job(jobID)
	if !ok {
		return nil, repository.Runtime{}, fmt.Errorf("任务不存在")
	}
	st, ok := s.Config.Storage(j.StorageID)
	if !ok {
		return nil, repository.Runtime{}, fmt.Errorf("存储不存在")
	}
	if st.Type == "webdav" {
		if err := runtimeutil.Ensure(s.Config.Tools.RclonePath); err != nil {
			return nil, repository.Runtime{}, err
		}
	}
	rt, err := repository.Build(s.Config, *st)
	return j, rt, err
}
func (s *Service) Snapshots(ctx context.Context, jobID string) ([]Snapshot, error) {
	_, rt, err := s.runtime(jobID)
	if err != nil {
		return nil, err
	}
	args := append(repository.ResticBase(rt), "snapshots", "--json", "--tag", "sbackup-job="+jobID)
	res := executor.Run(ctx, &executor.Logger{Quiet: true}, "snapshots", s.Config.Tools.ResticPath, args, rt.Env, func(string) {})
	if res.Err != nil {
		return nil, res.Err
	}
	var raw []struct {
		ID       string    `json:"id"`
		ShortID  string    `json:"short_id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
		Tags     []string  `json:"tags"`
		Paths    []string  `json:"paths"`
	}
	if err := json.Unmarshal([]byte(res.Output), &raw); err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(raw))
	for _, x := range raw {
		out = append(out, Snapshot{ID: x.ID, ShortID: x.ShortID, Time: x.Time.Format(time.RFC3339), Hostname: x.Hostname, Tags: x.Tags, Paths: x.Paths})
	}
	return out, nil
}
func (s *Service) Restore(ctx context.Context, jobID, snapshot, target string, includes []string, logger *executor.Logger) error {
	if err := validateRestoreTarget(target); err != nil {
		return err
	}
	_, rt, err := s.runtime(jobID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0750); err != nil {
		return err
	}
	args := append(repository.ResticBase(rt), "restore", snapshot, "--target", target)
	for _, p := range includes {
		args = append(args, "--include", p)
	}
	res := executor.Run(ctx, logger, "restore", s.Config.Tools.ResticPath, args, rt.Env, nil)
	if res.Err != nil {
		return fmt.Errorf("恢复失败: %w", res.Err)
	}
	return nil
}

func validateRestoreTarget(target string) error {
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) == "/" {
		return fmt.Errorf("拒绝恢复到危险目标 %q；目标必须是绝对路径且不能是根目录", target)
	}
	clean := filepath.Clean(target)
	if info, err := os.Lstat(clean); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("拒绝恢复到符号链接目标 %q", target)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(clean)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil && filepath.Clean(resolved) == "/" && parent != "/" {
		return fmt.Errorf("拒绝通过指向根目录的符号链接恢复: %q", target)
	}
	return nil
}
func (s *Service) Verify(ctx context.Context, jobID, level string, logger *executor.Logger) error {
	j, rt, err := s.runtime(jobID)
	if err != nil {
		return err
	}
	args := append(repository.ResticBase(rt), "check")
	switch level {
	case "metadata":
	case "standard":
		subset := j.Verification.FullReadDataSubset
		if subset == "" {
			subset = "5%"
		}
		args = append(args, "--read-data-subset="+subset)
	case "full":
		args = append(args, "--read-data")
	default:
		return fmt.Errorf("未知验证级别 %q", level)
	}
	res := executor.Run(ctx, logger, "verify", s.Config.Tools.ResticPath, args, rt.Env, nil)
	if res.Err != nil {
		return fmt.Errorf("验证失败: %w", res.Err)
	}
	return nil
}
func (s *Service) Forget(ctx context.Context, jobID string, prune bool, logger *executor.Logger) error {
	j, rt, err := s.runtime(jobID)
	if err != nil {
		return err
	}
	args := append(repository.ResticBase(rt), "forget", "--tag", "sbackup-job="+jobID, "--group-by", "host,tags")
	pairs := []struct {
		v int
		n string
	}{{j.Retention.KeepLast, "--keep-last"}, {j.Retention.KeepHourly, "--keep-hourly"}, {j.Retention.KeepDaily, "--keep-daily"}, {j.Retention.KeepWeekly, "--keep-weekly"}, {j.Retention.KeepMonthly, "--keep-monthly"}, {j.Retention.KeepYearly, "--keep-yearly"}}
	for _, p := range pairs {
		if p.v > 0 {
			args = append(args, p.n, strconv.Itoa(p.v))
		}
	}
	if j.Retention.KeepWithin != "" {
		args = append(args, "--keep-within", j.Retention.KeepWithin)
	}
	if prune {
		args = append(args, "--prune")
	}
	res := executor.Run(ctx, logger, "retention", s.Config.Tools.ResticPath, args, rt.Env, nil)
	if res.Err != nil {
		return fmt.Errorf("应用保留策略失败: %w", res.Err)
	}
	return nil
}
