// Package filesize implements the file-size modularity rule used by
// `gg audit file-size` and the 30-file-size.sh pre-task-done gate.
package filesize

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultSourceLimit = 500
	DefaultTestLimit   = 800
)

// excludedPrefixes lists path prefixes that are never gated.
var excludedPrefixes = []string{
	"vendor/",
	"node_modules/",
	"testdata/",
	"_bmad",
	"_bmad-output",
	"docs/cli/",
}

// excludedSuffixes lists filename suffixes that are never gated.
var excludedSuffixes = []string{
	".pb.go",
	"_generated.go",
}

// sourcedExtensions are the file extensions subject to the size gate.
var sourcedExtensions = []string{
	".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java",
}

// Baseline records line counts for grandfathered files.
type Baseline struct {
	Version    int            `json:"version"`
	CapturedAt string         `json:"captured_at"`
	Files      map[string]int `json:"files"`
}

// Violation describes a single file that exceeds its limit.
type Violation struct {
	Path     string
	Lines    int
	Limit    int
	Baseline int // 0 if file is not in the baseline
}

// ShouldExclude reports whether path should be skipped by the size gate.
func ShouldExclude(path string) bool {
	for _, p := range excludedPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, s := range excludedSuffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	// Check extension is in the gated set.
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range sourcedExtensions {
		if ext == e {
			return false
		}
	}
	return true // not a gated extension
}

// IsTestFile reports whether the file path looks like a test file.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	noExt := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(base, ".go"), ".ts"), ".js")
	noExt = strings.TrimSuffix(strings.TrimSuffix(noExt, ".tsx"), ".jsx")
	noExt = strings.TrimSuffix(strings.TrimSuffix(noExt, ".py"), ".java")
	noExt = strings.TrimSuffix(noExt, ".rs")
	if strings.HasSuffix(noExt, ".test") || strings.HasSuffix(noExt, ".spec") {
		return true
	}
	return false
}

// ParseLimit returns the line limit for the given file path.
func ParseLimit(path string) int {
	if IsTestFile(path) {
		return DefaultTestLimit
	}
	return DefaultSourceLimit
}

// CountLines returns the number of lines in the file.
func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
	}
	return n, sc.Err()
}

// ReadBaseline reads .gg/file-size-baseline.json relative to root.
// Returns an empty baseline (not an error) when the file does not exist.
func ReadBaseline(root string) (*Baseline, error) {
	path := filepath.Join(root, ".gg", "file-size-baseline.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Baseline{Files: map[string]int{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	if b.Files == nil {
		b.Files = map[string]int{}
	}
	return &b, nil
}

// WriteBaseline writes the baseline to .gg/file-size-baseline.json.
func WriteBaseline(root string, b *Baseline) error {
	b.Version = 1
	b.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ".gg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "file-size-baseline.json"), append(data, '\n'), 0o644)
}

// ScanDir walks root and returns all gated files with their line counts.
// Does not apply the baseline filter — callers may apply it themselves.
func ScanDir(root string) (map[string]int, error) {
	result := make(map[string]int)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			base := d.Name()
			switch base {
			case "vendor", "node_modules", "testdata", ".git", ".gg", ".gsd",
				"dist", "build", "_bmad-output":
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, "_bmad") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || ShouldExclude(rel) {
			return nil
		}
		n, err := CountLines(path)
		if err != nil {
			return nil
		}
		result[rel] = n
		return nil
	})
	return result, err
}

// CheckViolations computes violations from a file→lines map given a baseline.
// When useBaseline is false, all files over their limit are flagged regardless.
func CheckViolations(files map[string]int, b *Baseline, useBaseline bool) []Violation {
	var out []Violation
	for path, lines := range files {
		limit := ParseLimit(path)
		if lines <= limit {
			continue
		}
		baselineLines := 0
		if useBaseline && b != nil {
			baselineLines = b.Files[path]
		}
		if useBaseline && baselineLines > 0 {
			// Grandfathered: only flag if it grew beyond baseline.
			if lines <= baselineLines {
				continue
			}
		}
		out = append(out, Violation{
			Path:     path,
			Lines:    lines,
			Limit:    limit,
			Baseline: baselineLines,
		})
	}
	return out
}
