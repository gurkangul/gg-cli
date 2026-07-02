package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexHookMarker identifies a gg-installed index hook so re-runs are idempotent
// and foreign hooks are detected.
const indexHookMarker = "gg-index-hook"

// indexHookNames are the git hooks gg installs to keep the local CodeGraph
// fresh. pre-push reflects what you push; post-merge re-indexes after pulling
// teammates' code; post-commit (TASK-502) refreshes on every local commit so
// the graph stays fresh during commit→commit local dev, not only at push/pull.
var indexHookNames = []string{"pre-push", "post-merge", "post-commit"}

// indexHookBody refreshes the local CodeGraph after a git operation. It launches
// `gg index --changed` DETACHED (nohup … &) so git never waits on indexing — the
// hook returns immediately and the graph refreshes in the background. It is not a
// daemon: one fire-and-forget run per git op, concurrency-guarded by gg's index
// lock (see index_lock.go) so overlapping commit→push runs never race the graph.
// Output goes to .gg/index-hook.log; the hook ALWAYS exits 0 so a missing gg
// binary or a failed index never blocks the git operation (TASK-471, TASK-502).
const indexHookBody = `#!/bin/sh
# ` + indexHookMarker + ` — installed by gg (doctor --install-index-hooks / init)
# Detached, non-blocking: refreshes the local code graph (.gg/graph.db) in the
# BACKGROUND so git never waits on indexing. Not a daemon — one fire-and-forget
# run per git op, concurrency-guarded and logged by gg (.gg/index-hook.log).
command -v gg >/dev/null 2>&1 || exit 0
nohup gg index --changed >>.gg/index-hook.log 2>&1 </dev/null &
exit 0
`

// installGitIndexHooks installs the index hook set ({pre-push, post-merge,
// post-commit}) that keeps the local CodeGraph fresh. pre-push makes your graph
// reflect what you push; post-merge re-indexes after you pull teammates' code;
// post-commit re-indexes on every local commit. Existing foreign hooks are
// appended to, not overwritten; gg-installed hooks are left as-is.
func installGitIndexHooks(projectRoot string) error {
	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		fmt.Printf("⚠ .git not found at %s — skipping index hooks (run after `git init`)\n", projectRoot)
		return nil
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create .git/hooks: %w", err)
	}
	for _, name := range indexHookNames {
		if err := installOneIndexHook(filepath.Join(hooksDir, name), name); err != nil {
			return err
		}
	}
	fmt.Println("  CodeGraph now refreshes on commit (post-commit), push (pre-push), and after pulling (post-merge).")
	fmt.Println("  Detached & non-blocking: indexing runs in the background (see .gg/index-hook.log); it never blocks or aborts git.")
	return nil
}

// indexHooksInstalled reports whether ALL of the gg index hooks are present and
// contain the gg marker. Used by status/doctor to surface install state and by
// init to decide whether to print the auto-refresh banner. A missing .git dir
// (not a repo) returns false with no error — there is nothing to install into.
func indexHooksInstalled(projectRoot string) bool {
	hooksDir := filepath.Join(projectRoot, ".git", "hooks")
	for _, name := range indexHookNames {
		data, err := os.ReadFile(filepath.Join(hooksDir, name)) //nolint:gosec
		if err != nil || !strings.Contains(string(data), indexHookMarker) {
			return false
		}
	}
	return true
}

// doctorCheckIndexHooks surfaces whether the CodeGraph git hooks are installed
// (TASK-502 AC-3). When the graph is fresh it is an ok/warn line; when the
// graph needs a notice AND the hooks are missing it fails with the install
// prompt so a stale graph that nothing is auto-refreshing is loud, not silent.
// anyIndexHookInstalled reports whether at least one gg-owned index hook is
// present. `gg system sync` uses this to REFRESH index hooks only where the
// project already opted in, so a project that deliberately runs without them
// (init --no-index-hooks) is never force-installed into during a host-wide sync.
func anyIndexHookInstalled(projectRoot string) bool {
	hooksDir := filepath.Join(projectRoot, ".git", "hooks")
	for _, name := range indexHookNames {
		data, err := os.ReadFile(filepath.Join(hooksDir, name)) //nolint:gosec
		if err == nil && strings.Contains(string(data), indexHookMarker) {
			return true
		}
	}
	return false
}

func doctorCheckIndexHooks(root string, fresh codeGraphFreshness, report *doctorReport) {
	if indexHooksInstalled(root) {
		report.ok("index hooks", "installed (pre-push/post-merge/post-commit auto-refresh the CodeGraph)")
		return
	}
	if fresh.NeedsNotice() {
		report.fail("index hooks", "missing and CodeGraph is stale — install with `gg doctor --install-index-hooks` so commit/push/merge auto-refresh it")
		return
	}
	report.warn("index hooks", "not installed — run `gg doctor --install-index-hooks` to auto-refresh the CodeGraph on commit/push/merge")
}

func installOneIndexHook(path, name string) error {
	existing, readErr := os.ReadFile(path) //nolint:gosec
	if readErr == nil {
		if strings.Contains(string(existing), indexHookMarker) {
			// A gg-owned hook that still holds ONLY gg's own lines is safe to rewrite:
			// upgrade it in place when its body drifts from the current template so
			// existing installs pick up new behavior (e.g. the foreground→detached
			// switch). A foreign hook we only appended a stanza to, or a gg hook the
			// user has since added their own commands to, must never be overwritten.
			if isGGOwnedIndexHook(string(existing)) && strings.TrimSpace(string(existing)) != strings.TrimSpace(indexHookBody) {
				if !isPristineGGIndexHook(string(existing)) {
					fmt.Printf("✓ .git/hooks/%s is a gg hook with local edits — left as-is (switch the `gg index --changed` line to the detached form by hand)\n", name)
					return nil
				}
				if err := os.WriteFile(path, []byte(indexHookBody), 0o755); err != nil { //nolint:gosec
					return fmt.Errorf("upgrade .git/hooks/%s: %w", name, err)
				}
				fmt.Printf("✓ Upgraded .git/hooks/%s to detached gg index\n", name)
				return nil
			}
			fmt.Printf("✓ .git/hooks/%s already runs gg index — skipping\n", name)
			return nil
		}
		// Foreign hook present — append our detached, non-blocking stanza. The whole
		// group is backgrounded with its fds redirected ({ … } >log </dev/null &):
		// backgrounding an `A && B` list runs it in a subshell that would otherwise
		// keep git's inherited stdout/stderr pipe open until the index finishes,
		// making git BLOCK. Redirecting the group's fds is what lets git return at once.
		stanza := "\n# " + indexHookMarker + " (appended by gg doctor --install-index-hooks)\n" +
			"{ command -v gg >/dev/null 2>&1 && nohup gg index --changed; } " +
			">>.gg/index-hook.log 2>&1 </dev/null &\n"
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o755) //nolint:gosec
		if err != nil {
			return fmt.Errorf("open .git/hooks/%s: %w", name, err)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.WriteString(stanza); err != nil {
			return fmt.Errorf("append to .git/hooks/%s: %w", name, err)
		}
		fmt.Printf("✓ Appended gg index stanza to existing .git/hooks/%s\n", name)
		return nil
	}
	if err := os.WriteFile(path, []byte(indexHookBody), 0o755); err != nil { //nolint:gosec
		return fmt.Errorf("write .git/hooks/%s: %w", name, err)
	}
	fmt.Printf("✓ Installed .git/hooks/%s (gg index --changed)\n", name)
	return nil
}

// isGGOwnedIndexHook reports whether the hook file is one gg wrote in full (our
// shebang + marker on the first lines), as opposed to a foreign hook we only
// appended a stanza to. Only fully-owned hooks are safe to overwrite on upgrade;
// a foreign hook (detected by the append marker) must be left intact.
func isGGOwnedIndexHook(body string) bool {
	if !strings.HasPrefix(body, "#!/bin/sh\n# "+indexHookMarker) {
		return false
	}
	return !strings.Contains(body, "(appended by gg")
}

// isPristineGGIndexHook reports whether a gg-owned index hook still contains ONLY
// gg's own lines — the shebang, comments, the `command -v gg` guard, the
// `gg index --changed` invocation (foreground or detached), and gg's own echo
// warnings / `exit 0`. If the user has added their own command to the file, this
// returns false so the upgrade path leaves it untouched instead of silently
// discarding that edit. It is deliberately permissive about gg's own lines so
// every historical gg template (which differ only in comment wording) still
// upgrades cleanly.
func isPristineGGIndexHook(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "", t == "exit 0", t == "\\":
			continue
		case strings.HasPrefix(t, "#"), strings.HasPrefix(t, "echo "):
			continue
		case strings.Contains(t, "command -v gg"), strings.Contains(t, "gg index --changed"):
			continue
		default:
			return false
		}
	}
	return true
}
