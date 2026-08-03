package executor

import (
	"strings"
	"testing"
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
