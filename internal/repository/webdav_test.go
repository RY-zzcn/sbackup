package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureWebDAV(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "rclone")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nread secret\nprintf 'obscured-value\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(root, "config", "rclone.conf")
	if err := ConfigureWebDAV(fake, conf, "cloud", "https://dav.example/", "other", "alice", "top-secret"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "top-secret") || !strings.Contains(text, "pass = obscured-value") {
		t.Fatalf("unexpected config: %s", text)
	}
	if info, _ := os.Stat(conf); info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if err := ConfigureWebDAV(fake, conf, "cloud", "https://dav.example/", "other", "alice", "another-secret"); err == nil {
		t.Fatal("existing remote was overwritten")
	}
}

func TestConfigureWebDAVRejectsUnsafeInput(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "rclone")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'value\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://dav.example/", "https://dav.example/\nuser = attacker"} {
		if err := ConfigureWebDAV(fake, filepath.Join(root, "rclone.conf"), "cloud", endpoint, "other", "alice", "secret"); err == nil {
			t.Fatalf("unsafe endpoint accepted: %q", endpoint)
		}
	}
}
