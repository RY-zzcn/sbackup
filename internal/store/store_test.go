package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunAndOutboxPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := Run{ID: "run-1", JobID: "job", Status: "success", Phase: "completed", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), SnapshotID: "abc"}
	if err := s.SaveRun(r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue("report", "endpoint", map[string]string{"ok": "yes"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.LastRun("job")
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != "abc" {
		t.Fatalf("got %q", got.SnapshotID)
	}
	items, err := s.DueOutbox(10)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}

func TestDeleteJobRunsAndLogs(t *testing.T) {
	root := t.TempDir()
	s, err := Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	logPath := filepath.Join(root, "run.jsonl")
	if err := os.WriteFile(logPath, []byte("log\n"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.SaveRun(Run{ID: "run-delete", JobID: "job", Status: "success", Phase: "completed", StartedAt: now, FinishedAt: now, LogPath: logPath}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteJobRuns("job", true, root); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LastRun("job"); !IsNotFound(err) {
		t.Fatalf("run still exists: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("log still exists: %v", err)
	}
}

func TestDeleteJobRunsRefusesLogOutsideDirectory(t *testing.T) {
	root := t.TempDir()
	logRoot := filepath.Join(root, "logs")
	outside := filepath.Join(root, "outside.jsonl")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	if err := s.SaveRun(Run{ID: "unsafe-log", JobID: "job", Status: "failed", Phase: "completed", StartedAt: now, LogPath: outside}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteJobRuns("job", true, logRoot); err == nil {
		t.Fatal("outside log deletion was not rejected")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}
