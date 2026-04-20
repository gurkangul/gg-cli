package cmd

import (
	"testing"
	"time"
)

func TestParseBypassWindow_Relative(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"90m", 90 * time.Minute},
		{"", 7 * 24 * time.Hour}, // default window
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseBypassWindow(c.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Allow a generous slop — parseBypassWindow reads `time.Now()` so
			// a microsecond skew is expected. Compare the delta.
			want := time.Now().Add(-c.want)
			delta := got.Sub(want)
			if delta < -2*time.Second || delta > 2*time.Second {
				t.Fatalf("window %q: got %v, want ~%v (delta %v)", c.in, got, want, delta)
			}
		})
	}
}

func TestParseBypassWindow_RFC3339Passthrough(t *testing.T) {
	input := "2026-04-19T00:00:00Z"
	got, err := parseBypassWindow(input)
	if err != nil {
		t.Fatalf("rfc3339 should parse, got %v", err)
	}
	want, _ := time.Parse(time.RFC3339, input)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseBypassWindow_RejectsGarbage(t *testing.T) {
	for _, in := range []string{"abc", "7x", "days"} {
		if _, err := parseBypassWindow(in); err == nil {
			t.Fatalf("expected error for %q, got nil", in)
		}
	}
}
