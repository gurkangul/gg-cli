package cmd

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// relevanceBundle builds a bundle with two active decisions on different topics,
// so a topic query should reorder them. One decision is architecture-tagged to
// exercise force-inject.
func relevanceBundle() contextBundle {
	return contextBundle{
		decisions: []store.Decision{
			{ID: "dec-docker", Text: "Remove Docker backend; embedded SQLite is the default", Status: "active", Tags: []string{"backend"}, CreatedAt: "2026-04-01T00:00:00Z"},
			{ID: "dec-inbox", Text: "Inbox routing uses role-scoped peek semantics", Status: "active", Tags: []string{"inbox"}, CreatedAt: "2026-04-01T00:00:00Z"},
			{ID: "dec-arch", Text: "Core stays CGO-free and daemonless", Status: "active", Tags: []string{"architecture"}, CreatedAt: "2026-01-01T00:00:00Z"},
		},
	}
}

func TestBuildTieredItems_TopicOrdersWithinTier(t *testing.T) {
	bundle := relevanceBundle()

	dockerItems := buildTieredItems("docker backend removal", bundle)
	inboxItems := buildTieredItems("inbox", bundle)

	firstLine := func(items []contextTieredItem) string {
		if len(items) == 0 {
			return ""
		}
		return items[0].line
	}

	dockerTop := firstLine(dockerItems)
	inboxTop := firstLine(inboxItems)

	if !strings.Contains(dockerTop, "Docker") {
		t.Errorf("docker query: top item = %q, want the Docker decision first", dockerTop)
	}
	if !strings.Contains(inboxTop, "Inbox") {
		t.Errorf("inbox query: top item = %q, want the Inbox decision first", inboxTop)
	}
	if dockerTop == inboxTop {
		t.Errorf("topic ordering not working: both queries surfaced the same top item %q", dockerTop)
	}
}

func TestBuildTieredItems_Deterministic(t *testing.T) {
	bundle := relevanceBundle()
	a := buildTieredItems("docker backend removal", bundle)
	b := buildTieredItems("docker backend removal", bundle)
	if len(a) != len(b) {
		t.Fatalf("nondeterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].line != b[i].line {
			t.Errorf("nondeterministic order at %d: %q vs %q", i, a[i].line, b[i].line)
		}
	}
}

func TestRenderContextBudget_ArchitectureNeverDropped(t *testing.T) {
	bundle := relevanceBundle()
	// budget = 1 token: everything non-forced should be dropped, but the
	// architecture-tagged decision must survive (force-inject).
	out := renderBudgetToString("docker backend removal", bundle, 1)
	if !strings.Contains(out, "CGO-free") {
		t.Errorf("architecture-tagged decision was dropped under tight budget:\n%s", out)
	}
}

func TestRenderContextBudget_CriticalTaskNeverDropped(t *testing.T) {
	bundle := contextBundle{
		tasks: []store.Task{
			{ID: "TASK-900", Title: "Pad budget with filler text so the critical task would not fit", Status: "pending", Priority: "low"},
			{ID: "TASK-901", Title: "Restore data loss on crash", Status: "pending", Priority: "critical"},
		},
	}
	out := renderBudgetToString("crash recovery", bundle, 1)
	if !strings.Contains(out, "TASK-901") {
		t.Errorf("critical-priority task was dropped under tight budget:\n%s", out)
	}
}

func TestScoreDecision_SupersededPenalized(t *testing.T) {
	rc := newRelevanceContext("docker backend")
	active := store.Decision{Text: "docker backend removed", Status: "active"}
	superseded := store.Decision{Text: "docker backend removed", Status: "superseded"}
	as, _ := rc.scoreDecision(active)
	ss, _ := rc.scoreDecision(superseded)
	if as <= ss {
		t.Errorf("active decision score %d should exceed superseded %d", as, ss)
	}
}

func TestScoreTask_PriorityOrder(t *testing.T) {
	rc := newRelevanceContext("login")
	crit, _ := rc.scoreTask(store.Task{Title: "login", Priority: "critical"})
	high, _ := rc.scoreTask(store.Task{Title: "login", Priority: "high"})
	low, _ := rc.scoreTask(store.Task{Title: "login", Priority: "low"})
	if !(crit > high && high > low) {
		t.Errorf("priority order broken: critical=%d high=%d low=%d", crit, high, low)
	}
}
