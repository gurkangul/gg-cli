package cmd

import "testing"

func TestHydrationRiskSuffix_LowRateWarnsSourceOfTruthRisk(t *testing.T) {
	got := hydrationRiskSuffix(5, 83)
	want := " ⚠ low; compact may be used as source-of-truth"
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
	want := " ⚠ drop-list muhtemelen agresif"
	if got != want {
		t.Fatalf("high refetch suffix = %q, want %q", got, want)
	}
}
