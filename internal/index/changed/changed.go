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

// EmptyTreeSHA is Git's canonical empty tree object. It is used as the
// synthetic base for repositories whose branch has no commits yet: source files
// can still be indexed, and the first real commit will naturally appear as a
// delta from this base.
const EmptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// FileChangeKind is the coarse change type used by incremental indexing and
// freshness summaries. Git has more statuses (rename/copy/type-change), but gg
// only needs to know whether a path must be re-written, created, or removed.
type FileChangeKind string

const (
	FileModified FileChangeKind = "modified"
	FileAdded    FileChangeKind = "added"
	FileDeleted  FileChangeKind = "deleted"
)

// FileChange describes one project file changed since the indexed base.
// Path is absolute and points at the project-relative path that should be
// invalidated/re-indexed. Rename entries are represented as two changes: the old
// path as deleted and the new path as added, so stale graph nodes do not survive.
type FileChange struct {
	Path string
	Kind FileChangeKind
}

// Files runs `git diff --name-only <baseSHA>` plus
// `git ls-files --others --exclude-standard` in projectRoot and returns the
// absolute paths of files that differ from the last indexed tree. Only files
// matching the given extension suffixes (e.g. ".go", ".ts") are included.
// An empty extensions list returns all changed files.
func Files(ctx context.Context, projectRoot, baseSHA string, extensions []string) ([]string, error) {
	changes, err := DetailedFiles(ctx, projectRoot, baseSHA, extensions)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(changes))
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		if seen[change.Path] {
			continue
		}
		seen[change.Path] = true
		files = append(files, change.Path)
	}
	return files, nil
}

// DetailedFiles is like Files but preserves whether each path is added,
// modified, or deleted. It uses `git diff --name-status <baseSHA>` for tracked
// paths plus untracked files as added paths.
func DetailedFiles(ctx context.Context, projectRoot, baseSHA string, extensions []string) ([]FileChange, error) {
	if baseSHA == "" {
		return nil, fmt.Errorf("baseSHA must not be empty")
	}

	committedAndTracked, err := gitLines(ctx, projectRoot, "diff", "--name-status", baseSHA)
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

	seen := make(map[string]FileChangeKind, len(committedAndTracked)+len(untracked))
	var changes []FileChange
	addChange := func(rel string, kind FileChangeKind) {
		rel = strings.TrimSpace(rel)
		if rel == "" || (len(extSet) > 0 && !extSet[filepath.Ext(rel)]) {
			return
		}
		path := filepath.Join(projectRoot, filepath.FromSlash(rel))
		if prev, ok := seen[path]; ok {
			if prev == kind || prev == FileDeleted {
				return
			}
		}
		seen[path] = kind
		changes = append(changes, FileChange{Path: path, Kind: kind})
	}
	for _, line := range committedAndTracked {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		status := fields[0]
		code := status[0]
		switch code {
		case 'A':
			if len(fields) > 1 {
				addChange(fields[1], FileAdded)
			}
		case 'D':
			if len(fields) > 1 {
				addChange(fields[1], FileDeleted)
			}
		case 'R':
			if len(fields) > 2 {
				addChange(fields[1], FileDeleted)
				addChange(fields[2], FileAdded)
			}
		case 'C':
			if len(fields) > 2 {
				addChange(fields[2], FileAdded)
			}
		default:
			if len(fields) > 1 {
				addChange(fields[len(fields)-1], FileModified)
			}
		}
	}
	for _, line := range untracked {
		addChange(line, FileAdded)
	}
	return changes, nil
}

// WorkingTreeFingerprint returns a deterministic fingerprint for the current
// tracked/untracked source delta against baseSHA. Clean trees return "".
func WorkingTreeFingerprint(ctx context.Context, projectRoot, baseSHA string, extensions []string) (string, error) {
	return WorkingTreeFingerprintWithNames(ctx, projectRoot, baseSHA, extensions, nil)
}

// WorkingTreeFingerprintWithNames returns a deterministic fingerprint for
// changed paths selected by extension OR exact basename. Use names for module
// manifests such as go.mod/package.json that do not have meaningful extensions.
func WorkingTreeFingerprintWithNames(ctx context.Context, projectRoot, baseSHA string, extensions, names []string) (string, error) {
	changes, err := DetailedFiles(ctx, projectRoot, baseSHA, nil)
	if err != nil {
		return "", err
	}
	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}
	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[name] = true
	}
	var selected []FileChange
	for _, change := range changes {
		base := filepath.Base(change.Path)
		if (len(extSet) == 0 && len(nameSet) == 0) || extSet[filepath.Ext(change.Path)] || nameSet[base] {
			selected = append(selected, change)
		}
	}
	if len(selected) == 0 {
		return "", nil
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Path == selected[j].Path {
			return selected[i].Kind < selected[j].Kind
		}
		return selected[i].Path < selected[j].Path
	})
	h := sha256.New()
	for _, change := range selected {
		rel, err := filepath.Rel(projectRoot, change.Path)
		if err != nil {
			return "", fmt.Errorf("rel %s: %w", change.Path, err)
		}
		rel = filepath.ToSlash(rel)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(change.Kind))
		_, _ = h.Write([]byte{0})
		data, readErr := os.ReadFile(change.Path)
		switch {
		case os.IsNotExist(readErr):
			_, _ = h.Write([]byte("<deleted>"))
		case readErr != nil:
			return "", fmt.Errorf("read %s: %w", change.Path, readErr)
		default:
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
	cmd := exec.CommandContext(ctx, "git", "-C", projectRoot, "rev-parse", "--verify", "HEAD")
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	out, err := cmd.Output()
	if err != nil {
		if isUnbornHead(ctx, projectRoot) {
			return EmptyTreeSHA, nil
		}
		return "", fmt.Errorf("git rev-parse HEAD: %w — %s", err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func isUnbornHead(ctx context.Context, projectRoot string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", projectRoot, "symbolic-ref", "-q", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return false
	}
	check := exec.CommandContext(ctx, "git", "-C", projectRoot, "show-ref", "--verify", "--quiet", ref)
	if err := check.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true
		}
	}
	return false
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
	if sha == EmptyTreeSHA {
		return true, nil
	}
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
