package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sbackup/internal/config"
)

func TestInstallQuotesExecPaths(t *testing.T) {
	dir := t.TempDir()
	job := config.Job{ID: "daily", Schedule: config.Schedule{Expression: "daily", Persistent: true}}
	if err := Install(job, "/opt/SBackup Client/sbackup", "/etc/sbackup config.yaml", dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sbackup-job-daily.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, `ExecStart="/opt/SBackup Client/sbackup" --config "/etc/sbackup config.yaml"`) {
		t.Fatalf("paths were not quoted: %s", text)
	}
}

func TestInstallMultipleCalendarTimes(t *testing.T) {
	dir := t.TempDir()
	job := config.Job{ID: "multi", Schedule: config.Schedule{Type: "calendar", Expression: "*-*-* 02:30:00;*-*-* 14:30:00", Persistent: true}}
	if err := Install(job, "/usr/bin/sbackup", "/etc/sbackup/config.yaml", dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sbackup-job-multi.timer"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "OnCalendar=*-*-* 02:30:00\nOnCalendar=*-*-* 14:30:00") {
		t.Fatalf("multiple times missing: %s", text)
	}
}

func TestInstallInterval(t *testing.T) {
	dir := t.TempDir()
	job := config.Job{ID: "interval", Schedule: config.Schedule{Type: "interval", Expression: "6h", Persistent: true}}
	if err := Install(job, "/usr/bin/sbackup", "/etc/sbackup/config.yaml", dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sbackup-job-interval.timer"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "OnBootSec=6h\nOnUnitActiveSec=6h") {
		t.Fatalf("interval trigger missing: %s", text)
	}
}
