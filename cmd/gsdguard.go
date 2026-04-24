package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/projectstate"
)

// gsdAgentPrefixes lists the agent identifier prefixes that are prohibited
// from owning task lifecycle transitions (done / ready-for-live). Matched
// case-insensitively via strings.HasPrefix so "claude-sonnet-4-6" and future
// variants are caught by the "claude-sonnet-" prefix.
var gsdLifecycleBlockedAgents = []string{"gsd", "claude-sonnet-"}

// isLifecycleBlockedAgent reports whether the given agent string matches any
// blocked prefix. Matching is case-insensitive.
func isLifecycleBlockedAgent(agent string) bool {
	lower := strings.ToLower(strings.TrimSpace(agent))
	for _, prefix := range gsdLifecycleBlockedAgents {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// checkAgentLifecycleGate enforces the Opus-owned lifecycle policy:
// GSD agents (GG_AGENT=gsd) and Sonnet models (GG_AGENT=claude-sonnet-*)
// must not call gg task done or gg task ready-for-live directly — only Opus
// (claude-code / claude-opus-*) owns these transitions.
//
// The check reads GG_AGENT (primary) and GG_ROLE (fallback). Returns nil
// when the agent is allowed, or an *ExitError with ExitVerifyFailed when
// blocked. Returns nil without checking when enforcement.Enabled() is false
// — callers must emit the bypass audit event themselves.
func checkAgentLifecycleGate(verb string) *ExitError {
	agent := os.Getenv("GG_AGENT")
	if agent == "" {
		agent = os.Getenv("GG_ROLE")
	}
	if agent == "" || !isLifecycleBlockedAgent(agent) {
		return nil
	}
	return &ExitError{
		Code: ExitVerifyFailed,
		Message: fmt.Sprintf(
			"lifecycle gate rejected '%s': GG_AGENT=%q is not permitted to own task lifecycle transitions.\n"+
				"Only Opus (claude-code / claude-opus-*) may call 'gg task %s'.\n"+
				"Set GG_ENFORCEMENT=off to bypass for this session (emergency use only).",
			verb, agent, verb),
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
	if !enforcement.Enabled() {
		emitGuardSkipEvent("gsd-guard", "")
		return nil // opt-out: set GG_ENFORCEMENT=off to bypass
	}
	// Load config — if .gg/ is not found or tracker.canonical != "gg", allow.
	if _, err := config.FindRoot(); err != nil {
		return nil // not a gg project, passthrough
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
				"BLOCKED by gg tracker guard: this project uses gg as its canonical tracker.\n"+
					"Do not call %s — use gg instead:\n\n"+
					"  gg task create \"<title>\" --priority <high|medium|low> --detail \"<details>\"\n\n"+
					"Set tracker.canonical in .gg/config.yaml to change this behaviour.\n",
				payload.ToolName)
			os.Exit(1)
		}
	}
	return nil
}

// emitGuardSkipEvent writes a single NDJSON line to stderr so operators
// and telemetry can audit how often a guard was asleep (enforcement off).
// Shape mirrors the pre-task-done gate's verify_failed event so agents
// parse both with one schema. As a side-effect, it also appends a
// BypassEntry to the project's state.json so future sessions can surface
// the bypass count (session-start) and list the full log
// (gg doctor --bypass-audit). Persistence is best-effort: any failure
// (runtime dir missing, disk full, …) is silently ignored — a gate already
// skipped is more important to observe than a missed audit line.
func emitGuardSkipEvent(gate, taskID string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	ev := struct {
		Event  string `json:"event"`
		Gate   string `json:"gate"`
		TaskID string `json:"task_id,omitempty"`
		TS     string `json:"ts"`
	}{
		Event:  "guard_skipped",
		Gate:   gate,
		TaskID: taskID,
		TS:     ts,
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
		_ = projectstate.AppendBypass(rt, gate, taskID, actor)
	}
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
