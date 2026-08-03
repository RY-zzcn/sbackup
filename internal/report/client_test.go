package report_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sbackup/internal/config"
	"sbackup/internal/monitor"
	"sbackup/internal/report"
	"sbackup/internal/store"
	"sbackup/pkg/reportprotocol"
)

func TestClientAndMonitorSigningProtocol(t *testing.T) {
	root := t.TempDir()
	serverState := filepath.Join(root, "monitor.json")
	monitorServer := monitor.New(monitor.Config{DataFile: serverState})
	sharedSecret := "0123456789abcdef0123456789abcdef"
	if err := monitorServer.AddNode("node-a", "Node A", sharedSecret); err != nil {
		t.Fatal(err)
	}
	handler := monitorServer.Routes()
	keyFile := filepath.Join(root, "monitor.key")
	if err := os.WriteFile(keyFile, []byte(sharedSecret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stateStore, err := store.Open(filepath.Join(root, "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stateStore.Close()
	cfg := config.Default()
	cfg.Monitoring = config.Monitoring{Enabled: true, Endpoint: "https://monitor.invalid/api/v1/report", NodeID: "node-a", KeyFile: keyFile, KeyVersion: 1, RequestTimeout: "5s"}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Result(), nil
	})}
	client := report.Client{Config: &cfg, Store: stateStore, HTTP: httpClient, Version: "test"}
	event := reportprotocol.Event{ProtocolVersion: 1, EventID: "event-client-monitor", EventType: "node.heartbeat", OccurredAt: time.Now().UTC(), Node: reportprotocol.Node{ID: "node-a"}}
	if err := client.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
