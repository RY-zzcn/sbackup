package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestConfigureWebDAVSerializesConcurrentUpdates(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "rclone")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nread secret\nsleep 0.05\nprintf 'obscured-%s\\n' \"$secret\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(root, "config", "rclone.conf")
	const updates = 8
	errCh := make(chan error, updates)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < updates; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			name := fmt.Sprintf("cloud-%d", i)
			errCh <- ConfigureWebDAV(fake, conf, name, "https://dav.example/", "other", "alice", name)
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for i := 0; i < updates; i++ {
		name := fmt.Sprintf("cloud-%d", i)
		if strings.Count(text, "["+name+"]") != 1 {
			t.Fatalf("remote %s missing or duplicated in config: %s", name, text)
		}
	}
	backupInfo, err := os.Stat(conf + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if backupInfo.Mode().Perm() != 0600 {
		t.Fatalf("backup mode=%o", backupInfo.Mode().Perm())
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
