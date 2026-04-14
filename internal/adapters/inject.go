package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	blockStart = "<!-- gg-managed-start -->"
	blockEnd   = "<!-- gg-managed-end -->"
	fileHeader = "<!-- This file is managed by gg adapt. Do not edit the managed block. -->\n\n"
)

// InjectResult is returned by Inject to report what happened.
type InjectResult struct {
	Path    string
	Created bool // true if the file was newly created
	Updated bool // true if content changed
}

// Inject writes the gg rules block into the target file using the adapter's
// configured format. agentsContent is the raw content of AGENTS.md.
func Inject(a *Adapter, targetPath, agentsContent string) (*InjectResult, error) {
	switch a.Format {
	case FullReplace:
		return injectFullReplace(targetPath, agentsContent)
	case ManagedBlock:
		return injectManagedBlock(targetPath, agentsContent)
	case Symlink:
		return injectSymlink(targetPath, agentsContent)
	default:
		return nil, fmt.Errorf("unknown format %d for adapter %s", a.Format, a.Name)
	}
}

// Check reports whether the managed block in targetPath is up-to-date with
// agentsContent. Returns (upToDate bool, drift string, err error).
func Check(a *Adapter, targetPath, agentsContent string) (bool, string, error) {
	switch a.Format {
	case FullReplace:
		return checkFullReplace(targetPath, agentsContent)
	case ManagedBlock:
		return checkManagedBlock(targetPath, agentsContent)
	case Symlink:
		return checkSymlink(targetPath, agentsContent)
	default:
		return false, "", fmt.Errorf("unknown format %d", a.Format)
	}
}

// ── FullReplace ─────────────────────────────────────────────────────────────

func injectFullReplace(path, content string) (*InjectResult, error) {
	existing, _ := os.ReadFile(path)
	body := fileHeader + wrapBlock(content)
	if string(existing) == body {
		return &InjectResult{Path: path}, nil
	}
	created := len(existing) == 0
	if err := writeFile(path, body); err != nil {
		return nil, err
	}
	return &InjectResult{Path: path, Created: created, Updated: !created}, nil
}

func checkFullReplace(path, content string) (bool, string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, "file not found", nil
	}
	if err != nil {
		return false, "", err
	}
	want := fileHeader + wrapBlock(content)
	if string(existing) == want {
		return true, "", nil
	}
	return false, "file content has drifted from AGENTS.md", nil
}

// ── ManagedBlock ─────────────────────────────────────────────────────────────

func injectManagedBlock(path, content string) (*InjectResult, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	created := os.IsNotExist(err)

	block := wrapBlock(content)
	var newBody string
	if created {
		newBody = block
	} else {
		newBody = replaceBlock(string(existing), block)
	}

	if !created && newBody == string(existing) {
		return &InjectResult{Path: path}, nil
	}
	if err := writeFile(path, newBody); err != nil {
		return nil, err
	}
	return &InjectResult{Path: path, Created: created, Updated: !created}, nil
}

func checkManagedBlock(path, content string) (bool, string, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, "managed block not present (file not found)", nil
	}
	if err != nil {
		return false, "", err
	}

	start := strings.Index(string(existing), blockStart)
	end := strings.Index(string(existing), blockEnd)
	if start == -1 || end == -1 {
		return false, "managed block markers not found — run 'gg adapt' to inject", nil
	}

	current := string(existing)[start+len(blockStart) : end]
	want := "\n" + content + "\n"
	if strings.TrimSpace(current) == strings.TrimSpace(want) {
		return true, "", nil
	}
	return false, "managed block is out of date with AGENTS.md", nil
}

// ── Symlink ──────────────────────────────────────────────────────────────────

func injectSymlink(path, _ string) (*InjectResult, error) {
	// For symlink format (e.g. codex/AGENTS.md), the target IS AGENTS.md —
	// no injection needed. Just confirm the file exists.
	if _, err := os.Stat(path); err == nil {
		return &InjectResult{Path: path}, nil
	}
	return &InjectResult{Path: path}, nil
}

func checkSymlink(path, _ string) (bool, string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, "AGENTS.md not found — run 'gg init' to create it", nil
	}
	return true, "", nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// wrapBlock wraps content in the gg managed-block delimiters.
func wrapBlock(content string) string {
	return blockStart + "\n" + content + "\n" + blockEnd + "\n"
}

// replaceBlock replaces the managed block inside body with newBlock.
// If no markers exist, the block is appended.
func replaceBlock(body, newBlock string) string {
	start := strings.Index(body, blockStart)
	end := strings.Index(body, blockEnd)
	if start == -1 || end == -1 {
		// Append block if markers are absent.
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		return body + "\n" + newBlock
	}
	after := end + len(blockEnd)
	if after < len(body) && body[after] == '\n' {
		after++
	}
	return body[:start] + newBlock + body[after:]
}

// writeFile creates parent directories as needed and writes content atomically.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // G703: path comes from adapter registry + ResolveTarget(cwd), not user input
}
