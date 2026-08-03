package executor

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeCommandRedactsSecretArguments(t *testing.T) {
	got := safeCommand("restic", []string{"--password-file", "/etc/sbackup/repo.pass", "--option", "token=abc", "backup"})
	if strings.Contains(got, "repo.pass") || strings.Contains(got, "token=abc") {
		t.Fatalf("secret argument leaked: %s", got)
	}
	if !strings.Contains(got, "--password-file <redacted>") {
		t.Fatalf("password-file flag missing: %s", got)
	}
}

func TestRunSerializesLineCallback(t *testing.T) {
	var active atomic.Int32
	var concurrent atomic.Bool
	result := Run(context.Background(), &Logger{Quiet: true}, "test", "/bin/sh", []string{"-c", "printf 'out\\n'; printf 'err\\n' >&2"}, nil, func(string) {
		if active.Add(1) != 1 {
			concurrent.Store(true)
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if concurrent.Load() {
		t.Fatal("line callback was entered concurrently")
	}
}
