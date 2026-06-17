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

// indexHookBody refreshes the local CodeGraph after a git operation. It is a
// FOREGROUND git hook, not a background daemon — it runs `gg index --changed`
// to completion and is non-blocking: any failure warns on stderr but never
// aborts the git operation (a stale graph must not stop a push/merge/commit).
// This keeps the recorded no-daemon CodeGraph contract intact while giving
// opt-in freshness (TASK-471, TASK-502). The hook ALWAYS exits 0 so a missing
// gg binary or a failed index never blocks the git operation.
const indexHookBody = `#!/bin/sh
# ` + indexHookMarker + ` — installed by gg (doctor --install-index-hooks / init)
# Foreground, non-blocking. Refreshes the local code graph (.gg/graph.db) from
# the changed files. Not a daemon. A failure warns but never blocks git.
command -v gg >/dev/null 2>&1 || exit 0
gg index --changed >/dev/null 2>&1 || \
  echo "gg: index --changed failed — CodeGraph may be stale (run 'gg doctor --fix-index')" >&2
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
	fmt.Println("  Non-blocking: an index failure warns but never aborts git.")
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
			fmt.Printf("✓ .git/hooks/%s already runs gg index — skipping\n", name)
			return nil
		}
		// Foreign hook present — append our non-blocking stanza.
		stanza := "\n# " + indexHookMarker + " (appended by gg doctor --install-index-hooks)\n" +
			"command -v gg >/dev/null 2>&1 && { gg index --changed >/dev/null 2>&1 || " +
			`echo "gg: index --changed failed — CodeGraph may be stale" >&2; }` + "\n"
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
