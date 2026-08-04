package tui

import (
	"strings"
	"testing"

	"sbackup/internal/config"
)

func TestValidClock(t *testing.T) {
	for _, value := range []string{"00:00", "02:30", "23:59"} {
		if !validClock(value) {
			t.Fatalf("valid clock rejected: %s", value)
		}
	}
	for _, value := range []string{"2:30", "24:00", "12:60", "nope"} {
		if validClock(value) {
			t.Fatalf("invalid clock accepted: %s", value)
		}
	}
}

func TestClocksToExpressions(t *testing.T) {
	got, err := clocksToExpressions("02:30,14:30,02:30")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "*-*-* 02:30:00" || got[1] != "*-*-* 14:30:00" {
		t.Fatalf("unexpected expressions: %#v", got)
	}
	if _, err := clocksToExpressions("25:00"); err == nil {
		t.Fatal("invalid clock accepted")
	}
}

func TestScheduleSummary(t *testing.T) {
	if got := scheduleSummary(config.Schedule{Enabled: true, Type: "interval", Expression: "6h"}); got != "每隔 6h" {
		t.Fatalf("unexpected interval summary: %s", got)
	}
	got := scheduleSummary(config.Schedule{Enabled: true, Type: "calendar", Expression: "*-*-* 02:30:00;*-*-* 14:30:00"})
	if got != "每天 02:30 / 14:30" {
		t.Fatalf("unexpected calendar summary: %s", got)
	}
}

func TestDisplayWidth(t *testing.T) {
	if got := displayWidth("SBackup 备份"); got != 12 {
		t.Fatalf("unexpected display width: %d", got)
	}
}

func TestRetentionSummary(t *testing.T) {
	got := retentionSummary(config.Retention{KeepLast: 3, KeepDaily: 14, KeepWeekly: 8})
	for _, value := range []string{"最近 3", "每天 14", "每周 8"} {
		if !strings.Contains(got, value) {
			t.Fatalf("summary %q missing %q", got, value)
		}
	}
}
