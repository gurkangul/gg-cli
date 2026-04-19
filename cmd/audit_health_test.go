package cmd

import (
	"testing"
)

func TestFormatReopenRate(t *testing.T) {
	cases := []struct {
		rate, warn, freeze float64
		total              int
		wantLabel          string
		wantIcon           string
	}{
		{0.0, 0.20, 0.40, 0, "n/a (no data)", ""},
		{0.10, 0.20, 0.40, 10, "10%", "✓"},
		{0.25, 0.20, 0.40, 8, "25%", "⚠"},
		{0.45, 0.20, 0.40, 20, "45%", "⚠⚠"},
		{0.20, 0.20, 0.40, 5, "20%", "✓"}, // boundary: exactly at warn is still OK
	}
	for _, c := range cases {
		label, icon := formatReopenRate(c.rate, c.warn, c.freeze, c.total)
		if label != c.wantLabel || icon != c.wantIcon {
			t.Errorf("formatReopenRate(%.2f, %.2f, %.2f, %d) = (%q, %q), want (%q, %q)",
				c.rate, c.warn, c.freeze, c.total, label, icon, c.wantLabel, c.wantIcon)
		}
	}
}

func TestFormatSurfacePressure(t *testing.T) {
	cases := []struct {
		p95, threshold, n int
		wantIcon          string
	}{
		{0, 3, 0, ""},
		{2, 3, 5, "✓"},
		{3, 3, 5, "✓"}, // boundary: exactly at threshold is still OK
		{4, 3, 5, "⚠"},
		{10, 3, 12, "⚠"},
	}
	for _, c := range cases {
		_, icon := formatSurfacePressure(c.p95, c.threshold, c.n)
		if icon != c.wantIcon {
			t.Errorf("formatSurfacePressure(%d, %d, %d) icon = %q, want %q",
				c.p95, c.threshold, c.n, icon, c.wantIcon)
		}
	}
}

func TestAuditHealthRecommendation(t *testing.T) {
	cases := []struct {
		reopenRate float64
		p95        int
		want       string
	}{
		// all clear
		{0.05, 2, ""},
		// high surface only
		{0.05, 5, "centralize common state to reduce surface pressure"},
		// warn reopen only
		{0.25, 2, "stabilize before adding new features — reopen rate is elevated"},
		// warn reopen + high surface
		{0.25, 5, "stabilize before adding features; centralize common state to reduce surface pressure"},
		// freeze reopen only
		{0.45, 2, "freeze new features until reopen_rate < 20%"},
		// freeze reopen + high surface
		{0.45, 5, "freeze new features until reopen_rate < 20% and centralize common state to reduce surface pressure"},
	}
	for _, c := range cases {
		got := auditHealthRecommendation(c.reopenRate, c.p95, 0.20, 0.40, 3)
		if got != c.want {
			t.Errorf("auditHealthRecommendation(%.2f, %d): got %q, want %q", c.reopenRate, c.p95, got, c.want)
		}
	}
}

func TestPctStr(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{0.20, "20%"},
		{0.40, "40%"},
		{0.125, "13%"}, // rounds up
	}
	for _, c := range cases {
		got := pctStr(c.f)
		if got != c.want {
			t.Errorf("pctStr(%.3f) = %q, want %q", c.f, got, c.want)
		}
	}
}
