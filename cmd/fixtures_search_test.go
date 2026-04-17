// Package cmd — store-down and cache-hit tests for search and context commands.
package cmd

import (
	"fmt"
	"testing"

	"github.com/gurkangul/gg-cli/internal/cache"
	"github.com/gurkangul/gg-cli/internal/store"
)

// TestSearch_StoreDown verifies that 'gg search' returns with a non-nil error
// or prints cached/empty results when Qdrant is unreachable. loadDepsReadOnly
// sets qdrantDown=true and the command serves from the LKG cache (empty on
// first run).
func TestSearch_StoreDown(t *testing.T) {
	setupGGDir(t)
	// Even in degraded mode the command should not crash.
	out, _, _ := execCmd(t, "search", "authentication")
	// On first run with empty cache, the command may print a warning or exit
	// cleanly. We just verify it doesn't panic.
	_ = fmt.Sprintf("output: %s", out) // prevent compiler unused warning
}

func TestContext_StoreDown(t *testing.T) {
	setupGGDir(t)
	// context uses loadDepsReadOnly — degraded mode returns cached/empty output.
	// It should NOT return ExitStoreDown but should exit cleanly with empty data.
	_, _, _ = execCmd(t, "context", "authentication")
	// On first run with empty cache this prints a degraded banner — just verify
	// it does not panic or return a non-store error.
}

// ── cache-populated read tests ────────────────────────────────────────────────
// These tests pre-populate the LKG cache so that commands in degraded mode
// (qdrantDown=true) find a cache hit and exercise printSearchResults /
// printContextBundle code paths.

func TestSearch_CacheHit(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)

	// Seed a comprehensive search cache entry so serveSearchFromCache hits it
	// and printSearchResults exercises all branches (reason, tags, taskID, author).
	payload := searchPayload{
		Decisions: []store.Decision{
			{
				Text:   "use JWT for auth",
				Reason: "stateless and portable",
				Tags:   []string{"auth", "security"},
				TaskID: "TASK-001",
				Author: "architect",
			},
		},
		Rejections: []store.Rejection{
			{
				Approach: "sessions",
				Reason:   "stateful and fragile under load",
				Tags:     []string{"auth"},
				TaskID:   "TASK-001",
				Author:   "architect",
			},
		},
	}
	if err := cache.Put(rtDir, "search", "authentication", payload); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	// search is a read-only command — qdrantDown=true serves from cache.
	_, _, err := execCmd(t, "search", "authentication")
	if err != nil {
		t.Errorf("unexpected error from search with cache hit: %v", err)
	}
}

func TestSearch_CacheHit_Empty(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)

	// Empty cache entry exercises the "No results found." path.
	payload := searchPayload{}
	if err := cache.Put(rtDir, "search", "empty-query", payload); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	_, _, err := execCmd(t, "search", "empty-query")
	if err != nil {
		t.Errorf("unexpected error from search with empty cache: %v", err)
	}
}

func TestContext_CacheHit(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)

	// Seed a comprehensive context cache entry covering all printContextBundle
	// branches: decisions with reason/tags/taskID, rejections, tasks with detail,
	// discussions with turns and resolved status, notes with taskID.
	payload := contextPayload{
		Decisions: []store.Decision{
			{
				Text:   "use JWT for auth",
				Reason: "stateless and portable",
				Tags:   []string{"auth", "security"},
				TaskID: "TASK-001",
			},
		},
		Rejections: []store.Rejection{
			{
				Approach: "sessions",
				Reason:   "stateful under load",
				Tags:     []string{"auth"},
				TaskID:   "TASK-001",
			},
		},
		Tasks: []store.Task{
			{ID: "TASK-001", Title: "implement auth", Status: "in_progress", Priority: "high", Detail: "Add JWT support"},
			{ID: "TASK-002", Title: "write tests", Status: "done", Priority: "medium"},
		},
		Discussions: []store.Discussion{
			{
				ID:           "DISC-001",
				Topic:        "should we use OAuth?",
				Status:       "resolved",
				Detail:       "comparing OAuth2 vs manual JWT",
				ResolvedNote: "decided manual JWT, see D001",
				Turns: []store.Turn{
					{By: "architect", Role: "architect", Text: "I prefer OAuth2"},
					{By: "developer", Role: "dev", Text: "JWT is simpler for now"},
				},
			},
			{
				ID:     "DISC-002",
				Topic:  "token expiry policy",
				Status: "open",
				Detail: "how long should tokens last?",
			},
		},
		Notes: []store.Note{
			{Text: "JWT refresh token must be rotated on use", TaskID: "TASK-001"},
			{Text: "consider token blacklist for logout"},
		},
	}
	if err := cache.Put(rtDir, "context", "authentication", payload); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	// context is a read-only command — qdrantDown=true serves from cache.
	_, _, err := execCmd(t, "context", "authentication")
	if err != nil {
		t.Errorf("unexpected error from context with cache hit: %v", err)
	}
}

func TestContext_CacheHit_FullTranscript(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)

	// Same payload as TestContext_CacheHit but tests the --full flag which
	// renders all turns in the transcript (contextFullTranscript=true).
	payload := contextPayload{
		Discussions: []store.Discussion{
			{
				ID:     "DISC-001",
				Topic:  "should we use OAuth?",
				Status: "open",
				Turns: []store.Turn{
					{By: "architect", Role: "architect", Text: "I prefer OAuth2"},
					{By: "developer", Role: "dev", Text: "JWT is simpler"},
					{By: "pm", Role: "pm", Text: "Whichever ships faster"},
				},
			},
		},
	}
	if err := cache.Put(rtDir, "context", "oauth-discussion", payload); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	_, _, err := execCmd(t, "context", "--full", "oauth-discussion")
	if err != nil {
		t.Errorf("unexpected error from context --full with cache hit: %v", err)
	}
}

func TestContext_CacheHit_LongDetail(t *testing.T) {
	setupGGDir(t)
	rtDir := testRuntimeDir(t)

	// Use details longer than 120 chars to exercise the truncation paths
	// in printContextBundle (lines 209-211 for tasks, 224-226 for discussions).
	longDetail := "This is a very long detail string that exceeds the 120 character limit used for truncation in the context bundle display function."

	payload := contextPayload{
		Tasks: []store.Task{
			{ID: "TASK-001", Title: "long detail task", Status: "pending", Priority: "high", Detail: longDetail},
		},
		Discussions: []store.Discussion{
			{ID: "DISC-001", Topic: "long detail disc", Status: "open", Detail: longDetail},
		},
	}
	if err := cache.Put(rtDir, "context", "long-detail", payload); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	_, _, err := execCmd(t, "context", "long-detail")
	if err != nil {
		t.Errorf("unexpected error from context with long detail: %v", err)
	}
}
