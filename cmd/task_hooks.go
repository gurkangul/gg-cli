package cmd

import (
	"context"
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
	"github.com/gurkangul/gg-cli/internal/hooks"
	"github.com/gurkangul/gg-cli/internal/store"
)

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
		// GG_INSIDE_HOOK=1 signals to tests and tools that they are running
		// inside a pre-task-done hook invoked by the parent gg process. Tests
		// that access live project state (git, Qdrant) should skip when this
		// is set to avoid racing with the parent process.
		"GG_INSIDE_HOOK": "1",
	}
}

// runTaskDoneHooks runs .gg/hooks/task-done.d/*.sh scripts after a task is
// marked done. Returns a non-nil error only in strict mode when a hook fails.
// cache may be nil for standalone use (tests); runTaskDone passes the shared
// cache to avoid loading config twice per command.
func runTaskDoneHooks(cmd *cobra.Command, cache *hookConfig, taskID, summary string) error {
	_, err := runTaskDoneHooksResult(cmd, cache, taskID, summary)
	return err
}

func runTaskDoneHooksResult(cmd *cobra.Command, cache *hookConfig, taskID, summary string) (bool, error) {
	if cache == nil {
		cache = &hookConfig{}
	}
	ggDir, cfg, err := cache.load(cmd.ErrOrStderr())
	if err != nil {
		return false, nil // can't find .gg — skip silently
	}

	results, hookErr := hooks.RunHooks(ggDir, "task-done", taskHookEnv(taskID, summary, cfg.ProjectID), cfg.Hooks.Strict, cmd.ErrOrStderr())
	findings := false
	for _, r := range results {
		if r.Err != nil || r.ExitCode != 0 {
			findings = true
			break
		}
	}
	return findings, hookErr
}

// notifyTaskLifecycle broadcasts a short "[actor → all] TASK-XXX <event>: detail"
// message so parallel sessions see task lifecycle transitions in gg status without
// a manual gg tell. Best-effort: errors are swallowed. Skipped when GG_NO_AUTO_NOTIFY=1.
func notifyTaskLifecycle(ctx context.Context, sender messageSender, taskID, event, detail string) {
	if os.Getenv("GG_NO_AUTO_NOTIFY") == "1" {
		return
	}
	// BUG-106: read raw GG_AGENT and this broadcast said "claude-code" while the
	// very same command stamped the task owner "claude-code-<sid>" — one action,
	// two identities. "agent" stays as the last resort, where it is honest.
	actor := resolveAuthorEnv()
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

// debugLog writes a one-line diagnostic to stderr when GG_DEBUG=1. Used to
// surface silent fall-through paths (e.g. a gate skipping because .gg config
// could not be loaded) without spamming normal sessions.
func debugLog(w io.Writer, format string, args ...any) {
	if os.Getenv("GG_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(w, "[gg debug] "+format+"\n", args...)
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
