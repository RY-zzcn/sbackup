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
