// Package changed computes the set of source files that need re-indexing
// in an incremental `gg index --changed` run.
//
// Strategy (CHANGED_CONTRACT.md §1):
//   - Changed files = `git diff --name-only <last_sha>` (committed + staged +
//     unstaged tracked working-tree delta)
//   - Plus untracked source files from `git ls-files --others --exclude-standard`
//   - First-run (no state): caller should fall back to full index
package changed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Files runs `git diff --name-only <baseSHA>` plus
// `git ls-files --others --exclude-standard` in projectRoot and returns the
// absolute paths of files that differ from the last indexed tree. Only files
// matching the given extension suffixes (e.g. ".go", ".ts") are included.
// An empty extensions list returns all changed files.
func Files(ctx context.Context, projectRoot, baseSHA string, extensions []string) ([]string, error) {
	if baseSHA == "" {
		return nil, fmt.Errorf("baseSHA must not be empty")
	}

	committedAndTracked, err := gitLines(ctx, projectRoot, "diff", "--name-only", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	untracked, err := gitLines(ctx, projectRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}

	seen := make(map[string]bool, len(committedAndTracked)+len(untracked))
	var files []string
	for _, line := range append(committedAndTracked, untracked...) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(extSet) > 0 && !extSet[filepath.Ext(line)] {
			continue
		}
		path := filepath.Join(projectRoot, filepath.FromSlash(line))
		if seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files, nil
}

// WorkingTreeFingerprint returns a deterministic fingerprint for the current
// tracked/untracked source delta against baseSHA. Clean trees return "".
func WorkingTreeFingerprint(ctx context.Context, projectRoot, baseSHA string, extensions []string) (string, error) {
	files, err := Files(ctx, projectRoot, baseSHA, extensions)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return "", fmt.Errorf("rel %s: %w", path, err)
		}
		rel = filepath.ToSlash(rel)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			_, _ = h.Write([]byte("<deleted>"))
		} else if readErr != nil {
			return "", fmt.Errorf("read %s: %w", path, readErr)
		} else {
			_, _ = h.Write(data)
		}
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

func gitLines(ctx context.Context, projectRoot string, args ...string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", projectRoot}, args...)...)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w — %s", err, strings.TrimSpace(errOut.String()))
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// HeadSHA returns the SHA of the current HEAD commit in projectRoot.
func HeadSHA(ctx context.Context, projectRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", projectRoot, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAncestor reports whether sha is a reachable ancestor of HEAD in projectRoot.
// It uses `git merge-base --is-ancestor <sha> HEAD` which exits 0 when true.
//
// Returns false (not an error) when sha is not an ancestor — callers use this
// to detect branch switches, rebases, or force pushes that break the linear
// history assumption of `--changed`.
//
// Returns an error only if git itself fails (e.g. sha is malformed or the repo
// is corrupt).
func IsAncestor(ctx context.Context, projectRoot, sha string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", projectRoot, "merge-base", "--is-ancestor", sha, "HEAD")
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		// Exit code 1 means "not an ancestor" — that is a valid outcome, not an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git merge-base: %w — %s", err, strings.TrimSpace(errOut.String()))
	}
	return true, nil
}
