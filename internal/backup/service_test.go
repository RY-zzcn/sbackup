package backup

import (
	"encoding/json"
	"testing"

	"sbackup/internal/config"
)

func TestResticSummaryJSONTags(t *testing.T) {
	var s Summary
	input := `{"message_type":"summary","snapshot_id":"snap","files_new":2,"files_changed":3,"files_unmodified":4,"data_added":5,"total_bytes_processed":6}`
	if err := json.Unmarshal([]byte(input), &s); err != nil {
		t.Fatal(err)
	}
	if s.SnapshotID != "snap" || s.FilesChanged != 3 || s.TotalBytesProcessed != 6 {
		t.Fatalf("bad summary: %#v", s)
	}
}

func TestBackupMode(t *testing.T) {
	job := config.Job{Restic: config.Restic{BackupMode: "full"}}
	if got, err := backupMode(job, ""); err != nil || got != "full" {
		t.Fatalf("configured mode: got=%q err=%v", got, err)
	}
	if got, err := backupMode(job, "incremental"); err != nil || got != "incremental" {
		t.Fatalf("override mode: got=%q err=%v", got, err)
	}
	if _, err := backupMode(job, "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
