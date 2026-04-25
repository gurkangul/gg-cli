package cmd

import (
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

func TestDetectConflicts(t *testing.T) {
	tests := []struct {
		name      string
		bundle    contextBundle
		wantIDs   []string // IDs expected to appear in conflicts
		wantNone  bool     // expect zero conflicts
	}{
		{
			name: "mismatch fires — decision says done, task is pending",
			bundle: contextBundle{
				decisions: []store.Decision{
					{ID: "dec-1", Text: "TASK-100 is done and shipped", Reason: "verified in prod"},
				},
				tasks: []store.Task{
					{ID: "TASK-100", Title: "Deploy service", Status: "pending"},
				},
			},
			wantIDs: []string{"TASK-100"},
		},
		{
			name: "terminal match is silent — task is done and decision says done",
			bundle: contextBundle{
				decisions: []store.Decision{
					{ID: "dec-2", Text: "TASK-200 closed and fixed", Reason: ""},
				},
				tasks: []store.Task{
					{ID: "TASK-200", Title: "Some task", Status: "done"},
				},
			},
			wantNone: true,
		},
		{
			name: "ID not in bundle is silent — avoids false positives for external refs",
			bundle: contextBundle{
				decisions: []store.Decision{
					{ID: "dec-3", Text: "TASK-999 was fixed last quarter", Reason: ""},
				},
				tasks: []store.Task{
					{ID: "TASK-100", Title: "Unrelated task", Status: "pending"},
				},
			},
			wantNone: true,
		},
		{
			name: "note with closure token and non-terminal task fires",
			bundle: contextBundle{
				notes: []store.Note{
					{ID: "n-1", Text: "TASK-300 approved by master"},
				},
				tasks: []store.Task{
					{ID: "TASK-300", Title: "Waiting task", Status: "in_progress"},
				},
			},
			wantIDs: []string{"TASK-300"},
		},
		{
			name: "rejection with closure token and blocked task fires",
			bundle: contextBundle{
				rejections: []store.Rejection{
					{ID: "r-1", Approach: "TASK-400 closed via bypass", Reason: "workaround"},
				},
				tasks: []store.Task{
					{ID: "TASK-400", Title: "Blocked thing", Status: "blocked"},
				},
			},
			wantIDs: []string{"TASK-400"},
		},
		{
			name: "multiple IDs in one document — only mismatched ones fire",
			bundle: contextBundle{
				decisions: []store.Decision{
					{ID: "dec-4", Text: "TASK-500 done, TASK-501 still pending work"},
				},
				tasks: []store.Task{
					{ID: "TASK-500", Title: "Already done", Status: "done"},
					{ID: "TASK-501", Title: "Not done", Status: "pending"},
				},
			},
			// "done" token fires for TASK-501 (non-terminal) but not TASK-500 (terminal).
			wantIDs: []string{"TASK-501"},
		},
		{
			name: "no closure token — no conflict even if task is non-terminal",
			bundle: contextBundle{
				decisions: []store.Decision{
					{ID: "dec-5", Text: "TASK-600 is being worked on now"},
				},
				tasks: []store.Task{
					{ID: "TASK-600", Title: "Work in progress", Status: "in_progress"},
				},
			},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectConflicts(tt.bundle)

			if tt.wantNone {
				if len(got) != 0 {
					t.Errorf("expected no conflicts, got %d: %+v", len(got), got)
				}
				return
			}

			gotIDs := make(map[string]bool, len(got))
			for _, c := range got {
				gotIDs[c.ID] = true
				if c.Authoritative != "live_status" {
					t.Errorf("conflict %s: Authoritative = %q, want \"live_status\"", c.ID, c.Authoritative)
				}
				if c.LiveStatus == "" {
					t.Errorf("conflict %s: LiveStatus is empty", c.ID)
				}
				if c.ClosureToken == "" {
					t.Errorf("conflict %s: ClosureToken is empty", c.ID)
				}
			}

			for _, id := range tt.wantIDs {
				if !gotIDs[id] {
					t.Errorf("expected conflict for %s not found in %+v", id, got)
				}
			}
		})
	}
}
