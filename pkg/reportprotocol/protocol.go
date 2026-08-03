package reportprotocol

import "time"

type Event struct {
	ProtocolVersion int           `json:"protocol_version"`
	EventID         string        `json:"event_id"`
	EventType       string        `json:"event_type"`
	OccurredAt      time.Time     `json:"occurred_at"`
	Node            Node          `json:"node"`
	Job             *Job          `json:"job,omitempty"`
	Run             *Run          `json:"run,omitempty"`
	Heartbeat       *Heartbeat    `json:"heartbeat,omitempty"`
	Verification    *Verification `json:"verification,omitempty"`
}
type Node struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name,omitempty"`
	ClientVersion string `json:"client_version,omitempty"`
	OS            string `json:"os,omitempty"`
	Arch          string `json:"arch,omitempty"`
}
type Job struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Enabled        bool       `json:"enabled"`
	Timezone       string     `json:"timezone"`
	NextExpectedAt *time.Time `json:"next_expected_at,omitempty"`
	GraceSeconds   int64      `json:"grace_seconds"`
}
type Run struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	Phase          string     `json:"phase"`
	ScheduledAt    *time.Time `json:"scheduled_at,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	DurationMS     int64      `json:"duration_ms"`
	SnapshotID     string     `json:"snapshot_id,omitempty"`
	FilesNew       int64      `json:"files_new"`
	FilesChanged   int64      `json:"files_changed"`
	BytesProcessed int64      `json:"bytes_processed"`
	BytesAdded     int64      `json:"bytes_added"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorSummary   string     `json:"error_summary,omitempty"`
}
type Heartbeat struct {
	UptimeSeconds  int64 `json:"uptime_seconds"`
	PendingReports int   `json:"pending_reports"`
	JobsEnabled    int   `json:"jobs_enabled"`
	JobsRunning    int   `json:"jobs_running"`
}
type Verification struct {
	ID           string     `json:"id"`
	Level        string     `json:"level"`
	Status       string     `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorSummary string     `json:"error_summary,omitempty"`
}
