package cmd

import (
	"testing"
	"time"

	"github.com/gurkangul/gg-cli/internal/store"
)

// trustTier is time-dependent, so it is the one part of TASK-519 that cannot be
// proven by a live run without fabricating clock state — the ledger it ships
// against happens to contain no evidence-bearing decision older than the aging
// threshold. A table over a fixed "now" is the honest way to pin the boundaries.
func TestTrustTier(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	cases := []struct {
		name string
		dec  store.Decision
		want string
	}{
		{
			name: "no evidence is unverified regardless of age",
			dec:  store.Decision{CreatedAt: at(time.Hour)},
			want: trustUnverified,
		},
		{
			name: "fresh evidence is verified",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: at(24 * time.Hour)},
			want: trustVerified,
		},
		{
			name: "just inside the aging boundary is still verified",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: at(trustAgingAfter - time.Hour)},
			want: trustVerified,
		},
		{
			name: "at the aging boundary it ages",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: at(trustAgingAfter)},
			want: trustAging,
		},
		{
			name: "just inside the stale boundary is still aging",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: at(trustStaleAfter - time.Hour)},
			want: trustAging,
		},
		{
			name: "at the stale boundary it goes stale",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: at(trustStaleAfter)},
			want: trustStale,
		},
		{
			name: "pinned decisions never decay",
			dec:  store.Decision{Evidence: "ran the smoke", Pinned: true, CreatedAt: at(2 * trustStaleAfter)},
			want: trustVerified,
		},
		{
			name: "policy-tagged decisions never decay",
			dec:  store.Decision{Evidence: "ran the smoke", Tags: []string{"Policy"}, CreatedAt: at(2 * trustStaleAfter)},
			want: trustVerified,
		},
		{
			name: "an exempt decision with no evidence is still unverified",
			dec:  store.Decision{Pinned: true, CreatedAt: at(2 * trustStaleAfter)},
			want: trustUnverified,
		},
		{
			name: "an unparseable date is not treated as stale",
			dec:  store.Decision{Evidence: "ran the smoke", CreatedAt: "not-a-date"},
			want: trustVerified,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trustTier(tc.dec, now); got != tc.want {
				t.Errorf("trustTier = %q, want %q", got, tc.want)
			}
		})
	}
}

// The label must always state the tier — "verified" and "verified a long time
// ago" reading identically is the exact failure this change exists to fix.
func TestTrustLabelDistinguishesTiers(t *testing.T) {
	seen := map[string]bool{}
	for _, tier := range []string{trustVerified, trustAging, trustStale, trustUnverified} {
		label := trustLabel(tier)
		if label == "" {
			t.Fatalf("tier %q rendered an empty label", tier)
		}
		if seen[label] {
			t.Errorf("tier %q reuses label %q — tiers must be distinguishable", tier, label)
		}
		seen[label] = true
	}
}
