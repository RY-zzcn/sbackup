package monitor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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

func TestCorruptStatePreventsMutationAndServe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{DataFile: path}); err == nil {
		t.Fatal("corrupt state was silently accepted")
	}
}

func TestConcurrentReplayAcceptedOnce(t *testing.T) {
	s, err := New(Config{DataFile: filepath.Join(t.TempDir(), "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef"
	if err := s.AddNode("node", "Node", key); err != nil {
		t.Fatal(err)
	}
	e := reportprotocol.Event{ProtocolVersion: 1, EventID: "event-concurrent", EventType: "node.heartbeat", OccurredAt: time.Now().UTC(), Node: reportprotocol.Node{ID: "node"}}
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			s.Routes().ServeHTTP(w, signedRequest(t, key, e, "same-nonce"))
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	ok, replay := 0, 0
	for code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusUnauthorized:
			replay++
		}
	}
	if ok != 1 || replay != 1 {
		t.Fatalf("unexpected response counts: ok=%d replay=%d", ok, replay)
	}
}

func TestAddNodeRollsBackOnSaveFailure(t *testing.T) {
	root := t.TempDir()
	s, err := New(Config{DataFile: filepath.Join(root, "missing", "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "missing"), []byte("blocks directory"), 0600); err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef"
	if err := s.AddNode("node", "Node", key); err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, ok := s.state.Nodes["node"]; ok {
		t.Fatal("failed node mutation remained in memory")
	}
}

func TestSignedReportAndReplay(t *testing.T) {
	s, err := New(Config{DataFile: filepath.Join(t.TempDir(), "state.json")})
	if err != nil {
		t.Fatal(err)
	}
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
