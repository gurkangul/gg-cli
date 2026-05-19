package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

const ggModulePrefix = "module github.com/gurkangul/gg-cli"

// runDoctorCheckBinary checks whether the installed gg binary is older than
// the latest commit in the gg-cli source directory. Emits a WARN when the
// binary is stale; emits a SKIP with info when the source dir cannot be found
// (i.e. gg-cli is not locally cloned); emits an ERROR when the binary is not
// resolvable via os.Executable.
//
// With fix=true, triggers `go install ./cmd/gg` in the found source directory.
func runDoctorCheckBinary(fix bool) error {
	fmt.Println("Binary Freshness Check")
	fmt.Println(strings.Repeat("─", 50))

	// --- Step 1: Locate the installed binary ---
	binPath, err := os.Executable()
	if err != nil {
		fmt.Printf("  ✗ %-28s %s\n", "gg binary", "cannot resolve path: "+err.Error())
		return fmt.Errorf("binary check failed: %w", err)
	}
	// Follow symlinks so we stat the real file.
	if resolved, lErr := filepath.EvalSymlinks(binPath); lErr == nil {
		binPath = resolved
	}

	binInfo, err := os.Lstat(binPath)
	if err != nil {
		fmt.Printf("  ✗ %-28s %s\n", "gg binary", "stat failed: "+err.Error())
		return fmt.Errorf("binary check failed: %w", err)
	}
	binMtime := binInfo.ModTime()

	// --- Step 2: Locate gg-cli source directory ---
	srcDir, srcErr := findGGCLISourceDir()
	if srcErr != nil {
		fmt.Printf("  ~ %-28s %s\n", "source dir", "not found — skipping freshness check")
		fmt.Printf("    (gg-cli not locally cloned, or not registered: %v)\n", srcErr)
		fmt.Printf("  ✓ %-28s %s\n", "gg binary", binPath)
		return nil
	}

	// --- Step 3: Get the HEAD commit timestamp from the source dir ---
	commitEpoch, gitErr := headCommitEpoch(srcDir)
	if gitErr != nil {
		fmt.Printf("  ~ %-28s %s\n", "source dir", "git log failed: "+gitErr.Error())
		fmt.Printf("    source: %s\n", srcDir)
		fmt.Printf("  ✓ %-28s %s\n", "gg binary", binPath)
		return nil
	}
	commitTime := time.Unix(commitEpoch, 0)
	dirtyPaths, dirtyErr := buildAffectingDirtyPaths(srcDir)
	if dirtyErr != nil {
		fmt.Printf("  ~ %-28s %s\n", "source state", "could not inspect working tree: "+dirtyErr.Error())
	}
	sourceDirty := len(dirtyPaths) > 0

	// --- Step 4: Compare ---
	fmt.Printf("  binary:   %s\n", binPath)
	fmt.Printf("  mtime:    %s\n", binMtime.UTC().Format(time.RFC3339))
	fmt.Printf("  HEAD at:  %s  (%s)\n", srcDir, commitTime.UTC().Format(time.RFC3339))
	if sourceDirty {
		fmt.Printf("  source:   %d uncommitted build-affecting change(s)\n", len(dirtyPaths))
		for _, p := range previewDirtyPaths(dirtyPaths, 5) {
			fmt.Printf("            %s\n", p)
		}
		if len(dirtyPaths) > 5 {
			fmt.Printf("            … and %d more\n", len(dirtyPaths)-5)
		}
	}
	fmt.Println()

	stale := binMtime.Before(commitTime)

	if fix {
		if !stale && !sourceDirty {
			fmt.Printf("  ~ %-28s %s\n", "fix-binary", "binary is already up to date — nothing to do")
			return nil
		}
		fmt.Printf("  ↓ %-28s running go install ./cmd/gg ...\n", "fix-binary")
		if installErr := goInstallGG(srcDir); installErr != nil {
			fmt.Printf("  ✗ %-28s %s\n", "fix-binary", installErr.Error())
			return fmt.Errorf("go install failed: %w", installErr)
		}
		if sourceDirty {
			fmt.Printf("  ✓ %-28s binary rebuilt from current working tree; commit or re-run after edits to prove freshness\n", "fix-binary")
			return nil
		}
		fmt.Printf("  ✓ %-28s binary rebuilt\n", "fix-binary")
		return nil
	}

	if stale {
		lag := commitTime.Sub(binMtime).Round(time.Minute)
		fmt.Printf("  ~ %-28s binary is %s behind HEAD — run: go install ./cmd/gg\n",
			"gg binary", lag)
		fmt.Printf("    or: gg doctor --check-binary --fix-binary\n")
		// Advisory warn — does not count as a hard failure for `gg doctor` default run.
		return nil
	}
	if sourceDirty {
		fmt.Printf("  ~ %-28s source has uncommitted build-affecting changes — binary freshness cannot be proven\n", "gg binary")
		fmt.Printf("    run: go install ./cmd/gg after edits are final, then re-check\n")
		return nil
	}

	fmt.Printf("  ✓ %-28s up to date\n", "gg binary")
	return nil
}

// doctorCheckBinaryAdvisory is the lightweight variant wired into the default
// `gg doctor` run. It emits a warn line but never increments report.problems,
// keeping the binary-freshness check advisory (non-blocking) in the default run.
func doctorCheckBinaryAdvisory(report *doctorReport) {
	binPath, err := os.Executable()
	if err != nil {
		report.warn("gg binary", "cannot resolve executable path")
		return
	}
	if resolved, lErr := filepath.EvalSymlinks(binPath); lErr == nil {
		binPath = resolved
	}

	binInfo, err := os.Lstat(binPath)
	if err != nil {
		report.warn("gg binary", "stat failed: "+err.Error())
		return
	}
	binMtime := binInfo.ModTime()

	srcDir, srcErr := findGGCLISourceDir()
	if srcErr != nil {
		// Source not locally cloned — not an error, just skip.
		report.ok("gg binary", "installed (source not local — skipping freshness)")
		return
	}

	commitEpoch, gitErr := headCommitEpoch(srcDir)
	if gitErr != nil {
		report.warn("gg binary", "could not read HEAD commit timestamp")
		return
	}
	commitTime := time.Unix(commitEpoch, 0)
	dirtyPaths, dirtyErr := buildAffectingDirtyPaths(srcDir)
	if dirtyErr != nil {
		report.warn("gg binary", "could not inspect source working tree")
		return
	}

	if binMtime.Before(commitTime) {
		lag := commitTime.Sub(binMtime).Round(time.Minute)
		report.warn("gg binary", fmt.Sprintf("stale by %s — run `gg doctor --check-binary --fix-binary`", lag))
		return
	}
	if len(dirtyPaths) > 0 {
		report.warn("gg binary", fmt.Sprintf("source has %d uncommitted build-affecting change(s); freshness cannot be proven", len(dirtyPaths)))
		return
	}
	report.ok("gg binary", "up to date")
}

func buildAffectingDirtyPaths(srcDir string) ([]string, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all", "--", ".")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := porcelainPath(line)
		if isBuildAffectingPath(path) {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func porcelainPath(line string) string {
	path := strings.TrimSpace(line)
	if len(line) >= 4 {
		path = strings.TrimSpace(line[3:])
	}
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		path = parts[len(parts)-1]
	}
	return strings.Trim(path, "\"")
}

func isBuildAffectingPath(path string) bool {
	p := filepath.ToSlash(strings.TrimSpace(path))
	if p == "" {
		return false
	}
	if p == "go.mod" || p == "go.sum" || p == "CHANGELOG.md" {
		return true
	}
	if strings.HasPrefix(p, "cmd/") || strings.HasPrefix(p, "internal/") {
		return true
	}
	return !strings.Contains(p, "/") && strings.HasSuffix(p, ".go")
}

func previewDirtyPaths(paths []string, limit int) []string {
	if limit <= 0 || len(paths) <= limit {
		return paths
	}
	return paths[:limit]
}

// findGGCLISourceDir scans the project registry looking for a locally cloned
// gg-cli repo (identified by its go.mod module line). Falls back to checking
// the current working directory so developers running from within the repo
// still get a useful result.
func findGGCLISourceDir() (string, error) {
	// Check cwd first — common case for the gg-cli developer.
	if dir, ok := isGGCLISourceDir("."); ok {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return dir, nil
		}
		return abs, nil
	}

	reg, err := config.LoadRegistry()
	if err != nil {
		return "", fmt.Errorf("load registry: %w", err)
	}

	for _, entry := range reg.Sorted() {
		if _, ok := isGGCLISourceDir(entry.Root); ok {
			return entry.Root, nil
		}
	}
	return "", fmt.Errorf("gg-cli source not found in registry")
}

// isGGCLISourceDir returns (dir, true) when dir contains a go.mod that
// declares the gg-cli module. The returned dir is the input unchanged.
func isGGCLISourceDir(dir string) (string, bool) {
	gomod := filepath.Join(dir, "go.mod")
	f, err := os.Open(gomod)
	if err != nil {
		return dir, false
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), ggModulePrefix) {
			return dir, true
		}
	}
	return dir, false
}

// headCommitEpoch runs `git log -1 --format=%ct HEAD` in srcDir and returns
// the Unix timestamp of the latest commit.
func headCommitEpoch(srcDir string) (int64, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%ct", "HEAD")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return 0, fmt.Errorf("git log returned empty output")
	}
	var epoch int64
	if _, err := fmt.Sscan(raw, &epoch); err != nil {
		return 0, fmt.Errorf("parse epoch %q: %w", raw, err)
	}
	return epoch, nil
}

// goInstallGG runs `go install ./cmd/gg` in srcDir.
func goInstallGG(srcDir string) error {
	cmd := exec.Command("go", "install", "./cmd/gg")
	cmd.Dir = srcDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
