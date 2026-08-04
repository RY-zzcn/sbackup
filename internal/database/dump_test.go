package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sbackup/internal/config"
	"sbackup/internal/executor"
)

func TestCredentialEscaping(t *testing.T) {
	if got := pgpassEscape(`a:b\c`); got != `a\:b\\c` {
		t.Fatalf("unexpected pgpass escaping: %q", got)
	}
	if got := mySQLConfigValue("a\\b\nc"); got != `a\\b\nc` {
		t.Fatalf("unexpected mysql escaping: %q", got)
	}
}

func TestDumpMySQLUsesExecutorAndRemovesPartialOutputOnCancellation(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "mysqldump")
	script := "#!/bin/sh\nprintf 'partial dump'\nprintf 'password=should-not-leak\\n' >&2\nsleep 30\n"
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	credentialFile := filepath.Join(root, "mysql.json")
	if err := os.WriteFile(credentialFile, []byte(`{"password":"top-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(root, "logs")
	logger, err := executor.NewLogger(logDir, "mysql-test")
	if err != nil {
		t.Fatal(err)
	}
	logger.Quiet = true
	defer logger.Close()
	cfg := &config.Config{Tools: config.Tools{MySQLDumpPath: fake}}
	db := config.Database{ID: "mysql", Type: "mysql", Host: "localhost", Database: "app", Username: "backup", CredentialFile: credentialFile}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out, err := dumpMySQL(ctx, cfg, db, root, logger)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if out != "" {
		t.Fatalf("unexpected output path: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "mysql.sql")); !os.IsNotExist(statErr) {
		t.Fatalf("partial output was not removed: %v", statErr)
	}
	if closeErr := logger.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	logData, readErr := os.ReadFile(logger.Path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	text := string(logData)
	if !strings.Contains(text, "执行 "+fake) || strings.Contains(text, "should-not-leak") || strings.Contains(text, "top-secret") {
		t.Fatalf("unexpected executor log: %s", text)
	}
}
