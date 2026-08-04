package config

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const DefaultPath = "/etc/sbackup/config.yaml"

type Config struct {
	Version       int            `yaml:"version"`
	Global        Global         `yaml:"global"`
	Tools         Tools          `yaml:"tools"`
	Storages      []Storage      `yaml:"storages"`
	Databases     []Database     `yaml:"database_sources"`
	Jobs          []Job          `yaml:"jobs"`
	Notifications []Notification `yaml:"notifications"`
	Monitoring    Monitoring     `yaml:"monitoring"`
}

type Global struct {
	Hostname         string `yaml:"hostname"`
	DisplayName      string `yaml:"display_name"`
	Timezone         string `yaml:"timezone"`
	Language         string `yaml:"language"`
	StateDB          string `yaml:"state_db"`
	TempDir          string `yaml:"temp_dir"`
	LogDir           string `yaml:"log_dir"`
	MaxParallelJobs  int    `yaml:"max_parallel_jobs"`
	LogRetentionDays int    `yaml:"log_retention_days"`
	LogMaxTotalMB    int    `yaml:"log_max_total_mb"`
}

type Tools struct {
	ResticPath    string `yaml:"restic_path"`
	RclonePath    string `yaml:"rclone_path"`
	PGDumpPath    string `yaml:"pg_dump_path"`
	MySQLDumpPath string `yaml:"mysqldump_path"`
	SQLitePath    string `yaml:"sqlite_path"`
}

type Storage struct {
	ID             string         `yaml:"id"`
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"`
	RepositoryPath string         `yaml:"repository_path"`
	PasswordFile   string         `yaml:"password_file"`
	WebDAV         *WebDAVStorage `yaml:"webdav,omitempty"`
	SFTP           *SFTPStorage   `yaml:"sftp,omitempty"`
	S3             *S3Storage     `yaml:"s3,omitempty"`
}

type WebDAVStorage struct {
	RemoteName          string `yaml:"remote_name"`
	URL                 string `yaml:"url"`
	Vendor              string `yaml:"vendor"`
	Username            string `yaml:"username"`
	RcloneConfig        string `yaml:"rclone_config"`
	RemoteRoot          string `yaml:"remote_root"`
	VerifyTLS           bool   `yaml:"verify_tls"`
	CAFile              string `yaml:"ca_file"`
	AllowHTTP           bool   `yaml:"allow_http"`
	AllowPrivateNetwork bool   `yaml:"allow_private_network"`
	Transfers           int    `yaml:"transfers"`
	Checkers            int    `yaml:"checkers"`
	Timeout             string `yaml:"timeout"`
	Retries             int    `yaml:"retries"`
	RetriesSleep        string `yaml:"retries_sleep"`
}

type SFTPStorage struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Path     string `yaml:"path"`
	KeyFile  string `yaml:"key_file"`
}

type S3Storage struct {
	Endpoint       string `yaml:"endpoint"`
	Bucket         string `yaml:"bucket"`
	Prefix         string `yaml:"prefix"`
	Region         string `yaml:"region"`
	CredentialFile string `yaml:"credential_file"`
	PathStyle      bool   `yaml:"path_style"`
}

type Database struct {
	ID                string `yaml:"id"`
	Name              string `yaml:"name"`
	Type              string `yaml:"type"`
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	Database          string `yaml:"database"`
	Username          string `yaml:"username"`
	CredentialFile    string `yaml:"credential_file"`
	Path              string `yaml:"path"`
	Format            string `yaml:"format"`
	ConnectTimeout    string `yaml:"connect_timeout"`
	SSLMode           string `yaml:"sslmode"`
	SingleTransaction bool   `yaml:"single_transaction"`
}

type Job struct {
	ID            string           `yaml:"id"`
	Name          string           `yaml:"name"`
	Enabled       bool             `yaml:"enabled"`
	StorageID     string           `yaml:"storage_id"`
	Sources       Sources          `yaml:"sources"`
	Excludes      []string         `yaml:"excludes"`
	Schedule      Schedule         `yaml:"schedule"`
	Retention     Retention        `yaml:"retention"`
	Verification  Verification     `yaml:"verification"`
	Restic        Restic           `yaml:"restic"`
	Notifications JobNotifications `yaml:"notifications"`
	Monitoring    JobMonitoring    `yaml:"monitoring"`
}

type Sources struct {
	Paths         []string `yaml:"paths"`
	Databases     []string `yaml:"databases"`
	OneFileSystem bool     `yaml:"one_file_system"`
	StrictPaths   bool     `yaml:"strict_paths"`
}

type Schedule struct {
	Enabled         bool   `yaml:"enabled"`
	Type            string `yaml:"type"`
	Expression      string `yaml:"expression"`
	Persistent      bool   `yaml:"persistent"`
	RandomizedDelay string `yaml:"randomized_delay"`
	GracePeriod     string `yaml:"grace_period"`
	Timeout         string `yaml:"timeout"`
}

type Retention struct {
	KeepLast          int    `yaml:"keep_last"`
	KeepHourly        int    `yaml:"keep_hourly"`
	KeepDaily         int    `yaml:"keep_daily"`
	KeepWeekly        int    `yaml:"keep_weekly"`
	KeepMonthly       int    `yaml:"keep_monthly"`
	KeepYearly        int    `yaml:"keep_yearly"`
	KeepWithin        string `yaml:"keep_within"`
	ForgetAfterBackup bool   `yaml:"forget_after_backup"`
	PruneSchedule     string `yaml:"prune_schedule"`
}

type Verification struct {
	MetadataAfterBackup bool   `yaml:"metadata_after_backup"`
	StandardSchedule    string `yaml:"standard_schedule"`
	FullSchedule        string `yaml:"full_schedule"`
	FullReadDataSubset  string `yaml:"full_read_data_subset"`
}

type Restic struct {
	Compression     string   `yaml:"compression"`
	ReadConcurrency int      `yaml:"read_concurrency"`
	PackSizeMB      int      `yaml:"pack_size_mb"`
	ExtraTags       []string `yaml:"extra_tags"`
	BackupMode      string   `yaml:"backup_mode,omitempty"`
}

type JobNotifications struct {
	OnSuccess  []string `yaml:"on_success"`
	OnWarning  []string `yaml:"on_warning"`
	OnFailure  []string `yaml:"on_failure"`
	OnRecovery []string `yaml:"on_recovery"`
}

type JobMonitoring struct {
	Report    bool `yaml:"report"`
	Heartbeat bool `yaml:"heartbeat"`
}

type Notification struct {
	ID         string         `yaml:"id"`
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	Enabled    bool           `yaml:"enabled"`
	SecretFile string         `yaml:"secret_file"`
	Settings   map[string]any `yaml:"settings"`
}

type Monitoring struct {
	Enabled             bool   `yaml:"enabled"`
	Endpoint            string `yaml:"endpoint"`
	NodeID              string `yaml:"node_id"`
	KeyFile             string `yaml:"key_file"`
	KeyVersion          int    `yaml:"key_version"`
	ReportSystemInfo    bool   `yaml:"report_system_info"`
	ReportCapacityStats bool   `yaml:"report_capacity_stats"`
	HeartbeatEnabled    bool   `yaml:"heartbeat_enabled"`
	HeartbeatInterval   string `yaml:"heartbeat_interval"`
	RequestTimeout      string `yaml:"request_timeout"`
	MaxPendingEvents    int    `yaml:"max_pending_events"`
	EventRetention      string `yaml:"event_retention"`
}

func Default() Config {
	host, _ := os.Hostname()
	return Config{
		Version:    1,
		Global:     Global{Hostname: host, DisplayName: host, Timezone: "UTC", Language: "zh-CN", StateDB: "/var/lib/sbackup/state.db", TempDir: "/var/lib/sbackup/tmp", LogDir: "/var/log/sbackup", MaxParallelJobs: 1, LogRetentionDays: 30, LogMaxTotalMB: 500},
		Tools:      Tools{ResticPath: "restic", RclonePath: "rclone", PGDumpPath: "pg_dump", MySQLDumpPath: "mysqldump", SQLitePath: "sqlite3"},
		Monitoring: Monitoring{KeyVersion: 1, HeartbeatInterval: "5m", RequestTimeout: "10s", MaxPendingEvents: 10000, EventRetention: "30d"},
	}
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func Save(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lockFile, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("锁定配置文件: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	if old, err := os.ReadFile(path); err == nil {
		if err := writeFileAtomic(path+".bak", old, 0600); err != nil {
			return fmt.Errorf("备份配置文件: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(path, b, 0600)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
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
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("不支持的配置版本 %d", c.Version)
	}
	if c.Global.StateDB == "" {
		c.Global.StateDB = "/var/lib/sbackup/state.db"
	}
	if c.Global.TempDir == "" {
		c.Global.TempDir = "/var/lib/sbackup/tmp"
	}
	if c.Global.LogDir == "" {
		c.Global.LogDir = "/var/log/sbackup"
	}
	if c.Global.Timezone == "" {
		c.Global.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(c.Global.Timezone); err != nil {
		return fmt.Errorf("无效时区 %q", c.Global.Timezone)
	}
	for label, path := range map[string]string{"state_db": c.Global.StateDB, "temp_dir": c.Global.TempDir, "log_dir": c.Global.LogDir} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("global.%s 必须是绝对路径", label)
		}
	}
	if c.Global.MaxParallelJobs < 1 || c.Global.LogRetentionDays < 0 || c.Global.LogMaxTotalMB < 0 {
		return errors.New("全局并发数必须大于 0，日志保留天数和容量不能为负数")
	}
	ids := map[string]string{}
	storageIDs := map[string]bool{}
	for _, s := range c.Storages {
		if err := validateID("存储", s.ID, ids); err != nil {
			return err
		}
		storageIDs[s.ID] = true
		if s.PasswordFile == "" {
			return fmt.Errorf("存储 %s 缺少 password_file", s.ID)
		}
		switch s.Type {
		case "local":
			if s.RepositoryPath == "" {
				return fmt.Errorf("本地存储 %s 缺少 repository_path", s.ID)
			}
		case "webdav":
			if s.WebDAV == nil || s.WebDAV.RemoteName == "" || s.WebDAV.URL == "" {
				return fmt.Errorf("WebDAV 存储 %s 配置不完整", s.ID)
			}
			if strings.HasPrefix(strings.ToLower(s.WebDAV.URL), "http://") && !s.WebDAV.AllowHTTP {
				return fmt.Errorf("WebDAV 存储 %s 使用 HTTP，但 allow_http 未开启", s.ID)
			}
			if _, err := url.ParseRequestURI(s.WebDAV.URL); err != nil {
				return fmt.Errorf("WebDAV 存储 %s URL 无效", s.ID)
			}
		case "sftp":
			if s.SFTP == nil || s.SFTP.Host == "" || s.SFTP.Username == "" || s.SFTP.Path == "" || s.SFTP.KeyFile == "" {
				return fmt.Errorf("SFTP 存储 %s 配置不完整", s.ID)
			}
		case "s3":
			if s.S3 == nil || s.S3.Endpoint == "" || s.S3.Bucket == "" {
				return fmt.Errorf("S3 存储 %s 配置不完整", s.ID)
			}
		default:
			return fmt.Errorf("存储 %s 类型 %q 不受支持", s.ID, s.Type)
		}
	}
	dbIDs := map[string]bool{}
	for _, d := range c.Databases {
		if err := validateID("数据库", d.ID, ids); err != nil {
			return err
		}
		dbIDs[d.ID] = true
		if d.Type != "postgres" && d.Type != "mysql" && d.Type != "sqlite" {
			return fmt.Errorf("数据库 %s 类型无效", d.ID)
		}
		if d.Type == "sqlite" && (d.Path == "" || !filepath.IsAbs(d.Path)) {
			return fmt.Errorf("SQLite %s 的 path 必须是绝对路径", d.ID)
		}
		if d.Type != "sqlite" && d.CredentialFile == "" {
			return fmt.Errorf("数据库 %s 缺少 credential_file", d.ID)
		}
	}
	notifyIDs := map[string]bool{}
	for _, n := range c.Notifications {
		if err := validateID("通知", n.ID, ids); err != nil {
			return err
		}
		notifyIDs[n.ID] = true
	}
	for _, j := range c.Jobs {
		if err := validateID("任务", j.ID, ids); err != nil {
			return err
		}
		if !storageIDs[j.StorageID] {
			return fmt.Errorf("任务 %s 引用了不存在的存储 %s", j.ID, j.StorageID)
		}
		if len(j.Sources.Paths)+len(j.Sources.Databases) == 0 {
			return fmt.Errorf("任务 %s 没有备份来源", j.ID)
		}
		for _, id := range j.Sources.Databases {
			if !dbIDs[id] {
				return fmt.Errorf("任务 %s 引用了不存在的数据库 %s", j.ID, id)
			}
		}
		for _, group := range [][]string{j.Notifications.OnSuccess, j.Notifications.OnWarning, j.Notifications.OnFailure, j.Notifications.OnRecovery} {
			for _, id := range group {
				if !notifyIDs[id] {
					return fmt.Errorf("任务 %s 引用了不存在的通知 %s", j.ID, id)
				}
			}
		}
		if j.Schedule.Timeout != "" {
			if d, err := time.ParseDuration(j.Schedule.Timeout); err != nil || d <= 0 {
				return fmt.Errorf("任务 %s timeout 无效", j.ID)
			}
		}
		if j.Schedule.Enabled {
			if j.Schedule.Expression == "" {
				return fmt.Errorf("任务 %s 启用了调度但缺少 expression", j.ID)
			}
			scheduleType := j.Schedule.Type
			if scheduleType == "" {
				scheduleType = "calendar"
			}
			switch scheduleType {
			case "calendar":
				for _, expression := range strings.Split(j.Schedule.Expression, ";") {
					if strings.TrimSpace(expression) == "" {
						return fmt.Errorf("任务 %s 包含空的日历调度表达式", j.ID)
					}
				}
			case "interval":
				if d, err := time.ParseDuration(j.Schedule.Expression); err != nil || d < time.Minute {
					return fmt.Errorf("任务 %s 的间隔调度无效（最短 1m）", j.ID)
				}
			default:
				return fmt.Errorf("任务 %s 的调度类型 %q 无效", j.ID, j.Schedule.Type)
			}
		}
		if j.Schedule.RandomizedDelay != "" {
			if d, err := time.ParseDuration(j.Schedule.RandomizedDelay); err != nil || d < 0 {
				return fmt.Errorf("任务 %s randomized_delay 无效", j.ID)
			}
		}
		if j.Verification.FullReadDataSubset != "" {
			if err := validateReadSubset(j.Verification.FullReadDataSubset); err != nil {
				return fmt.Errorf("任务 %s full_read_data_subset 无效: %w", j.ID, err)
			}
		}
		if j.Schedule.GracePeriod != "" {
			if d, err := time.ParseDuration(j.Schedule.GracePeriod); err != nil || d < 0 {
				return fmt.Errorf("任务 %s grace_period 无效", j.ID)
			}
		}
		if j.Restic.BackupMode != "" && j.Restic.BackupMode != "incremental" && j.Restic.BackupMode != "full" {
			return fmt.Errorf("任务 %s 的 backup_mode 必须是 incremental 或 full", j.ID)
		}
		if j.Restic.Compression != "" && j.Restic.Compression != "auto" && j.Restic.Compression != "off" && j.Restic.Compression != "max" {
			return fmt.Errorf("任务 %s 的 compression 必须是 auto、off 或 max", j.ID)
		}
		if hasDuplicates(j.Sources.Paths) || hasDuplicates(j.Sources.Databases) {
			return fmt.Errorf("任务 %s 包含重复来源", j.ID)
		}
		if retentionEmpty(j.Retention) {
			return fmt.Errorf("任务 %s 的保留策略不能全部为空", j.ID)
		}
	}
	if c.Monitoring.Enabled {
		if c.Monitoring.Endpoint == "" || c.Monitoring.NodeID == "" || c.Monitoring.KeyFile == "" {
			return errors.New("监控已启用但 endpoint、node_id 或 key_file 缺失")
		}
		if !strings.HasPrefix(strings.ToLower(c.Monitoring.Endpoint), "https://") {
			return errors.New("监控 endpoint 必须使用 HTTPS")
		}
		u, err := url.Parse(c.Monitoring.Endpoint)
		if err != nil || u.User != nil {
			return errors.New("监控 endpoint 无效或包含 URL 凭据")
		}
		if !strings.HasSuffix(u.Path, "/api/v1/report") {
			return errors.New("监控 endpoint 必须以 /api/v1/report 结尾")
		}
		if c.Monitoring.RequestTimeout != "" {
			if d, err := time.ParseDuration(c.Monitoring.RequestTimeout); err != nil || d <= 0 {
				return errors.New("request_timeout 无效")
			}
		}
		if c.Monitoring.EventRetention != "" {
			if _, err := ParseDuration(c.Monitoring.EventRetention); err != nil {
				return errors.New("event_retention 无效")
			}
		}
		if c.Monitoring.KeyVersion < 1 {
			return errors.New("key_version 必须大于 0")
		}
		if d, err := time.ParseDuration(c.Monitoring.HeartbeatInterval); c.Monitoring.HeartbeatEnabled && (err != nil || d <= 0) {
			return errors.New("heartbeat_interval 必须是正数时长")
		}
	}
	return nil
}

// ParseDuration accepts Go durations plus whole-day values such as "30d".
func ParseDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("无效天数 %q", value)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("时长必须大于 0: %q", value)
	}
	return d, nil
}

func validateReadSubset(value string) error {
	if strings.HasSuffix(value, "%") {
		n, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
		if err != nil || n < 1 || n > 100 {
			return errors.New("百分比必须在 1% 到 100% 之间")
		}
		return nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return errors.New("必须是百分比或 N/M 格式")
	}
	n, errN := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errN != nil || errM != nil || n < 1 || m < 1 || n > m {
		return errors.New("N/M 必须满足 1 <= N <= M")
	}
	return nil
}

func validateID(kind, id string, seen map[string]string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%s ID %q 无效", kind, id)
	}
	if old := seen[id]; old != "" {
		return fmt.Errorf("ID %q 在 %s 和 %s 中重复", id, old, kind)
	}
	seen[id] = kind
	return nil
}
func retentionEmpty(r Retention) bool {
	return r.KeepLast+r.KeepHourly+r.KeepDaily+r.KeepWeekly+r.KeepMonthly+r.KeepYearly == 0 && r.KeepWithin == ""
}
func hasDuplicates(values []string) bool {
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}
func (c *Config) Job(id string) (*Job, bool) {
	for i := range c.Jobs {
		if c.Jobs[i].ID == id {
			return &c.Jobs[i], true
		}
	}
	return nil, false
}
func (c *Config) Storage(id string) (*Storage, bool) {
	for i := range c.Storages {
		if c.Storages[i].ID == id {
			return &c.Storages[i], true
		}
	}
	return nil, false
}
func (c *Config) Database(id string) (*Database, bool) {
	for i := range c.Databases {
		if c.Databases[i].ID == id {
			return &c.Databases[i], true
		}
	}
	return nil, false
}
func (c *Config) Notification(id string) (*Notification, bool) {
	for i := range c.Notifications {
		if c.Notifications[i].ID == id {
			return &c.Notifications[i], true
		}
	}
	return nil, false
}
