// Package changed computes the set of source files that need re-indexing
// in an incremental `gg index --changed` run.
//
// Strategy (CHANGED_CONTRACT.md §1):
//   - Changed files = `git diff --name-only <last_sha> HEAD` (committed delta)
//   - Plus any untracked files the user hasn't committed yet (future: day-2)
//   - First-run (no state): caller should fall back to full index
package changed

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Files runs `git diff --name-only <baseSHA> HEAD` in projectRoot and
// returns the absolute paths of files that changed. Only files matching
// the given extension suffixes (e.g. ".go", ".ts") are included.
// An empty extensions list returns all changed files.
func Files(ctx context.Context, projectRoot, baseSHA string, extensions []string) ([]string, error) {
	if baseSHA == "" {
		return nil, fmt.Errorf("baseSHA must not be empty")
	}

	// git diff --name-only <baseSHA> HEAD
	cmd := exec.CommandContext(ctx, "git", "-C", projectRoot, "diff", "--name-only", baseSHA, "HEAD")
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff: %w — %s", err, strings.TrimSpace(errOut.String()))
	}

	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(extSet) > 0 && !extSet[filepath.Ext(line)] {
			continue
		}
		files = append(files, filepath.Join(projectRoot, filepath.FromSlash(line)))
	}
	return files, nil
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
