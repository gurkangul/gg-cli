package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/brain"
)

func TestAC1AC2AC3AC4AC5NativeWorkflowMemorySyncSmoke(t *testing.T) {
	requireOllamaOrSkip(t)
	ggDir := setupGGDir(t)
	t.Setenv("GG_AGENT", "native-smoke")
	t.Setenv("GG_ROLE", "implementer")

	const token = "bmadsync462"
	const gsdToken = "gsdmirror462"
	const unmirroredToken = "unmirrored462"

	if _, _, err := execCmd(t,
		"task", "create", "BMAD memory sync smoke "+token,
		"--detail", "AC-1 simulated BMAD/subagent round durable work item "+token,
		"--priority", "medium",
		"--requester", "agent",
		"--tags", "bmad,smoke,TASK-462",
	); err != nil {
		t.Fatalf("AC-1 task create: %v", err)
	}
	taskID := nativeSmokeTaskID(t, ggDir, token)

	if _, _, err := execCmd(t,
		"record", "AC-1 BMAD round decision persisted "+token,
		"--task", taskID,
		"--reason", "Simulated subagent output accepted; native workflow stays outside gg, durable decision goes into gg.",
		"--tags", "bmad,smoke,TASK-462",
	); err != nil {
		t.Fatalf("AC-1 decision record: %v", err)
	}

	if _, _, err := execCmd(t,
		"record", "AC-1 BMAD round rejected execution controller "+token,
		"--decision-status", "rejected",
		"--task", taskID,
		"--reason", "Native workflow is free; gg stores shared memory rather than owning local execution.",
		"--tags", "bmad,smoke,TASK-462",
	); err != nil {
		t.Fatalf("AC-1 rejection record: %v", err)
	}

	if _, _, err := execCmd(t,
		"tell", "reviewer",
		"AC-1 handoff "+token+": Evidence: commands=simulated native round; live=not applicable; gaps=none",
		"--from", "implementer",
		"--task", taskID,
	); err != nil {
		t.Fatalf("AC-1 handoff tell: %v", err)
	}

	gsdDir := filepath.Join(filepath.Dir(ggDir), ".gsd")
	if err := os.MkdirAll(gsdDir, 0o755); err != nil {
		t.Fatalf("mkdir .gsd scratchpad: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gsdDir, "scratchpad.md"), []byte("local-only GSD note "+unmirroredToken+"\n"), 0o644); err != nil {
		t.Fatalf("write .gsd scratchpad: %v", err)
	}

	if _, _, err := execCmd(t,
		"record", "AC-2 GSD scratchpad outcome mirrored "+gsdToken,
		"--task", taskID,
		"--reason", "The .gsd scratchpad remains local and non-canonical; this record is the shared durable mirror.",
		"--tags", "gsd,smoke,TASK-462",
	); err != nil {
		t.Fatalf("AC-2 GSD mirror record: %v", err)
	}

	assertNativeSmokeJSONL(t, ggDir, token, gsdToken, taskID)

	searchOut := captureStdout(t, func() {
		if _, _, err := execCmd(t, "search", token, "--compact"); err != nil {
			t.Fatalf("AC-4 gg search %s: %v", token, err)
		}
	})
	for _, want := range []string{token, "AC-1 BMAD round decision", "BMAD memory sync smoke"} {
		if !strings.Contains(searchOut, want) {
			t.Fatalf("AC-4 search output missing %q:\n%s", want, searchOut)
		}
	}

	gsdSearchOut := captureStdout(t, func() {
		if _, _, err := execCmd(t, "search", gsdToken, "--compact"); err != nil {
			t.Fatalf("AC-4 gg search %s: %v", gsdToken, err)
		}
	})
	if !strings.Contains(gsdSearchOut, gsdToken) {
		t.Fatalf("AC-2 mirrored GSD record not surfaced by search:\n%s", gsdSearchOut)
	}

	rejectionSearchOut := captureStdout(t, func() {
		if _, _, err := execCmd(t, "search", "rejected execution controller "+token, "--compact"); err != nil {
			t.Fatalf("AC-4 gg search rejection %s: %v", token, err)
		}
	})
	for _, want := range []string{token, "AC-1 BMAD round rejected execution controller"} {
		if !strings.Contains(rejectionSearchOut, want) {
			t.Fatalf("AC-4 search output missing rejected approach %q:\n%s", want, rejectionSearchOut)
		}
	}

	handoffSearchOut := captureStdout(t, func() {
		if _, _, err := execCmd(t, "search", "handoff "+token, "--compact"); err != nil {
			t.Fatalf("AC-4 gg search handoff %s: %v", token, err)
		}
	})
	for _, want := range []string{token, "AC-1 handoff"} {
		if !strings.Contains(handoffSearchOut, want) {
			t.Fatalf("AC-4 search output missing handoff message %q:\n%s", want, handoffSearchOut)
		}
	}

	unmirroredOut := captureStdout(t, func() {
		if _, _, err := execCmd(t, "search", unmirroredToken, "--compact"); err != nil {
			t.Fatalf("AC-2 gg search %s: %v", unmirroredToken, err)
		}
	})
	if strings.Contains(unmirroredOut, unmirroredToken) {
		t.Fatalf("AC-2 .gsd scratchpad leaked into gg search; .gsd must remain non-canonical:\n%s", unmirroredOut)
	}

	// AC-4: durable memory must be rehydratable. The queryability contract (task +
	// decisions + rejections + handoff) is fully verified by the `gg search`
	// assertions above, which serve from the committed JSONL source of truth.
	//
	// `gg context --for-task` additionally pulls the task by id. On this fresh
	// fixture the vector collections are not materialized yet (.gg/vectorstore.db is
	// a derived, gitignored artifact built by `gg reembed`), so the by-id GetTask
	// surfaces a collection-not-found — which now transparently falls back to the
	// committed JSONL brain (VEC-1) instead of erroring. Assert the command succeeds
	// and rehydrates the task from JSONL.
	// Success is only reachable via the JSONL fallback: the vector collections are
	// not materialized on this fresh fixture, so the by-id GetTask returns
	// collection-not-found, which VEC-1 transparently routes to the JSONL brain.
	// (The compact context renderer writes to os.Stdout directly, bypassing the
	// test capture buffer, so we assert on the exit contract, not stdout text.)
	if _, _, ctxErr := execCmd(t, "context", "--for-task", taskID, "--compact"); ctxErr != nil {
		t.Fatalf("AC-4: context --for-task should rehydrate from the JSONL brain on a fresh (un-reembedded) fixture, got error: %v", ctxErr)
	}
}

func nativeSmokeTaskID(t *testing.T, ggDir, token string) string {
	t.Helper()
	entries, err := brain.SearchByText(ggDir, "tasks", token)
	if err != nil {
		t.Fatalf("search task JSONL: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("task JSONL matches = %d, want 1", len(entries))
	}
	task := taskFromJSONLEntry(entries[0])
	if task.ID == "" {
		t.Fatalf("task JSONL missing ID: %#v", entries[0].Payload)
	}
	return task.ID
}

func assertNativeSmokeJSONL(t *testing.T, ggDir, token, gsdToken, taskID string) {
	t.Helper()
	for _, tc := range []struct {
		kind  string
		query string
	}{
		{kind: "tasks", query: token},
		{kind: "decisions", query: token},
		{kind: "rejections", query: token},
		{kind: "messages", query: token},
		{kind: "decisions", query: gsdToken},
	} {
		entries, err := brain.SearchByText(ggDir, tc.kind, tc.query)
		if err != nil {
			t.Fatalf("AC-3 search %s JSONL for %q: %v", tc.kind, tc.query, err)
		}
		if len(entries) == 0 {
			t.Fatalf("AC-3 expected %s JSONL to contain %q", tc.kind, tc.query)
		}
	}

	messages, err := brain.ReadAll(ggDir, "messages")
	if err != nil {
		t.Fatalf("read messages JSONL: %v", err)
	}
	for _, entry := range messages {
		if stringPayload(entry.Payload, "task_id", "") == taskID {
			return
		}
	}
	t.Fatalf("AC-1 handoff message was not linked to task %s", taskID)
}
