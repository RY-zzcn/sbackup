package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

type Run struct {
	ID, JobID, Status, Phase                  string
	ScheduledAt, StartedAt, FinishedAt        time.Time
	SnapshotID                                string
	FilesNew, FilesChanged, FilesUnmodified   int64
	BytesAdded, BytesProcessed, DurationMS    int64
	Warning, ErrorCode, ErrorSummary, LogPath string
}

type Outbox struct {
	ID, Kind, DestinationID string
	Payload                 []byte
	Attempts                int
	NextAttemptAt           time.Time
	LastError               string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS runs (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL, status TEXT NOT NULL, phase TEXT NOT NULL,
 scheduled_at TEXT, started_at TEXT NOT NULL, finished_at TEXT, snapshot_id TEXT NOT NULL DEFAULT '',
 files_new INTEGER NOT NULL DEFAULT 0, files_changed INTEGER NOT NULL DEFAULT 0,
 files_unmodified INTEGER NOT NULL DEFAULT 0, bytes_added INTEGER NOT NULL DEFAULT 0,
 bytes_processed INTEGER NOT NULL DEFAULT 0, duration_ms INTEGER NOT NULL DEFAULT 0,
 warning TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT '',
 error_summary TEXT NOT NULL DEFAULT '', log_path TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_runs_job_started ON runs(job_id, started_at DESC);
CREATE TABLE IF NOT EXISTS outbox (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, destination_id TEXT NOT NULL, payload BLOB NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL,
 created_at TEXT NOT NULL, last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_outbox_due ON outbox(next_attempt_at, created_at);
`)
	return err
}

func timeValue(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(v sql.NullString) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, v.String)
	return t
}

func (s *Store) SaveRun(r Run) error {
	_, err := s.DB.Exec(`INSERT INTO runs
(id,job_id,status,phase,scheduled_at,started_at,finished_at,snapshot_id,files_new,files_changed,files_unmodified,bytes_added,bytes_processed,duration_ms,warning,error_code,error_summary,log_path)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,phase=excluded.phase,scheduled_at=excluded.scheduled_at,
finished_at=excluded.finished_at,snapshot_id=excluded.snapshot_id,files_new=excluded.files_new,
files_changed=excluded.files_changed,files_unmodified=excluded.files_unmodified,bytes_added=excluded.bytes_added,
bytes_processed=excluded.bytes_processed,duration_ms=excluded.duration_ms,warning=excluded.warning,
error_code=excluded.error_code,error_summary=excluded.error_summary,log_path=excluded.log_path`,
		r.ID, r.JobID, r.Status, r.Phase, timeValue(r.ScheduledAt), timeValue(r.StartedAt), timeValue(r.FinishedAt),
		r.SnapshotID, r.FilesNew, r.FilesChanged, r.FilesUnmodified, r.BytesAdded, r.BytesProcessed,
		r.DurationMS, r.Warning, r.ErrorCode, r.ErrorSummary, r.LogPath)
	return err
}

func (s *Store) RecentRuns(jobID string, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT id,job_id,status,phase,scheduled_at,started_at,finished_at,snapshot_id,files_new,files_changed,files_unmodified,bytes_added,bytes_processed,duration_ms,warning,error_code,error_summary,log_path FROM runs`
	args := []any{}
	if jobID != "" {
		q += " WHERE job_id=?"
		args = append(args, jobID)
	}
	q += " ORDER BY started_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		var scheduled, started, finished sql.NullString
		if err := rows.Scan(&r.ID, &r.JobID, &r.Status, &r.Phase, &scheduled, &started, &finished, &r.SnapshotID,
			&r.FilesNew, &r.FilesChanged, &r.FilesUnmodified, &r.BytesAdded, &r.BytesProcessed, &r.DurationMS,
			&r.Warning, &r.ErrorCode, &r.ErrorSummary, &r.LogPath); err != nil {
			return nil, err
		}
		r.ScheduledAt, r.StartedAt, r.FinishedAt = parseTime(scheduled), parseTime(started), parseTime(finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) LastRun(jobID string) (Run, error) {
	runs, err := s.RecentRuns(jobID, 1)
	if err != nil {
		return Run{}, err
	}
	if len(runs) == 0 {
		return Run{}, sql.ErrNoRows
	}
	return runs[0], nil
}

func (s *Store) LastCompletedBefore(jobID, runID string) (Run, error) {
	row := s.DB.QueryRow(`SELECT id,job_id,status,phase,scheduled_at,started_at,finished_at,snapshot_id,files_new,files_changed,files_unmodified,bytes_added,bytes_processed,duration_ms,warning,error_code,error_summary,log_path FROM runs WHERE job_id=? AND id<>? AND status<>'running' ORDER BY started_at DESC LIMIT 1`, jobID, runID)
	var r Run
	var scheduled, started, finished sql.NullString
	err := row.Scan(&r.ID, &r.JobID, &r.Status, &r.Phase, &scheduled, &started, &finished, &r.SnapshotID,
		&r.FilesNew, &r.FilesChanged, &r.FilesUnmodified, &r.BytesAdded, &r.BytesProcessed, &r.DurationMS,
		&r.Warning, &r.ErrorCode, &r.ErrorSummary, &r.LogPath)
	if err != nil {
		return Run{}, err
	}
	r.ScheduledAt, r.StartedAt, r.FinishedAt = parseTime(scheduled), parseTime(started), parseTime(finished)
	return r, nil
}

func (s *Store) Enqueue(kind, destination string, payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	id, now := NewID(), time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.DB.Exec(`INSERT INTO outbox(id,kind,destination_id,payload,attempts,next_attempt_at,created_at) VALUES(?,?,?,?,0,?,?)`, id, kind, destination, b, now, now)
	return id, err
}

func (s *Store) DueOutbox(limit int) ([]Outbox, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id,kind,destination_id,payload,attempts,next_attempt_at,last_error FROM outbox WHERE next_attempt_at<=? ORDER BY created_at LIMIT ?`, time.Now().UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outbox
	for rows.Next() {
		var o Outbox
		var next string
		if err := rows.Scan(&o.ID, &o.Kind, &o.DestinationID, &o.Payload, &o.Attempts, &next, &o.LastError); err != nil {
			return nil, err
		}
		o.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, next)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) PendingOutbox() (int, error) {
	var count int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count)
	return count, err
}

func (s *Store) CompleteOutbox(id string) error {
	_, err := s.DB.Exec(`DELETE FROM outbox WHERE id=?`, id)
	return err
}

func (s *Store) FailOutbox(id, message string, attempts int) error {
	delay := time.Minute * time.Duration(1<<min(attempts, 8))
	if delay > 12*time.Hour {
		delay = 12 * time.Hour
	}
	_, err := s.DB.Exec(`UPDATE outbox SET attempts=?,next_attempt_at=?,last_error=? WHERE id=?`, attempts, time.Now().Add(delay).UTC().Format(time.RFC3339Nano), message, id)
	return err
}

func (s *Store) Prune(maxRuns, maxPending int, eventRetention time.Duration) error {
	if maxRuns > 0 {
		_, _ = s.DB.Exec(`DELETE FROM runs WHERE id NOT IN (SELECT id FROM runs ORDER BY started_at DESC LIMIT ?)`, maxRuns)
	}
	if eventRetention > 0 {
		cutoff := time.Now().Add(-eventRetention).UTC().Format(time.RFC3339Nano)
		_, _ = s.DB.Exec(`DELETE FROM outbox WHERE created_at<?`, cutoff)
	}
	if maxPending > 0 {
		_, _ = s.DB.Exec(`DELETE FROM outbox WHERE id IN (SELECT id FROM outbox ORDER BY created_at DESC LIMIT -1 OFFSET ?)`, maxPending)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func NewID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("%013d-%016x", now.UnixMilli(), uint64(now.UnixNano()))
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist)
}
