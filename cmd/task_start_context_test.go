package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// TASK-538: an errored related-item search and an empty brain both leave the
// slices empty. The renderer must not report them the same way — telling a
// claiming agent "no related items found" when the lookup actually failed reads
// as "nothing was ever decided here", which is the one wrong answer this block
// exists to prevent. These branches have no practical live trigger (they need a
// mid-flight store error while embedding succeeds), so they are covered here
// rather than by a repro script.
func TestRenderRelatedContext_DistinguishesFailureFromEmptiness(t *testing.T) {
	sample := []store.Decision{{
		ID:        "DEC-1",
		Text:      "example decision",
		CreatedAt: "2026-08-07T00:00:00Z",
	}}

	tests := []struct {
		name        string
		rc          *relatedContext
		wantContain string
		wantAbsent  string
	}{
		{
			name:        "nil context reports unavailable",
			rc:          nil,
			wantContain: "unavailable",
			wantAbsent:  "no related items found",
		},
		{
			name:        "empty and clean reports genuinely empty",
			rc:          &relatedContext{},
			wantContain: "no related items found",
			wantAbsent:  "unavailable",
		},
		{
			name:        "empty after a failed search must not claim emptiness",
			rc:          &relatedContext{searchFailed: true},
			wantContain: "search failed",
			wantAbsent:  "no related items found",
		},
		{
			name:        "partial results are flagged as incomplete",
			rc:          &relatedContext{decisions: sample, searchFailed: true},
			wantContain: "partial",
			wantAbsent:  "no related items found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderRelatedContext(&buf, tc.rc)
			got := buf.String()
			if !strings.Contains(got, tc.wantContain) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.wantContain, got)
			}
			if strings.Contains(got, tc.wantAbsent) {
				t.Errorf("expected output NOT to contain %q, got:\n%s", tc.wantAbsent, got)
			}
		})
	}
}

// items() feeds the telemetry delivered/degraded split, so a miscount there
// would misreport an outage as healthy delivery.
func TestRelatedContextItems_CountsAcrossAllSources(t *testing.T) {
	if got := (*relatedContext)(nil).items(); got != 0 {
		t.Errorf("nil relatedContext: got %d items, want 0", got)
	}
	if got := (&relatedContext{}).items(); got != 0 {
		t.Errorf("empty relatedContext: got %d items, want 0", got)
	}
	rc := &relatedContext{
		decisions:  make([]store.Decision, 3),
		rejections: make([]store.Rejection, 2),
		notes:      make([]store.Note, 1),
	}
	if got := rc.items(); got != 6 {
		t.Errorf("populated relatedContext: got %d items, want 6", got)
	}
}
