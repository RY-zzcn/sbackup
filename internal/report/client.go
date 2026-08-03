package report

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sbackup/internal/config"
	"sbackup/internal/store"
	"sbackup/pkg/reportprotocol"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Config  *config.Config
	Store   *store.Store
	HTTP    *http.Client
	Version string
}

func (c *Client) EventForRun(j config.Job, r store.Run, kind string) reportprotocol.Event {
	node := reportprotocol.Node{ID: c.Config.Monitoring.NodeID, Name: c.Config.Global.Hostname, DisplayName: c.Config.Global.DisplayName, ClientVersion: c.Version}
	if c.Config.Monitoring.ReportSystemInfo {
		node.OS = runtime.GOOS
		node.Arch = runtime.GOARCH
	}
	job := reportprotocol.Job{ID: j.ID, Name: j.Name, Enabled: j.Enabled, Timezone: c.Config.Global.Timezone, GraceSeconds: durationSeconds(j.Schedule.GracePeriod)}
	run := reportprotocol.Run{ID: r.ID, Status: r.Status, Phase: r.Phase, StartedAt: r.StartedAt, DurationMS: r.DurationMS, SnapshotID: short(r.SnapshotID), FilesNew: r.FilesNew, FilesChanged: r.FilesChanged, BytesProcessed: r.BytesProcessed, BytesAdded: r.BytesAdded, ErrorCode: r.ErrorCode, ErrorSummary: r.ErrorSummary}
	if !r.ScheduledAt.IsZero() {
		run.ScheduledAt = &r.ScheduledAt
	}
	if !r.FinishedAt.IsZero() {
		run.FinishedAt = &r.FinishedAt
	}
	return reportprotocol.Event{ProtocolVersion: 1, EventID: store.NewID(), EventType: kind, OccurredAt: time.Now().UTC(), Node: node, Job: &job, Run: &run}
}
func (c *Client) Heartbeat() reportprotocol.Event {
	enabled := 0
	for _, j := range c.Config.Jobs {
		if j.Enabled {
			enabled++
		}
	}
	pending, _ := c.Store.PendingOutbox()
	node := reportprotocol.Node{ID: c.Config.Monitoring.NodeID, Name: c.Config.Global.Hostname, DisplayName: c.Config.Global.DisplayName, ClientVersion: c.Version}
	if c.Config.Monitoring.ReportSystemInfo {
		node.OS, node.Arch = runtime.GOOS, runtime.GOARCH
	}
	return reportprotocol.Event{ProtocolVersion: 1, EventID: store.NewID(), EventType: "node.heartbeat", OccurredAt: time.Now().UTC(), Node: node, Heartbeat: &reportprotocol.Heartbeat{PendingReports: pending, JobsEnabled: enabled}}
}
func (c *Client) Send(ctx context.Context, e reportprotocol.Event) error {
	m := c.Config.Monitoring
	if !m.Enabled {
		return nil
	}
	key, err := os.ReadFile(m.KeyFile)
	if err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := store.NewID()
	path := "/api/v1/report"
	canonical := "POST\n" + path + "\n" + ts + "\n" + nonce + "\n" + shaHex(b)
	keyDigest := sha256.Sum256(bytes.TrimSpace(key))
	mac := hmac.New(sha256.New, keyDigest[:])
	_, _ = mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(m.Endpoint, "/"), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SBackup-Node", m.NodeID)
	req.Header.Set("X-SBackup-Key-Version", strconv.Itoa(m.KeyVersion))
	req.Header.Set("X-SBackup-Timestamp", ts)
	req.Header.Set("X-SBackup-Nonce", nonce)
	req.Header.Set("X-SBackup-Signature", "v1="+sig)
	client := c.HTTP
	if client == nil {
		timeout := 10 * time.Second
		if d, e := time.ParseDuration(m.RequestTimeout); e == nil {
			timeout = d
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("监控端返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

// Test sends one heartbeat even while monitoring is not yet enabled. This is
// used by the setup wizard to validate credentials before saving the setting.
func (c *Client) Test(ctx context.Context) error {
	wasEnabled := c.Config.Monitoring.Enabled
	c.Config.Monitoring.Enabled = true
	defer func() { c.Config.Monitoring.Enabled = wasEnabled }()
	return c.Send(ctx, c.Heartbeat())
}
func (c *Client) SendOrQueue(ctx context.Context, e reportprotocol.Event) {
	if err := c.Send(ctx, e); err != nil {
		_, _ = c.Store.Enqueue("report", c.Config.Monitoring.Endpoint, e)
	}
}
func shaHex(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
func durationSeconds(s string) int64 { d, _ := time.ParseDuration(s); return int64(d.Seconds()) }
