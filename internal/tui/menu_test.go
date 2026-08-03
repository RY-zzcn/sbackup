package tui

import "testing"

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
