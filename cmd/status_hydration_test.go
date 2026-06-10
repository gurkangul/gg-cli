package cmd

import "testing"

// hydrationRiskSuffix now takes the DISCRETIONARY agent re-fetch count as its
// first arg (TASK-491). Gate-mandated --full reads and human full-reads are
// excluded by the caller, so these cases exercise the honest discretionary rate.

func TestHydrationRiskSuffix_ZeroDiscretionaryReportsCompactTrusted(t *testing.T) {
	got := hydrationRiskSuffix(0, 7)
	want := " ⚠ low discretionary re-fetch; compact trusted as source-of-truth"
	if got != want {
		t.Fatalf("hydrationRiskSuffix = %q, want %q", got, want)
	}
}

func TestHydrationRiskSuffix_LowDiscretionaryReportsCompactTrusted(t *testing.T) {
	got := hydrationRiskSuffix(5, 83)
	want := " ⚠ low discretionary re-fetch; compact trusted as source-of-truth"
	if got != want {
		t.Fatalf("hydrationRiskSuffix = %q, want %q", got, want)
	}
}

func TestHydrationRiskSuffix_MediumRateWarns(t *testing.T) {
	got := hydrationRiskSuffix(15, 100)
	want := " ⚠ moderate; hydrate before action"
	if got != want {
		t.Fatalf("hydrationRiskSuffix = %q, want %q", got, want)
	}
}

func TestHydrationRiskSuffix_NormalAndHighRefetch(t *testing.T) {
	if got := hydrationRiskSuffix(25, 100); got != "" {
		t.Fatalf("normal hydration suffix = %q, want empty", got)
	}
	got := hydrationRiskSuffix(60, 100)
	want := " ⚠ drop-list muhtemelen agresif (discretionary re-fetch)"
	if got != want {
		t.Fatalf("high refetch suffix = %q, want %q", got, want)
	}
}

// TestHydrationRiskSuffix_MandatedDoesNotWarn is the load-bearing TASK-491 case:
// a high TOTAL re-fetch rate that is entirely gate-mandated (discretionary=0)
// must NOT trigger the drop-list warning. The caller passes discretionary-only,
// so a fixture of 80 mandated + 0 discretionary against 100 compact calls
// surfaces here as discretionary=0 → "compact trusted", never "agresif".
func TestHydrationRiskSuffix_AllMandatedNeverFiresAgresif(t *testing.T) {
	got := hydrationRiskSuffix(0, 100) // 0 discretionary despite many mandated reads
	if got == " ⚠ drop-list muhtemelen agresif (discretionary re-fetch)" {
		t.Fatalf("all-mandated hydration must not warn agresif, got %q", got)
	}
}
