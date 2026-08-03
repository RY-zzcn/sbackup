package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"sbackup/internal/config"
	"sbackup/internal/executor"
	"sbackup/internal/store"
)

type Event struct {
	Title   string    `json:"title"`
	Body    string    `json:"body"`
	Status  string    `json:"status"`
	Node    string    `json:"node"`
	JobID   string    `json:"job_id"`
	JobName string    `json:"job_name"`
	RunID   string    `json:"run_id"`
	Time    time.Time `json:"time"`
}
type Service struct {
	Config *config.Config
	Store  *store.Store
	Client *http.Client
}

func (s *Service) SendIDs(ctx context.Context, ids []string, e Event) {
	for _, id := range ids {
		if err := s.Send(ctx, id, e); err != nil {
			_, _ = s.Store.Enqueue("notification", id, e)
		}
	}
}
func (s *Service) Send(ctx context.Context, id string, e Event) error {
	n, ok := s.Config.Notification(id)
	if !ok {
		return fmt.Errorf("通知不存在: %s", id)
	}
	if !n.Enabled {
		return nil
	}
	secret := map[string]string{}
	if n.SecretFile != "" {
		b, err := os.ReadFile(n.SecretFile)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &secret); err != nil {
			return err
		}
	}
	switch n.Type {
	case "smtp":
		return sendSMTP(*n, secret, e)
	case "telegram", "webhook", "gotify", "ntfy", "bark":
		return s.sendHTTP(ctx, *n, secret, e)
	default:
		return fmt.Errorf("不支持的通知类型 %s", n.Type)
	}
}
func sendSMTP(n config.Notification, secret map[string]string, e Event) error {
	host := str(n.Settings["host"])
	port := integer(n.Settings["port"], 587)
	from := str(n.Settings["from"])
	to := stringsList(n.Settings["to"])
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	msg := []byte("From: " + from + "\r\nTo: " + strings.Join(to, ",") + "\r\nSubject: " + e.Title + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + e.Body + "\r\n")
	auth := smtp.PlainAuth("", secret["username"], secret["password"], host)
	if str(n.Settings["security"]) == "tls" {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer c.Close()
		if err := c.Auth(auth); err != nil {
			return err
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		for _, x := range to {
			if err := c.Rcpt(x); err != nil {
				return err
			}
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		return w.Close()
	}
	return smtp.SendMail(addr, auth, from, to, msg)
}
func (s *Service) sendHTTP(ctx context.Context, n config.Notification, secret map[string]string, e Event) error {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	var url string
	var payload any
	e.Body = executor.Redact(e.Body)
	switch n.Type {
	case "telegram":
		url = "https://api.telegram.org/bot" + secret["bot_token"] + "/sendMessage"
		payload = map[string]any{"chat_id": secret["chat_id"], "text": e.Title + "\n" + e.Body}
	case "webhook":
		url = secret["url"]
		if url == "" {
			url = str(n.Settings["url"])
		}
		payload = e
	case "gotify":
		url = str(n.Settings["url"]) + "/message?token=" + secret["app_token"]
		payload = map[string]any{"title": e.Title, "message": e.Body, "priority": 8}
	case "ntfy":
		url = str(n.Settings["url"]) + "/" + secret["topic"]
		payload = map[string]any{"topic": secret["topic"], "title": e.Title, "message": e.Body}
	case "bark":
		url = str(n.Settings["url"]) + "/" + secret["device_key"]
		payload = map[string]any{"title": e.Title, "body": e.Body}
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("通知服务返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
func str(v any) string {
	if x, ok := v.(string); ok {
		return x
	}
	return fmt.Sprint(v)
}
func integer(v any, d int) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case string:
		i, _ := strconv.Atoi(x)
		if i > 0 {
			return i
		}
	}
	return d
}
func stringsList(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		r := []string{}
		for _, y := range x {
			r = append(r, str(y))
		}
		return r
	case string:
		return []string{x}
	}
	return nil
}
