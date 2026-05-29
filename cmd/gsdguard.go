package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/store"
)

// lifecycleBlockedRoles lists implementation roles that cannot provide the
// independent review/verification evidence required for close/review lifecycle
// transitions. This is deliberately role-based, not agent-brand based: Codex,
// Claude, Cursor, GSD, or any future runtime may be the worker or the master
// depending on GG_ROLE.
var lifecycleBlockedRoles = []string{"developer", "worker", "implementer"}

// isLifecycleBlockedRole reports whether role is an implementation role that
// cannot supply independent review/verification evidence for its own work.
// Matching is case-insensitive.
func isLifecycleBlockedRole(role string) bool {
	lower := strings.ToLower(strings.TrimSpace(role))
	for _, blocked := range lifecycleBlockedRoles {
		if lower == blocked {
			return true
		}
	}
	return false
}

// checkAgentLifecycleGate enforces the role-owned evidence policy:
// implementation roles may mark work ready-for-live, but close/review
// transitions need independent review evidence (`gg task done` / `gg task
// review`). The policy is intentionally agent-agnostic: authority comes from
// GG_ROLE, not from whether the process is Codex, Claude, Cursor, GSD, or
// another runtime.
//
// The check reads GG_ROLE. Returns nil when allowed, or an *ExitError with
// ExitVerifyFailed when blocked. Returns nil without checking when
// enforcement.Enabled() is false — callers must emit the bypass audit event
// themselves.
func checkAgentLifecycleGate(verb string) *ExitError {
	if verb == "ready-for-live" {
		return nil
	}
	role := os.Getenv("GG_ROLE")
	if role == "" || !isLifecycleBlockedRole(role) {
		return nil
	}
	return &ExitError{
		Code: ExitVerifyFailed,
		Message: fmt.Sprintf(
			"missing independent review evidence for '%s': GG_ROLE=%q is an implementation role, so gg-managed close/review gates require a separate reviewer or verifier.\n"+
				"Record implementation evidence with 'gg task ready-for-live', then have a reviewer/verifier run 'gg task %s' with their own evidence.\n"+
				"The issue is not your native workflow; it is missing shared review evidence. Set GG_ENFORCEMENT=off to bypass for this session (emergency use only).",
			verb, role, verb),
	}
}

var gsdGuardCmd = &cobra.Command{
	Use:    "gsd-guard",
	Hidden: true, // internal — called only by the Claude Code PreToolUse hook
	Short:  "Block gsd_plan_* calls when tracker.canonical=gg",
	Long: `Claude Code PreToolUse hook guard. Reads the tool-call JSON from stdin,
checks whether tracker.canonical is set to "gg" in .gg/config.yaml, and
exits non-zero to block the call when both conditions are true.

Exit codes:
  0  allow (canonical != gg, or tool not in the forbidden list)
  1  block (canonical == gg and tool is a forbidden gsd_plan_* call)`,
	RunE: runGSDGuard,
}

func init() {
	rootCmd.AddCommand(gsdGuardCmd)
}

// forbiddenGSDTools is the set of MCP tool names that create GSD milestone
// state. Substring-matched against the incoming tool_name so both
// "mcp__gsd-workflow__gsd_plan_milestone" and bare "gsd_plan_milestone" hit.
var forbiddenGSDTools = []string{
	"gsd_plan_milestone",
	"gsd_plan_slice",
	"gsd_plan_task",
}

func runGSDGuard(_ *cobra.Command, _ []string) error {
	// AC-4: config.FindRoot() check MUST happen before the rationale gate so that
	// non-gg directories pass through without requiring GG_BYPASS_RATIONALE.
	// Prior ordering (rationale first, then FindRoot) wrongly rejected callers in
	// directories that have no .gg/ — they could never satisfy the gate.
	if _, err := config.FindRoot(); err != nil {
		return nil // not a gg project, passthrough
	}
	if !enforcement.Enabled() {
		if rej := emitGuardSkipEvent("gsd-guard", ""); rej != nil {
			return rej
		}
		return nil // opt-out: set GG_ENFORCEMENT=off + GG_BYPASS_RATIONALE to bypass
	}
	cfg, err := config.Load()
	if err != nil || strings.ToLower(cfg.Tracker.Canonical) != "gg" {
		return nil // not canonical, passthrough
	}

	// Read the PreToolUse JSON payload from stdin.
	raw, _ := io.ReadAll(os.Stdin)
	var payload struct {
		ToolName string `json:"tool_name"`
	}
	_ = json.Unmarshal(raw, &payload)

	toolName := strings.ToLower(payload.ToolName)
	for _, forbidden := range forbiddenGSDTools {
		if strings.Contains(toolName, forbidden) {
			fmt.Fprintf(os.Stdout,
				"BLOCKED by gg durable-memory guard: %s writes only to local GSD planner state, which future agents may not read.\n"+
					"Record durable shared work in gg instead:\n\n"+
					"  gg task create \"<title>\" --priority <high|medium|low> --detail \"<details>\"\n\n"+
					"This does not ban GSD as a native workflow; it prevents missing shared memory/evidence. Set tracker.canonical in .gg/config.yaml to change this behaviour.\n",
				payload.ToolName)
			os.Exit(1)
		}
	}
	return nil
}

// emitGuardSkipEvent validates the bypass rationale (TASK-317/318) and, if valid,
// writes a single NDJSON line to stderr so operators and telemetry can audit
// how often a guard was asleep (enforcement off). Returns an *ExitError
// (ExitVerifyFailed) when no rationale is provided or the rationale references
// the wrong task — callers must propagate this error to abort the command.
//
// Shape mirrors the pre-task-done gate's verify_failed event so agents parse
// both with one schema. As side-effects:
//   - When GG_BYPASS_RATIONALE is set but GG_BYPASS_RATIONALE_RECORD is not,
//     auto-writes a brain record tagged bypass-rationale so the bypass is
//     permanently searchable (AC-2). Best-effort — store errors are ignored.
//   - Appends a BypassEntry (including rationale + record ID) to state.json.
//     Persistence is best-effort: any failure is silently ignored — a gate
//     already skipped is more important to observe than a missed audit line.
func emitGuardSkipEvent(gate, taskID string) *ExitError {
	res, err := enforcement.CheckBypassRationale(taskID)
	if err != nil {
		return &ExitError{Code: ExitVerifyFailed, Message: err.Error()}
	}

	// AC-2: when only GG_BYPASS_RATIONALE is set (no record FK), auto-promote
	// by writing a brain record so the bypass is always queryable.
	recordID := res.RationaleRecordID
	if res.Rationale != "" && recordID == "" {
		recordID = tryAutoWriteBypassRecord(res.Rationale, taskID)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	ev := struct {
		Event             string `json:"event"`
		Gate              string `json:"gate"`
		TaskID            string `json:"task_id,omitempty"`
		Rationale         string `json:"rationale,omitempty"`
		RationaleTaskID   string `json:"rationale_task_id,omitempty"`
		RationaleRecordID string `json:"rationale_record_id,omitempty"`
		TS                string `json:"ts"`
	}{
		Event:             "guard_skipped",
		Gate:              gate,
		TaskID:            taskID,
		Rationale:         res.Rationale,
		RationaleTaskID:   res.RationaleTaskID,
		RationaleRecordID: recordID,
		TS:                ts,
	}
	if b, err := json.Marshal(ev); err == nil {
		fmt.Fprintln(os.Stderr, string(b))
	}

	// Best-effort durable audit record.
	actor := os.Getenv("GG_ROLE")
	if actor == "" {
		actor = os.Getenv("GG_AGENT")
	}
	if rt, rtErr := runtimeDirForBypass(); rtErr == nil {
		_ = projectstate.AppendBypass(rt, gate, taskID, actor, res.Rationale, res.RationaleTaskID, recordID)
	}
	return nil
}

// tryAutoWriteBypassRecord writes a brain decision record with the bypass
// rationale text and returns the new record's UUID. Used by emitGuardSkipEvent
// (AC-2) to ensure every text-only bypass (GG_BYPASS_RATIONALE without a
// GG_BYPASS_RATIONALE_RECORD) leaves a queryable artifact in the brain.
// Best-effort: returns "" on any error so the caller is never blocked.
func tryAutoWriteBypassRecord(rationale, taskID string) string {
	d, err := loadDeps(true)
	if err != nil {
		return ""
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	tags := []string{"bypass-rationale"}
	if taskID != "" {
		tags = append(tags, taskID)
	}

	embedText := "bypass rationale: " + rationale
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return ""
	}

	id := uuid.New().String()
	author := strings.TrimSpace(os.Getenv("GG_ROLE"))
	if author == "" {
		author = strings.TrimSpace(os.Getenv("GG_AGENT"))
	}
	dec := store.Decision{
		ID:     id,
		Text:   "bypass rationale: " + rationale,
		Reason: "auto-promoted from GG_BYPASS_RATIONALE by emitGuardSkipEvent (TASK-318)",
		Status: "active",
		Tags:   tags,
		TaskID: taskID,
		Author: author,
	}
	if err := d.store.AddDecision(ctx, dec, vector); err != nil {
		return ""
	}
	return id
}

// runtimeDirForBypass resolves ~/.gg/projects/<id>/ without requiring the
// full deps bundle. Returns an error when there's no gg project context,
// in which case bypass persistence quietly no-ops.
func runtimeDirForBypass() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.RuntimeDir()
}
