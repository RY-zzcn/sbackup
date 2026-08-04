package executor

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeCommandRedactsSecretArguments(t *testing.T) {
	got := safeCommand("restic", []string{"--password-file", "/etc/sbackup/repo.pass", "--defaults-extra-file=/tmp/mysql.cnf", "--option", "token=abc", "backup"})
	if strings.Contains(got, "repo.pass") || strings.Contains(got, "mysql.cnf") || strings.Contains(got, "token=abc") {
		t.Fatalf("secret argument leaked: %s", got)
	}
	if !strings.Contains(got, "--password-file <redacted>") {
		t.Fatalf("password-file flag missing: %s", got)
	}
	if !strings.Contains(got, "--defaults-extra-file=<redacted>") {
		t.Fatalf("defaults-extra-file flag missing: %s", got)
	}
}

func TestRunWithStdoutStreamsOutputAndCapturesStderr(t *testing.T) {
	var stdout bytes.Buffer
	result := RunWithStdout(context.Background(), &Logger{Quiet: true}, "test", "/bin/sh", []string{"-c", "printf 'dump-data'; printf 'password=secret\\n' >&2"}, nil, nil, &stdout)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if stdout.String() != "dump-data" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if strings.Contains(result.Output, "secret") || !strings.Contains(result.Output, "password=<redacted>") {
		t.Fatalf("stderr was not redacted: %q", result.Output)
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
