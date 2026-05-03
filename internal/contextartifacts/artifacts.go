// Package contextartifacts finds project-local knowledge files declared for
// inclusion in `gg context`.
package contextartifacts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ConfigFile = ".gg/context-artifacts.yaml"
	LockFile   = ".gg/context-artifacts.lock.json"
	MaxFile    = 1 << 20
)

type Config struct {
	Paths     []string         `yaml:"paths"`
	Artifacts []ArtifactConfig `yaml:"artifacts"`
}

type ArtifactConfig struct {
	Path string `yaml:"path"`
}

type Lock struct {
	Version   int                  `json:"version"`
	IndexedAt string               `json:"indexed_at"`
	Artifacts map[string]LockEntry `json:"artifacts"`
}

type LockEntry struct {
	Hash  string `json:"hash"`
	Bytes int64  `json:"bytes"`
}

type Snippet struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Hash      string `json:"hash"`
	Stale     bool   `json:"stale"`
	Text      string `json:"text"`
}

type IndexResult struct {
	Configured bool `json:"configured"`
	Indexed    int  `json:"indexed"`
}

func Search(root, query string, limit int) ([]Snippet, error) {
	files, configured, err := configuredFiles(root)
	if err != nil || !configured {
		return nil, err
	}
	lock, _ := readLock(root)
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	var out []Snippet
	for _, file := range files {
		snips, err := snippetsForFile(root, file, terms, lock)
		if err != nil {
			return nil, err
		}
		out = append(out, snips...)
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
	}
	return out, nil
}

func Index(root string) (IndexResult, error) {
	files, configured, err := configuredFiles(root)
	if err != nil || !configured {
		return IndexResult{Configured: configured}, err
	}
	lock := Lock{
		Version:   1,
		IndexedAt: time.Now().UTC().Format(time.RFC3339),
		Artifacts: map[string]LockEntry{},
	}
	for _, file := range files {
		rel, hash, size, err := hashFile(root, file)
		if err != nil {
			return IndexResult{}, err
		}
		lock.Artifacts[rel] = LockEntry{Hash: hash, Bytes: size}
	}
	if err := writeLock(root, lock); err != nil {
		return IndexResult{}, err
	}
	return IndexResult{Configured: true, Indexed: len(lock.Artifacts)}, nil
}

func configuredFiles(root string) ([]string, bool, error) {
	cfg, ok, err := LoadConfig(root)
	if err != nil || !ok {
		return nil, ok, err
	}
	var entries []string
	entries = append(entries, cfg.Paths...)
	for _, a := range cfg.Artifacts {
		entries = append(entries, a.Path)
	}
	seen := map[string]bool{}
	var files []string
	for _, entry := range entries {
		found, err := expandEntry(root, entry)
		if err != nil {
			return nil, true, err
		}
		for _, f := range found {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	return files, true, nil
}

func LoadConfig(root string) (Config, bool, error) {
	path := filepath.Join(root, ConfigFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, true, nil
}

func expandEntry(root, entry string) ([]string, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil, nil
	}
	if strings.ContainsAny(entry, "*?[") {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(entry)))
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", entry, err)
		}
		return filterFiles(root, matches)
	}
	abs, err := safeAbs(root, entry)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return filterFiles(root, []string{abs})
	}
	var files []string
	err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && shouldSkipDir(d.Name()) && path != abs {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", abs, err)
	}
	return filterFiles(root, files)
}

func filterFiles(root string, paths []string) ([]string, error) {
	var out []string
	for _, path := range paths {
		abs, err := safeAbs(root, path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() > MaxFile || !isTextArtifact(abs) {
			continue
		}
		out = append(out, abs)
	}
	return out, nil
}

func safeAbs(root, path string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(rootAbs, filepath.FromSlash(path))
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("context artifact path escapes project root: %s", path)
	}
	return abs, nil
}

func snippetsForFile(root, file string, terms []string, lock Lock) ([]Snippet, error) {
	rel, hash, _, err := hashFile(root, file)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", file, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", file, err)
	}
	stale := lock.Artifacts == nil || lock.Artifacts[rel].Hash != hash
	for i, line := range lines {
		if lineMatches(line, terms) {
			start := max(0, i-1)
			end := min(len(lines)-1, i+1)
			return []Snippet{{
				Path: rel, StartLine: start + 1, EndLine: end + 1,
				Hash: hash, Stale: stale, Text: strings.Join(lines[start:end+1], "\n"),
			}}, nil
		}
	}
	return nil, nil
}

func hashFile(root, file string) (string, string, int64, error) {
	abs, err := safeAbs(root, file)
	if err != nil {
		return "", "", 0, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("read %s: %w", abs, err)
	}
	sum := sha256.Sum256(data)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", "", 0, err
	}
	return filepath.ToSlash(rel), hex.EncodeToString(sum[:]), int64(len(data)), nil
}

func readLock(root string) (Lock, error) {
	data, err := os.ReadFile(filepath.Join(root, LockFile))
	if errors.Is(err, os.ErrNotExist) {
		return Lock{Artifacts: map[string]LockEntry{}}, nil
	}
	if err != nil {
		return Lock{}, err
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return Lock{}, err
	}
	if lock.Artifacts == nil {
		lock.Artifacts = map[string]LockEntry{}
	}
	return lock, nil
}

func writeLock(root string, lock Lock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, LockFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s to %s: %w", tmp, path, err)
	}
	return nil
}

func queryTerms(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	var out []string
	for _, part := range parts {
		if len(part) >= 2 {
			out = append(out, part)
		}
	}
	return out
}

func lineMatches(line string, terms []string) bool {
	lower := strings.ToLower(line)
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".gg", ".gsd", "node_modules", "vendor", "dist", "build", ".next", ".cache":
		return true
	default:
		return false
	}
}

func isTextArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".yaml", ".yml", ".json", ".toml", ".graphql", ".gql", ".proto", ".sql", ".env", ".example":
		return true
	default:
		return strings.HasSuffix(path, ".env.example")
	}
}
