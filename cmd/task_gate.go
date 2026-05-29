package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/hooks"
	"github.com/gurkangul/gg-cli/internal/store"
)

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

// messageSender is a narrow interface for injecting the notification path in tests.
type messageSender interface {
	SendMessage(ctx context.Context, msg store.Message) error
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

	// Strict hook execution guarantees the failing hook is the last (and only
	// failing) entry. Fall back to the final result defensively.
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
