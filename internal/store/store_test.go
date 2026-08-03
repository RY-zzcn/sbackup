package store

import (
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
