package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "database.json")
	if err := writeJSONSecret(path, map[string]string{"password": "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected permissions: %o", info.Mode().Perm())
	}
	var got map[string]string
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &got); err != nil || got["password"] != "secret" {
		t.Fatalf("unexpected secret: %#v err=%v", got, err)
	}
	if err := writeJSONSecret(path, map[string]string{"password": "changed"}); err == nil {
		t.Fatal("existing secret was overwritten")
	}
}
