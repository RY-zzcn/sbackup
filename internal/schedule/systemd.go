package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"sbackup/internal/config"
)

var safeID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func Install(job config.Job, bin, configPath, dir string) error {
	if !safeID.MatchString(job.ID) {
		return fmt.Errorf("无效任务 ID")
	}
	for name, value := range map[string]string{"bin": bin, "config": configPath, "expression": job.Schedule.Expression, "delay": job.Schedule.RandomizedDelay} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s 不能包含换行", name)
		}
	}
	if dir == "" {
		dir = "/etc/systemd/system"
	}
	calendar := job.Schedule.Expression
	if calendar == "" {
		calendar = "*-*-* 02:00:00"
	}
	delay := job.Schedule.RandomizedDelay
	if delay == "" {
		delay = "0"
	}
	service := fmt.Sprintf("[Unit]\nDescription=SBackup job %s\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=oneshot\nExecStart=%s --config %s job run %s --scheduled\nNice=10\nIOSchedulingClass=best-effort\nIOSchedulingPriority=7\nNoNewPrivileges=true\nPrivateTmp=true\n", job.ID, systemdQuote(bin), systemdQuote(configPath), systemdQuote(job.ID))
	timer := fmt.Sprintf("[Unit]\nDescription=Schedule SBackup job %s\n\n[Timer]\nOnCalendar=%s\nPersistent=%t\nRandomizedDelaySec=%s\n\n[Install]\nWantedBy=timers.target\n", job.ID, calendar, job.Schedule.Persistent, delay)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "sbackup-job-"+job.ID+".service"), []byte(service), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sbackup-job-"+job.ID+".timer"), []byte(timer), 0644)
}

func systemdQuote(value string) string { return strconv.Quote(value) }
