package monitor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sbackup/pkg/reportprotocol"
)

func signedRequest(t *testing.T, key string, event reportprotocol.Event, nonce string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(event)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	digest := sha256.Sum256(body)
	keyDigest := sha256.Sum256([]byte(key))
	canonical := "POST\n/api/v1/report\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, keyDigest[:])
	_, _ = mac.Write([]byte(canonical))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/report", bytes.NewReader(body))
	r.Header.Set("X-SBackup-Node", "node")
	r.Header.Set("X-SBackup-Key-Version", "1")
	r.Header.Set("X-SBackup-Timestamp", ts)
	r.Header.Set("X-SBackup-Nonce", nonce)
	r.Header.Set("X-SBackup-Signature", "v1="+hex.EncodeToString(mac.Sum(nil)))
	return r
}

func TestSignedReportAndReplay(t *testing.T) {
	s := New(Config{DataFile: filepath.Join(t.TempDir(), "state.json")})
	key := "0123456789abcdef0123456789abcdef"
	if err := s.AddNode("node", "Node", key); err != nil {
		t.Fatal(err)
	}
	e := reportprotocol.Event{ProtocolVersion: 1, EventID: "event-1", EventType: "node.heartbeat", OccurredAt: time.Now().UTC(), Node: reportprotocol.Node{ID: "node"}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, signedRequest(t, key, e, "nonce"))
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, signedRequest(t, key, e, "nonce"))
	if w.Code != 401 {
		t.Fatalf("replay code=%d", w.Code)
	}
}
