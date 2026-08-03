package database

import "testing"

func TestCredentialEscaping(t *testing.T) {
	if got := pgpassEscape(`a:b\c`); got != `a\:b\\c` {
		t.Fatalf("unexpected pgpass escaping: %q", got)
	}
	if got := mySQLConfigValue("a\\b\nc"); got != `a\\b\nc` {
		t.Fatalf("unexpected mysql escaping: %q", got)
	}
}
