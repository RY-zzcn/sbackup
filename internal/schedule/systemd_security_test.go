package schedule

import (
	"testing"

	"sbackup/internal/config"
)

func TestInstallRejectsNewlines(t *testing.T) {
	job := config.Job{ID: "job", Schedule: config.Schedule{Expression: "*-*-* 02:00:00\nExecStart=/bin/sh"}}
	if err := Install(job, "/usr/bin/sbackup", "/etc/sbackup/config.yaml", t.TempDir()); err == nil {
		t.Fatal("newline in systemd expression accepted")
	}
}
