package monitor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sbackup/pkg/reportprotocol"
)

type Config struct{ Listen, DataFile, AdminUsername, AdminPassword string }
type Server struct {
	cfg    Config
	mu     sync.Mutex
	state  State
	nonces map[string]time.Time
}
type State struct {
	Nodes   map[string]Node        `json:"nodes"`
	Reports []reportprotocol.Event `json:"reports"`
}
type Node struct {
	ID, Name, SecretHex string
	KeyVersion          int
	LastHeartbeat       time.Time
	Jobs                map[string]JobState
}
type JobState struct {
	Name, Status                           string
	LastAttempt, LastSuccess, NextExpected time.Time
	ConsecutiveFailures                    int
	GraceSeconds                           int64
}
type JobView struct {
	ID, Name, Status                       string
	LastAttempt, LastSuccess, NextExpected time.Time
	ConsecutiveFailures                    int
}
type NodeView struct {
	ID, Name, Status string
	LastHeartbeat    time.Time
	Jobs             []JobView
}
type pageData struct {
	Nodes   []NodeView
	Reports []reportprotocol.Event
}

func New(cfg Config) *Server {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8788"
	}
	if cfg.DataFile == "" {
		cfg.DataFile = "/var/lib/sbackup-monitor/state.json"
	}
	s := &Server{cfg: cfg, state: State{Nodes: map[string]Node{}}, nonces: map[string]time.Time{}}
	_ = s.load()
	return s
}
func (s *Server) AddNode(id, name, key string) error {
	key = strings.TrimSpace(key)
	if id == "" || len(key) < 32 {
		return fmt.Errorf("node id 不能为空，key 至少需要 32 个字符")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.state.Nodes[id]
	n.ID = id
	n.Name = name
	digest := sha256.Sum256([]byte(key))
	n.SecretHex = hex.EncodeToString(digest[:])
	n.KeyVersion = 1
	if n.Jobs == nil {
		n.Jobs = map[string]JobState{}
	}
	s.state.Nodes[id] = n
	return s.saveLocked()
}
func (s *Server) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/api/v1/report", s.report)
	m.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	m.HandleFunc("/", s.index)
	return securityHeaders(m)
}
func (s *Server) Serve() error {
	server := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.ContentLength > 256<<10 {
		http.Error(w, "request too large", 413)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	nodeID, ts, nonce, sig := r.Header.Get("X-SBackup-Node"), r.Header.Get("X-SBackup-Timestamp"), r.Header.Get("X-SBackup-Nonce"), r.Header.Get("X-SBackup-Signature")
	keyVersion, keyErr := strconv.Atoi(r.Header.Get("X-SBackup-Key-Version"))
	unix, err := strconv.ParseInt(ts, 10, 64)
	if nodeID == "" || nonce == "" || sig == "" || err != nil || keyErr != nil || abs(time.Since(time.Unix(unix, 0))) > 5*time.Minute {
		http.Error(w, "unauthorized", 401)
		return
	}
	s.mu.Lock()
	n, ok := s.state.Nodes[nodeID]
	expires, seen := s.nonces[nodeID+":"+nonce]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "unknown node", 401)
		return
	}
	if n.KeyVersion != 0 && keyVersion != n.KeyVersion {
		http.Error(w, "unknown key version", 401)
		return
	}
	if seen && expires.After(time.Now()) {
		http.Error(w, "replayed request", 401)
		return
	}
	secret, err := hex.DecodeString(n.SecretHex)
	if err != nil {
		http.Error(w, "unauthorized", 401)
		return
	}
	hash := sha256.Sum256(body)
	canonical := "POST\n" + r.URL.Path + "\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(hash[:])
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if !strings.HasPrefix(sig, "v1=") || !hmac.Equal([]byte(want), []byte(sig[3:])) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var e reportprotocol.Event
	if err := json.Unmarshal(body, &e); err != nil || e.EventID == "" {
		http.Error(w, "invalid json", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.state.Reports {
		if old.EventID == e.EventID {
			writeJSON(w, map[string]any{"ok": true, "event_id": e.EventID, "duplicate": true, "server_time": time.Now().UTC()})
			return
		}
	}
	s.nonces[nodeID+":"+nonce] = time.Now().Add(10 * time.Minute)
	s.cleanupNoncesLocked()
	previousNode := n
	previousReportCount := len(s.state.Reports)
	n = s.state.Nodes[nodeID]
	if e.Node.Name != "" {
		n.Name = e.Node.Name
	}
	if e.EventType == "node.heartbeat" {
		n.LastHeartbeat = e.OccurredAt
	}
	if e.Job != nil {
		js := n.Jobs[e.Job.ID]
		js.Name = e.Job.Name
		js.GraceSeconds = e.Job.GraceSeconds
		if e.Job.NextExpectedAt != nil {
			js.NextExpected = *e.Job.NextExpectedAt
		}
		if e.Run != nil {
			js.Status = e.Run.Status
			js.LastAttempt = e.Run.StartedAt
			if e.Run.Status == "success" || e.Run.Status == "warning" {
				js.LastSuccess = e.OccurredAt
				js.ConsecutiveFailures = 0
			} else if e.Run.Status == "failed" {
				js.ConsecutiveFailures++
			}
		}
		n.Jobs[e.Job.ID] = js
	}
	s.state.Nodes[nodeID] = n
	s.state.Reports = append(s.state.Reports, e)
	if len(s.state.Reports) > 10000 {
		s.state.Reports = s.state.Reports[len(s.state.Reports)-10000:]
	}
	if err := s.saveLocked(); err != nil {
		s.state.Nodes[nodeID] = previousNode
		s.state.Reports = s.state.Reports[:previousReportCount]
		delete(s.nonces, nodeID+":"+nonce)
		http.Error(w, "state persistence failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "event_id": e.EventID, "duplicate": false, "server_time": time.Now().UTC()})
}
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AdminUsername != "" {
		u, p, ok := r.BasicAuth()
		if !ok || !hmac.Equal([]byte(u), []byte(s.cfg.AdminUsername)) || !hmac.Equal([]byte(p), []byte(s.cfg.AdminPassword)) {
			w.Header().Set("WWW-Authenticate", `Basic realm="SBackup Monitor"`)
			http.Error(w, "unauthorized", 401)
			return
		}
	}
	s.mu.Lock()
	d := s.viewLocked()
	s.mu.Unlock()
	const page = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>SBackup Monitor</title><style>body{font-family:system-ui,sans-serif;background:#f4f6f8;color:#17202a;margin:0;padding:24px}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(290px,1fr));gap:16px}.card,section{background:#fff;border-radius:12px;padding:18px;box-shadow:0 2px 10px #0001;margin-bottom:18px}.healthy{color:#16803c}.warning,.missed{color:#a86600}.critical,.offline{color:#b42318}.unknown{color:#667085}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:8px;border-bottom:1px solid #eee}small{color:#667085}</style></head><body><h1>SBackup Monitor</h1><div class="grid">{{range .Nodes}}<div class="card"><h2>{{.Name}}</h2><p class="{{.Status}}">{{.Status}}</p><small>{{.ID}} · 心跳 {{.LastHeartbeat}}</small><table><tr><th>任务</th><th>状态</th><th>连续失败</th></tr>{{range .Jobs}}<tr><td>{{.Name}}</td><td class="{{.Status}}">{{.Status}}</td><td>{{.ConsecutiveFailures}}</td></tr>{{end}}</table></div>{{else}}<div class="card">暂无节点</div>{{end}}</div><section><h2>最近事件</h2><table><tr><th>时间</th><th>节点</th><th>任务</th><th>事件</th><th>状态</th></tr>{{range .Reports}}<tr><td>{{.OccurredAt}}</td><td>{{.Node.Name}}</td><td>{{if .Job}}{{.Job.Name}}{{end}}</td><td>{{.EventType}}</td><td>{{if .Run}}{{.Run.Status}}{{end}}</td></tr>{{end}}</table></section></body></html>`
	_ = template.Must(template.New("page").Parse(page)).Execute(w, d)
}
func (s *Server) viewLocked() pageData {
	now := time.Now()
	d := pageData{}
	for _, n := range s.state.Nodes {
		nv := NodeView{ID: n.ID, Name: n.Name, LastHeartbeat: n.LastHeartbeat, Status: "healthy"}
		if n.LastHeartbeat.IsZero() {
			nv.Status = "unknown"
		} else if now.Sub(n.LastHeartbeat) > 15*time.Minute {
			nv.Status = "offline"
		}
		for id, j := range n.Jobs {
			status := j.Status
			if !j.NextExpected.IsZero() && now.After(j.NextExpected.Add(time.Duration(j.GraceSeconds)*time.Second)) {
				status = "missed"
			}
			if status == "failed" {
				status = "critical"
			}
			nv.Jobs = append(nv.Jobs, JobView{ID: id, Name: j.Name, Status: status, LastAttempt: j.LastAttempt, LastSuccess: j.LastSuccess, NextExpected: j.NextExpected, ConsecutiveFailures: j.ConsecutiveFailures})
			if status == "critical" || status == "missed" {
				nv.Status = status
			}
		}
		sort.Slice(nv.Jobs, func(i, j int) bool { return nv.Jobs[i].Name < nv.Jobs[j].Name })
		d.Nodes = append(d.Nodes, nv)
	}
	sort.Slice(d.Nodes, func(i, j int) bool { return d.Nodes[i].Name < d.Nodes[j].Name })
	start := 0
	if len(s.state.Reports) > 100 {
		start = len(s.state.Reports) - 100
	}
	for i := len(s.state.Reports) - 1; i >= start; i-- {
		d.Reports = append(d.Reports, s.state.Reports[i])
	}
	return d
}
func (s *Server) load() error {
	b, err := os.ReadFile(s.cfg.DataFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return err
	}
	if s.state.Nodes == nil {
		s.state.Nodes = map[string]Node{}
	}
	return nil
}
func (s *Server) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.DataFile), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.cfg.DataFile), ".monitor-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0600)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	_ = tmp.Sync()
	_ = tmp.Close()
	return os.Rename(name, s.cfg.DataFile)
}
func (s *Server) cleanupNoncesLocked() {
	now := time.Now()
	for k, v := range s.nonces {
		if v.Before(now) {
			delete(s.nonces, k)
		}
	}
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
