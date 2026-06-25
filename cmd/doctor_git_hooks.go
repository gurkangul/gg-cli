package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// preCommitDispatcherBody is the git pre-commit dispatcher script. It fans
// out to every executable *.sh in the gg pre-commit.d directory, collecting
// non-zero exits and returning after the last script completes.
const preCommitDispatcherBody = `#!/bin/sh
# gg pre-commit dispatcher — installed by gg doctor --install-task-hooks
# Runs every executable *.sh in .gg/hooks/pre-commit.d/ in lexicographic order.
# Any script that exits non-zero fails the commit immediately.
# To bypass a script: set the env vars it documents (e.g. GG_BYPASS_RATIONALE).

GG_HOOK_DIR="$(git rev-parse --show-toplevel 2>/dev/null)/.gg/hooks/pre-commit.d"
if [ ! -d "$GG_HOOK_DIR" ]; then
  exit 0
fi

for hook in $(ls "$GG_HOOK_DIR"/*.sh 2>/dev/null | sort); do
  [ -x "$hook" ] || continue
  "$hook" "$@"
  rc=$?
  if [ $rc -ne 0 ]; then
    exit $rc
  fi
done
exit 0
`

// installGitPreCommitDispatcher installs the gg fan-out dispatcher at
// .git/hooks/pre-commit so that git actually calls the hooks in pre-commit.d.
// If a non-gg pre-commit hook already exists, a sourcing stanza is appended
// rather than overwriting — preserving existing behaviour.
func installGitPreCommitDispatcher(projectRoot, preCommitDir string) error {
	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		fmt.Printf("⚠ .git not found at %s — skipping pre-commit dispatcher (run after `git init`)\n", projectRoot)
		return nil
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create .git/hooks: %w", err)
	}
	dispatcherPath := filepath.Join(hooksDir, "pre-commit")

	existing, readErr := os.ReadFile(dispatcherPath) //nolint:gosec
	if readErr == nil {
		// File already exists. If it already fans out to our pre-commit.d
		// directory, nothing to do.
		if strings.Contains(string(existing), "pre-commit.d") {
			fmt.Printf("✓ .git/hooks/pre-commit already dispatches to pre-commit.d — skipping\n")
			return nil
		}
		// A foreign hook exists — append our fan-out stanza rather than
		// overwriting to avoid breaking the user's existing setup.
		stanza := "\n# gg pre-commit.d fan-out (appended by gg doctor --install-task-hooks)\n" +
			`GG_HOOK_DIR="$(git rev-parse --show-toplevel 2>/dev/null)/.gg/hooks/pre-commit.d"` + "\n" +
			`if [ -d "$GG_HOOK_DIR" ]; then` + "\n" +
			`  for _gg_hook in $(ls "$GG_HOOK_DIR"/*.sh 2>/dev/null | sort); do` + "\n" +
			`    [ -x "$_gg_hook" ] || continue` + "\n" +
			`    "$_gg_hook" "$@"` + "\n" +
			`    _gg_rc=$?` + "\n" +
			`    if [ $_gg_rc -ne 0 ]; then exit $_gg_rc; fi` + "\n" +
			`  done` + "\n" +
			`fi` + "\n"
		f, err := os.OpenFile(dispatcherPath, os.O_APPEND|os.O_WRONLY, 0o755) //nolint:gosec
		if err != nil {
			return fmt.Errorf("open .git/hooks/pre-commit: %w", err)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.WriteString(stanza); err != nil {
			return fmt.Errorf("append to .git/hooks/pre-commit: %w", err)
		}
		fmt.Printf("✓ Appended gg pre-commit.d fan-out to existing .git/hooks/pre-commit\n")
		fmt.Printf("  pre-commit hooks directory: %s\n", preCommitDir)
		return nil
	}

	// No existing hook — install the full dispatcher.
	if err := os.WriteFile(dispatcherPath, []byte(preCommitDispatcherBody), 0o755); err != nil {
		return fmt.Errorf("write .git/hooks/pre-commit: %w", err)
	}
	fmt.Printf("✓ Installed .git/hooks/pre-commit dispatcher\n")
	fmt.Printf("  hooks directory: %s\n", preCommitDir)
	return nil
}

// commitMsgDispatcherBody is the git commit-msg dispatcher script. git invokes
// commit-msg with the path to the prepared message as $1, so the dispatcher
// forwards "$@" to every hook in commit-msg.d. (pre-commit cannot do this — it
// runs before the message exists.)
const commitMsgDispatcherBody = `#!/bin/sh
# gg commit-msg dispatcher — installed by gg doctor --install-task-hooks
# Runs every executable *.sh in .gg/hooks/commit-msg.d/ in lexicographic order,
# passing the commit-message file path ("$@") through to each hook.
# Any script that exits non-zero rejects the commit message immediately.
# To bypass a script: set the env vars it documents (e.g. GG_BYPASS_RATIONALE).

GG_HOOK_DIR="$(git rev-parse --show-toplevel 2>/dev/null)/.gg/hooks/commit-msg.d"
if [ ! -d "$GG_HOOK_DIR" ]; then
  exit 0
fi

for hook in $(ls "$GG_HOOK_DIR"/*.sh 2>/dev/null | sort); do
  [ -x "$hook" ] || continue
  "$hook" "$@"
  rc=$?
  if [ $rc -ne 0 ]; then
    exit $rc
  fi
done
exit 0
`

// installGitCommitMsgDispatcher installs the gg fan-out dispatcher at
// .git/hooks/commit-msg so git calls the hooks in commit-msg.d. A foreign
// commit-msg hook is preserved by appending a fan-out stanza rather than
// overwriting. Mirrors installGitPreCommitDispatcher.
func installGitCommitMsgDispatcher(projectRoot, commitMsgDir string) error {
	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		fmt.Printf("⚠ .git not found at %s — skipping commit-msg dispatcher (run after `git init`)\n", projectRoot)
		return nil
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create .git/hooks: %w", err)
	}
	dispatcherPath := filepath.Join(hooksDir, "commit-msg")

	existing, readErr := os.ReadFile(dispatcherPath) //nolint:gosec
	if readErr == nil {
		if strings.Contains(string(existing), "commit-msg.d") {
			fmt.Printf("✓ .git/hooks/commit-msg already dispatches to commit-msg.d — skipping\n")
			return nil
		}
		stanza := "\n# gg commit-msg.d fan-out (appended by gg doctor --install-task-hooks)\n" +
			`GG_HOOK_DIR="$(git rev-parse --show-toplevel 2>/dev/null)/.gg/hooks/commit-msg.d"` + "\n" +
			`if [ -d "$GG_HOOK_DIR" ]; then` + "\n" +
			`  for _gg_hook in $(ls "$GG_HOOK_DIR"/*.sh 2>/dev/null | sort); do` + "\n" +
			`    [ -x "$_gg_hook" ] || continue` + "\n" +
			`    "$_gg_hook" "$@"` + "\n" +
			`    _gg_rc=$?` + "\n" +
			`    if [ $_gg_rc -ne 0 ]; then exit $_gg_rc; fi` + "\n" +
			`  done` + "\n" +
			`fi` + "\n"
		f, err := os.OpenFile(dispatcherPath, os.O_APPEND|os.O_WRONLY, 0o755) //nolint:gosec
		if err != nil {
			return fmt.Errorf("open .git/hooks/commit-msg: %w", err)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.WriteString(stanza); err != nil {
			return fmt.Errorf("append to .git/hooks/commit-msg: %w", err)
		}
		fmt.Printf("✓ Appended gg commit-msg.d fan-out to existing .git/hooks/commit-msg\n")
		fmt.Printf("  commit-msg hooks directory: %s\n", commitMsgDir)
		return nil
	}

	if err := os.WriteFile(dispatcherPath, []byte(commitMsgDispatcherBody), 0o755); err != nil {
		return fmt.Errorf("write .git/hooks/commit-msg: %w", err)
	}
	fmt.Printf("✓ Installed .git/hooks/commit-msg dispatcher\n")
	fmt.Printf("  hooks directory: %s\n", commitMsgDir)
	return nil
}
