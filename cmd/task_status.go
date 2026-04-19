package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/hooks"
	"github.com/gurkangul/gg-cli/internal/store"
)

// taskDoneVerifier is the role flag for the verifier-separation gate in
// `gg task done`. Read only when .gg/config.yaml has
// tasks.verifier_separation: true — otherwise ignored for back-compat.
var taskDoneVerifier string

var taskDoneCmd = &cobra.Command{
	Use:   `done TASK-ID "summary"`,
	Short: "Mark a task done — include a one-sentence summary of what was accomplished",
	Long: `Close a task and record a completion summary in the shared brain.

WHEN TO USE: you have finished the work described in the task. The summary is stored
and surfaced in 'gg status' and 'gg search' — write it for the next agent that reads it.

VERIFY GATE: before writing the new state, gg runs every executable *.sh in
.gg/hooks/pre-task-done.d/ in lexicographic order. Any non-zero exit aborts
the transition with exit code 7 (ExitVerifyFailed); the task stays in its
current state and a machine-parseable {"event":"verify_failed",...} line is
emitted to stderr along with an internal 'gg tell' to all agents.
Install starter scripts with 'gg doctor --install-task-hooks' (auto-detects
Go and Node/Bun).

READY-FOR-LIVE GATE (opt-in): when .gg/config.yaml has
tasks.require_ready_for_live: true, this command refuses unless the task is
already in status "ready_for_live" (transition it with 'gg task ready-for-live'
after local checks pass). Combined with tasks.verifier_separation: true the
command also requires --verifier <role> and rejects when the verifier is the
same actor that performed the ready-for-live transition. Prevents the
premature-closure / same-actor-verification pattern surfaced by the
dogfood audit 2026-04-19.

See also: gg task review (request peer review), gg record (capture design decisions made during the work)`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskDone,
}

var taskBlockCmd = &cobra.Command{
	Use:   `block TASK-ID "reason"`,
	Short: "Mark a task blocked — state what specific dependency is missing",
	Long: `Signal that work is stalled because of an external dependency or unresolved question.

WHEN TO USE: you cannot make progress without input from another agent or an external
resource. The reason is stored and shown in 'gg status --blocked'.

WHEN NOT TO USE: for long-term deprioritization, update priority instead.`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskBlock,
}

var taskDepsCmd = &cobra.Command{
	Use:   "deps TASK-ID",
	Short: "Show dependency status for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runTaskDeps,
}

func init() {
	taskDoneCmd.Flags().StringVar(&taskDoneVerifier, "verifier",
		"", "actor role that verified the live run (required when tasks.verifier_separation is true)")
	taskCmd.AddCommand(taskDoneCmd)
	taskCmd.AddCommand(taskBlockCmd)
	taskCmd.AddCommand(taskDepsCmd)
}

func runTaskDone(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}

	// Shared per-command config cache — loaded lazily on first use so the
	// pre-hook and post-hook paths don't each pay for a separate
	// config.GGDir + config.Load pair.
	hookCfg := &hookConfig{}

	// Pre-done verify gate: run .gg/hooks/pre-task-done.d/*.sh BEFORE touching
	// the store. Always strict by design — a gate that passes on failure is
	// not a gate. If any hook fails, the task stays in its current state and
	// we return ExitVerifyFailed so agents can detect the blocked transition.
	//
	// Opt-out: skipped when GG_ENFORCEMENT=off. Set it to opt out for a session.
	if !enforcement.Enabled() {
		// Emit an audit line so telemetry can count how often the gate was bypassed.
		emitGuardSkipEvent("pre-task-done")
	} else if rej := runGateHooks(cmd, hookCfg, "pre-task-done", taskID, summary); rej != nil {
		emitGateFailedEvent(cmd.ErrOrStderr(), rej)
		notifyGateFailure(cmd, rej) // best-effort: no-op if store unreachable or opted out
		return &ExitError{
			Code:    ExitVerifyFailed,
			Message: fmt.Sprintf("%s hook rejected %s: %s exited %d (task state unchanged)", rej.Gate, rej.TaskID, rej.Hook, rej.ExitCode),
		}
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Ready-for-live gate (opt-in via .gg/config.yaml tasks.*). Runs AFTER
	// pre-task-done.d hooks so callers see hook failures before state-machine
	// complaints — cheaper feedback when both would reject. Same
	// GG_ENFORCEMENT=off escape hatch applies for emergencies.
	if _, cfg, cfgErr := hookCfg.load(cmd.ErrOrStderr()); cfgErr == nil && cfg != nil && cfg.Tasks.RequireReadyForLive {
		if !enforcement.Enabled() {
			emitGuardSkipEvent("pre-task-done-ready-for-live")
		} else {
			t, getErr := d.store.GetTask(ctx, taskID)
			if getErr != nil {
				return getErr
			}
			if rej := checkReadyForLiveGate(t, &cfg.Tasks, taskDoneVerifier); rej != nil {
				return rej
			}
		}
	}

	if err := d.store.UpdateTaskStatus(ctx, taskID, "done", summary); err != nil {
		return err
	}

	notifyTaskLifecycle(ctx, d.store, taskID, "done", summary)

	// Run post-done hooks from .gg/hooks/task-done.d/*.sh (warn-only unless hooks.strict=true).
	if hookErr := runTaskDoneHooks(cmd, hookCfg, taskID, summary); hookErr != nil {
		return hookErr // only non-nil when strict mode is enabled and a hook failed
	}

	warnBinaryStale()

	return printJSON(map[string]any{"id": taskID, "status": "done", "summary": summary}, func() {
		fmt.Printf("✓ %s marked as done\n", taskID)
	})
}

// taskHookEnv builds the env map passed to every task-lifecycle hook. The same
// contract is intended for future gates (pre-review-approve.d, etc.) so agents
// only learn one env shape.
func taskHookEnv(taskID, summary, projectID string) map[string]string {
	actor := os.Getenv("GG_ROLE")
	if actor == "" {
		actor = os.Getenv("GG_AGENT")
	}
	return map[string]string{
		"GG_TASK_ID":      taskID,
		"GG_TASK_SUMMARY": summary,
		"GG_PROJECT_ID":   projectID,
		"GG_ACTOR":        actor,
	}
}

// gateRejection describes a gate hook failure with enough detail to emit a
// structured stderr event and a cross-agent notification. Every field is part
// of the public contract — agents program against it, so changes here are
// schema changes.
type gateRejection struct {
	TaskID   string
	Gate     string // which gate directory triggered this (e.g. "pre-task-done")
	Hook     string
	ExitCode int
	Stderr   string // trimmed hook output, capped
}

// verifyRejection is the legacy alias for gateRejection kept as a type alias so
// external consumers (and tests written against the earlier name) don't break
// while the rest of the codebase migrates to the generic name.
type verifyRejection = gateRejection

// gateFailedPayload is the ordered NDJSON contract for verify_failed events.
// Struct layout matches the stderr field order so a human reading a log sees
// event → gate → task → hook → exit → ts → detail instead of alphabetical
// soup. JSON tags are stable public API.
type gateFailedPayload struct {
	Event  string `json:"event"`
	Gate   string `json:"gate"`
	Task   string `json:"task"`
	Hook   string `json:"hook"`
	Exit   int    `json:"exit"`
	TS     string `json:"ts"`
	Detail string `json:"detail"`
}

// debugLog writes a one-line diagnostic to stderr when GG_DEBUG=1. Used to
// surface silent fall-through paths (e.g. a gate skipping because .gg config
// could not be loaded) without spamming normal sessions.
func debugLog(w io.Writer, format string, args ...any) {
	if os.Getenv("GG_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(w, "[gg debug] "+format+"\n", args...)
}

// hookConfig memoises one config.GGDir + config.Load pair per command so the
// pre-hook and post-hook paths in runTaskDone do not each pay the cost of a
// second disk read. Zero value is a fresh, not-yet-loaded cache.
type hookConfig struct {
	loaded bool
	ggDir  string
	cfg    *config.Config
	err    error
}

// load returns the cached (ggDir, cfg) pair, loading on first call. When load
// fails the error is memoised too so repeat calls are cheap no-ops.
func (c *hookConfig) load(w io.Writer) (string, *config.Config, error) {
	if c.loaded {
		return c.ggDir, c.cfg, c.err
	}
	c.loaded = true
	c.ggDir, c.err = config.GGDir()
	if c.err != nil {
		debugLog(w, "hookConfig.load: GGDir failed (%v)", c.err)
		return "", nil, c.err
	}
	c.cfg, c.err = config.Load()
	if c.err != nil {
		debugLog(w, "hookConfig.load: config.Load failed (%v)", c.err)
	}
	return c.ggDir, c.cfg, c.err
}

// runGateHooks runs a task-lifecycle gate's *.sh scripts in lexicographic
// order. Strict is hardcoded true — a gate that passes on failure is not a
// gate. On success returns nil; on failure returns a *gateRejection carrying
// the first failing hook's name, exit code, and (trimmed) output. Caller is
// responsible for emitting the NDJSON event, notifying other agents, and
// returning the ExitVerifyFailed ExitError.
//
// gateName becomes the .d/ directory suffix (e.g. "pre-task-done" resolves to
// .gg/hooks/pre-task-done.d/) and rides along in the rejection payload so the
// stderr event identifies which gate fired. Future gates (pre-review-approve,
// pre-bug-fix, …) reuse the same runner.
//
// cache is the per-command hookConfig memo. Pass a non-nil empty cache for
// standalone calls (tests), or share one across the whole runTaskDone flow to
// avoid duplicate disk reads.
func runGateHooks(cmd *cobra.Command, cache *hookConfig, gateName, taskID, summary string) *gateRejection {
	if cache == nil {
		cache = &hookConfig{}
	}
	ggDir, cfg, err := cache.load(cmd.ErrOrStderr())
	if err != nil {
		return nil // no .gg or unreadable config — gate skipped, caller will hit the same error downstream
	}

	results, hookErr := hooks.RunHooks(ggDir, gateName, taskHookEnv(taskID, summary, cfg.ProjectID), true, cmd.ErrOrStderr())
	if hookErr == nil {
		return nil
	}

	// Strict mode guarantees the failing hook is the last (and only failing)
	// entry. Fall back to the final result defensively.
	var failed hooks.Result
	for _, r := range results {
		if r.Err != nil || r.ExitCode != 0 {
			failed = r
			break
		}
	}
	if failed.Script == "" && len(results) > 0 {
		failed = results[len(results)-1]
	}

	return &gateRejection{
		TaskID:   taskID,
		Gate:     gateName,
		Hook:     failed.Script,
		ExitCode: failed.ExitCode,
		Stderr:   truncateHookOutput(failed.Output, 400),
	}
}

// emitGateFailedEvent writes a single NDJSON line to stderr describing the
// rejection. The contract is deliberately boring: one line, stable keys,
// human-friendly field order — any agent can parse it with `tail -1` + `jq`.
// Human text continues to print alongside via hooks.RunHooks itself and the
// caller's ExitError message; this line is purely for machine consumption.
func emitGateFailedEvent(w io.Writer, rej *gateRejection) {
	payload := gateFailedPayload{
		Event:  "verify_failed",
		Gate:   rej.Gate,
		Task:   rej.TaskID,
		Hook:   rej.Hook,
		Exit:   rej.ExitCode,
		TS:     time.Now().UTC().Format(time.RFC3339),
		Detail: rej.Stderr,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return // should never happen with these field types
	}
	fmt.Fprintln(w, string(b))
}

// messageSender is a narrow interface for injecting the notification path in tests.
type messageSender interface {
	SendMessage(ctx context.Context, msg store.Message) error
}

// sendGateFailure is the testable core of cross-agent notification. It builds
// the message content and calls sender.SendMessage; errors are swallowed
// because a failed notification must never mask the underlying verify failure.
func sendGateFailure(ctx context.Context, sender messageSender, rej *gateRejection) {
	content := fmt.Sprintf("%s blocked at %s: %s (exit %d)", rej.TaskID, rej.Gate, rej.Hook, rej.ExitCode)
	if rej.Stderr != "" {
		content += " — " + firstLine(rej.Stderr)
	}
	_ = sender.SendMessage(ctx, store.Message{
		FromRole: "verify-gate",
		ToRole:   "all",
		Content:  content,
		TaskID:   rej.TaskID,
	})
}

// sendVerifyFailure is the legacy alias retained for the current test-suite
// naming; prefer sendGateFailure for new call sites.
func sendVerifyFailure(ctx context.Context, sender messageSender, rej *gateRejection) {
	sendGateFailure(ctx, sender, rej)
}

// notifyGateFailure broadcasts a "verify-gate" message so parallel agents and
// the next session's `gg status` surface the rejection. Best-effort: any
// error (store down, config missing) is swallowed — a failed notification
// must never mask the underlying verify failure. Skipped when
// GG_NO_AUTO_NOTIFY=1 (useful in CI / tests / reentrant hook scripts).
func notifyGateFailure(cmd *cobra.Command, rej *gateRejection) {
	if os.Getenv("GG_NO_AUTO_NOTIFY") == "1" {
		return
	}

	d, err := loadDeps(false)
	if err != nil {
		return
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	sendVerifyFailure(ctx, d.store, rej)
}

// notifyTaskLifecycle broadcasts a short "[actor → all] TASK-XXX <event>: detail"
// message so parallel sessions see task lifecycle transitions in gg status without
// a manual gg tell. Best-effort: errors are swallowed. Skipped when GG_NO_AUTO_NOTIFY=1.
func notifyTaskLifecycle(ctx context.Context, sender messageSender, taskID, event, detail string) {
	if os.Getenv("GG_NO_AUTO_NOTIFY") == "1" {
		return
	}
	actor := os.Getenv("GG_ROLE")
	if actor == "" {
		actor = os.Getenv("GG_AGENT")
	}
	if actor == "" {
		actor = "agent"
	}
	_ = sender.SendMessage(ctx, store.Message{
		FromRole: actor,
		ToRole:   "all",
		Audience: "agents",
		Content:  taskID + " " + event + ": " + detail,
		TaskID:   taskID,
	})
}

// truncateHookOutput trims a hook's combined stdout+stderr to at most n bytes,
// keeping the tail (most recent output usually carries the error) and adding a
// leading ellipsis when truncation occurred.
func truncateHookOutput(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// runTaskDoneHooks runs .gg/hooks/task-done.d/*.sh scripts after a task is
// marked done. Returns a non-nil error only in strict mode when a hook fails.
// cache may be nil for standalone use (tests); runTaskDone passes the shared
// cache to avoid loading config twice per command.
func runTaskDoneHooks(cmd *cobra.Command, cache *hookConfig, taskID, summary string) error {
	if cache == nil {
		cache = &hookConfig{}
	}
	ggDir, cfg, err := cache.load(cmd.ErrOrStderr())
	if err != nil {
		return nil // can't find .gg — skip silently
	}

	_, hookErr := hooks.RunHooks(ggDir, "task-done", taskHookEnv(taskID, summary, cfg.ProjectID), cfg.Hooks.Strict, cmd.ErrOrStderr())
	return hookErr
}

// warnBinaryStale prints an advisory when the most recently committed Go
// source file in the project is newer than the installed gg binary. This
// catches the "gg task done while binary is stale" workflow problem.
//
// The check is entirely advisory: it never fails the command. Silence it
// with GG_SKIP_SHIP_CHECK=1.
func warnBinaryStale() {
	if os.Getenv("GG_SKIP_SHIP_CHECK") == "1" {
		return
	}

	projectRoot, err := config.FindRoot()
	if err != nil {
		return // not in a gg project — skip
	}

	// Only warn in Go repos (go.mod present) to avoid false positives in
	// Python/JS projects where .go sources don't map to the gg binary.
	if _, statErr := os.Stat(filepath.Join(projectRoot, "go.mod")); statErr != nil {
		return
	}

	// Last commit timestamp touching any *.go file (Unix seconds, empty = no Go files).
	out, runErr := exec.Command("git", "-C", projectRoot, "log", "-1", "--format=%ct", "--", "*.go").Output()
	if runErr != nil || len(strings.TrimSpace(string(out))) == 0 {
		return
	}
	srcTS, convErr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if convErr != nil {
		return
	}
	srcTime := time.Unix(srcTS, 0)

	// Find installed binary.
	binPath, lookErr := exec.LookPath("gg")
	if lookErr != nil {
		return
	}
	info, statErr := os.Stat(binPath)
	if statErr != nil {
		return
	}

	if srcTime.After(info.ModTime()) {
		fmt.Fprintf(os.Stderr, "\n⚠  Source files modified after installed binary mtime.\n")
		fmt.Fprintf(os.Stderr, "   Binary: %s (built %s)\n", binPath, info.ModTime().Format("2006-01-02 15:04"))
		fmt.Fprintf(os.Stderr, "   Source: last commit %s\n", srcTime.Format("2006-01-02 15:04"))
		fmt.Fprintf(os.Stderr, "   Run: go install ./... (or your install path) then re-test.\n")
		fmt.Fprintf(os.Stderr, "   This task is marked done but may not be live in your shell.\n\n")
	}
}

func runTaskBlock(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.UpdateTaskStatus(ctx, taskID, "blocked", reason); err != nil {
		return err
	}

	notifyTaskLifecycle(ctx, d.store, taskID, "blocked", reason)

	return printJSON(map[string]any{"id": taskID, "status": "blocked", "reason": reason}, func() {
		fmt.Printf("⚠ %s marked as blocked: %s\n", taskID, reason)
	})
}

func runTaskDeps(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return notFound(err.Error())
	}

	type depEntry struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Priority string `json:"priority,omitempty"`
		Title    string `json:"title,omitempty"`
		Found    bool   `json:"found"`
	}

	var deps []depEntry
	allDone := true
	for _, depID := range t.DependsOn {
		dep, err := d.store.GetTask(ctx, depID)
		if err != nil {
			deps = append(deps, depEntry{ID: depID, Found: false})
			allDone = false
			continue
		}
		deps = append(deps, depEntry{ID: dep.ID, Status: dep.Status, Priority: dep.Priority, Title: dep.Title, Found: true})
		if dep.Status != "done" {
			allDone = false
		}
	}

	payload := map[string]any{
		"task_id":  taskID,
		"all_done": allDone,
		"deps":     deps,
	}
	return printJSON(payload, func() {
		if len(deps) == 0 {
			fmt.Printf("%s has no dependencies.\n", taskID)
			return
		}
		fmt.Printf("Dependencies of %s:\n", taskID)
		for _, dep := range deps {
			if !dep.Found {
				fmt.Printf("  ! %-12s (not found)\n", dep.ID)
				continue
			}
			fmt.Printf("  %s %-12s [%s] %s\n", statusIcon(dep.Status), dep.ID, dep.Priority, dep.Title)
		}
		fmt.Println()
		if allDone {
			fmt.Printf("✓ All dependencies done — %s is ready to start.\n", taskID)
		} else {
			fmt.Printf("○ Not all dependencies are done — %s is blocked by the above.\n", taskID)
		}
	})
}
