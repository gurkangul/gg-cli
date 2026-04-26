package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/scrub"
)

// ErrGitleaksAlreadyInstalled is returned by runDoctorInstallSecretScanner
// when the binary already exists at the target path. Callers (e.g. CI) can
// check for this sentinel to distinguish "already present" from a real error.
var ErrGitleaksAlreadyInstalled = errors.New("gitleaks already installed")

// gitleaksVersion is the single pinned release. Update this constant and
// re-run `gg doctor --install-secret-scanner` to upgrade. The installer
// always verifies the downloaded archive against the checksums.txt file
// published alongside each release on GitHub.
const gitleaksVersion = "v8.21.2"

// gitleaksBinPath returns the full path where gg installs the gitleaks binary.
func gitleaksBinPath() (string, error) {
	shared, err := config.SharedDir()
	if err != nil {
		return "", fmt.Errorf("shared dir: %w", err)
	}
	return filepath.Join(shared, "bin", "gitleaks"), nil
}

// gitleaksPlatformKey returns the GOOS/GOARCH slug used in GitHub release URLs.
func gitleaksPlatformKey() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "darwin" && goarch == "arm64":
		return "darwin_arm64", nil
	case goos == "darwin" && goarch == "amd64":
		return "darwin_x64", nil
	case goos == "linux" && goarch == "arm64":
		return "linux_arm64", nil
	case goos == "linux" && goarch == "amd64":
		return "linux_x64", nil
	case goos == "windows" && goarch == "amd64":
		return "windows_x64", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s — download gitleaks manually from https://github.com/gitleaks/gitleaks/releases", goos, goarch)
	}
}

// runDoctorInstallSecretScanner downloads and installs the pinned gitleaks
// binary into ~/.gg/bin/gitleaks. It fetches the checksums.txt published
// alongside the release, verifies the SHA-256, and refuses to silently
// overwrite an existing binary.
func runDoctorInstallSecretScanner() error {
	fmt.Println("GG Doctor — install secret scanner (gitleaks)")
	fmt.Println(strings.Repeat("─", 50))

	binPath, err := gitleaksBinPath()
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(binPath); statErr == nil {
		fmt.Printf("  ⚠ gitleaks already installed at %s\n", binPath)
		fmt.Println("    Remove it manually and re-run to reinstall.")
		out, runErr := exec.Command(binPath, "version").Output()
		if runErr == nil {
			installed := strings.TrimSpace(string(out))
			fmt.Printf("    installed version: %s  (pinned: %s)\n", installed, gitleaksVersion)
		}
		// Check if a different gitleaks is also available on PATH.
		if pathBin, lookErr := exec.LookPath("gitleaks"); lookErr == nil && pathBin != binPath {
			pathOut, _ := exec.Command(pathBin, "version").Output()
			pathVer := strings.TrimSpace(string(pathOut))
			fmt.Printf("    PATH gitleaks: %s (version: %s)\n", pathBin, pathVer)
			if pathVer != strings.TrimPrefix(gitleaksVersion, "v") && pathVer != gitleaksVersion {
				fmt.Println("    ⚠ PATH version differs from gg-pinned version — gg commands use the pinned binary")
			}
		}
		return fmt.Errorf("%w at %s", ErrGitleaksAlreadyInstalled, binPath)
	}

	platformKey, err := gitleaksPlatformKey()
	if err != nil {
		return err
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}

	versionNum := strings.TrimPrefix(gitleaksVersion, "v")
	archiveName := fmt.Sprintf("gitleaks_%s_%s.%s", versionNum, platformKey, ext)
	baseURL := fmt.Sprintf("https://github.com/gitleaks/gitleaks/releases/download/%s", gitleaksVersion)
	archiveURL := baseURL + "/" + archiveName
	checksumsURL := fmt.Sprintf("%s/gitleaks_%s_checksums.txt", baseURL, versionNum)

	client := &http.Client{Timeout: 120 * time.Second}

	// 1. Download checksums.txt to get the expected SHA for this archive.
	fmt.Printf("  ↓ fetching checksums.txt ...\n")
	expectedSHA, err := fetchExpectedSHA(client, checksumsURL, archiveName)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	fmt.Printf("  ✓ expected SHA-256 for %s: %s...\n", archiveName, expectedSHA[:16])

	// 2. Download the archive.
	fmt.Printf("  ↓ downloading %s ...\n", archiveName)
	fmt.Printf("    from: %s\n", archiveURL)

	tmpFile, err := os.CreateTemp("", "gitleaks-*."+ext)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	resp, err := client.Get(archiveURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("download gitleaks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download gitleaks: HTTP %d from %s", resp.StatusCode, archiveURL)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, h), resp.Body); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("flush archive: %w", err)
	}

	// 3. Verify checksum.
	gotSHA := hex.EncodeToString(h.Sum(nil))
	if gotSHA != expectedSHA {
		return fmt.Errorf(
			"checksum mismatch for %s:\n  got:    %s\n  expect: %s\n  Possible MITM or corrupted download — aborting",
			archiveName, gotSHA, expectedSHA,
		)
	}
	fmt.Printf("  ✓ checksum verified\n")

	// 4. Extract the binary.
	binDir := filepath.Dir(binPath)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	if err := extractGitleaksBinary(tmpFile.Name(), binPath, ext); err != nil {
		return fmt.Errorf("extract gitleaks: %w", err)
	}
	if err := os.Chmod(binPath, 0o755); err != nil {
		return fmt.Errorf("chmod gitleaks: %w", err)
	}

	// 5. Smoke-test the installed binary.
	out, runErr := exec.Command(binPath, "version").Output()
	if runErr != nil {
		return fmt.Errorf("gitleaks installed but failed to run: %w", runErr)
	}
	installedVer := strings.TrimSpace(string(out))
	fmt.Printf("  ✓ gitleaks installed at %s (version: %s)\n", binPath, installedVer)
	fmt.Println()
	fmt.Printf("  Next steps:\n")
	fmt.Printf("    gg doctor --check-secrets          # scan this repo\n")
	fmt.Printf("    gg doctor --install-task-hooks     # install pre-commit hook\n")
	return nil
}

// fetchExpectedSHA downloads the checksums.txt file and returns the SHA-256
// hex string for archiveName. Returns an error when the file cannot be fetched
// or archiveName is not listed.
func fetchExpectedSHA(client *http.Client, url, archiveName string) (string, error) {
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: "<sha256>  <filename>" (two spaces as separator, standard sha256sum output)
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		if parts[1] == archiveName {
			return strings.ToLower(parts[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("%q not found in checksums.txt", archiveName)
}

// extractGitleaksBinary extracts the gitleaks binary from a tar.gz or zip archive.
func extractGitleaksBinary(archivePath, destPath, ext string) error {
	switch ext {
	case "tar.gz":
		return extractTarGz(archivePath, destPath)
	case "zip":
		return extractZip(archivePath, destPath)
	default:
		return fmt.Errorf("unsupported archive format: %s", ext)
	}
}

// extractTarGz extracts the gitleaks binary from a tar.gz archive using the
// system `tar` command, which is universally available on Linux/macOS.
func extractTarGz(archivePath, destPath string) error {
	destDir := filepath.Dir(destPath)
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", destDir, "gitleaks")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar extract: %w", err)
	}
	extracted := filepath.Join(destDir, "gitleaks")
	if extracted != destPath {
		if err := os.Rename(extracted, destPath); err != nil {
			return fmt.Errorf("rename gitleaks binary: %w", err)
		}
	}
	return nil
}

// extractZip extracts the gitleaks.exe from a zip archive using the system
// `unzip` command (Windows path).
func extractZip(archivePath, destPath string) error {
	destDir := filepath.Dir(destPath)
	binaryName := "gitleaks.exe"
	cmd := exec.Command("unzip", "-o", archivePath, binaryName, "-d", destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unzip extract: %w", err)
	}
	extracted := filepath.Join(destDir, binaryName)
	if extracted != destPath {
		if err := os.Rename(extracted, destPath); err != nil {
			return fmt.Errorf("rename gitleaks binary: %w", err)
		}
	}
	return nil
}

// runDoctorCheckSecrets runs gitleaks against the repo. It exposes two modes:
//   - staged scan:  staged/working-tree files via gitleaks detect --no-git
//   - history scan: full git history via gitleaks detect
//
// Falls back to the narrow-regex scanner from internal/scrub when gitleaks is
// not installed, with a clear warning about reduced coverage.
func runDoctorCheckSecrets(staged, history bool) error {
	fmt.Println("GG Doctor — check secrets")
	fmt.Println(strings.Repeat("─", 50))

	// Locate gitleaks.
	binPath, err := gitleaksBinPath()
	if err != nil {
		return err
	}

	gitleaksAvailable := false
	if _, statErr := os.Stat(binPath); statErr == nil {
		gitleaksAvailable = true
	} else if pathBin, lookErr := exec.LookPath("gitleaks"); lookErr == nil {
		binPath = pathBin
		gitleaksAvailable = true
	}

	if !gitleaksAvailable {
		fmt.Fprintln(os.Stderr, "  ⚠ gitleaks binary not found")
		fmt.Fprintln(os.Stderr, "    Run: gg doctor --install-secret-scanner")
		fmt.Fprintln(os.Stderr, "    Falling back to narrow-regex scan (less thorough than gitleaks):")
		return runNarrowSecretScan()
	}

	// Default: run both staged and history scans unless caller restricts.
	runStaged := staged || (!staged && !history)
	runHistory := history || (!staged && !history)

	problems := 0

	if runStaged {
		fmt.Println("\nStaged / working-tree scan (gitleaks detect --no-git):")
		if err := runGitleaksScan(binPath, false); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ staged scan: %v\n", err)
			problems++
		}
	}

	if runHistory {
		fmt.Println("\nFull history scan (gitleaks detect):")
		if err := runGitleaksScan(binPath, true); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ history scan: %v\n", err)
			problems++
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	if problems == 0 {
		fmt.Println("✓ No secrets detected.")
		return nil
	}
	return fmt.Errorf("%d scan(s) found secrets — see output above", problems)
}

// runGitleaksScan invokes gitleaks with the appropriate flags.
// history=false → --no-git (staged/working-tree only)
// history=true  → full git history
func runGitleaksScan(binPath string, history bool) error {
	root, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	// Write findings to a temp JSON report in os.TempDir() — NOT the repo root.
	// Writing to the repo root causes the next --no-git scan to re-scan the file
	// itself, producing false-positive findings on its own content.
	reportFile, err := os.CreateTemp("", "gitleaks-report-*.json")
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	reportPath := reportFile.Name()
	reportFile.Close()
	defer os.Remove(reportPath)

	args := []string{
		"detect", "--no-banner", "--exit-code", "1",
		"--report-format", "json", "--report-path", reportPath,
	}
	if !history {
		args = append(args, "--no-git")
	}

	tomlPath := filepath.Join(root, ".gitleaks.toml")
	if _, statErr := os.Stat(tomlPath); statErr == nil {
		args = append(args, "--config", tomlPath)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	if runErr == nil {
		fmt.Println("  ✓ clean")
		return nil
	}

	if ee, ok := runErr.(*exec.ExitError); ok {
		switch ee.ExitCode() {
		case 1:
			// Findings — print the JSON report in a human-readable form.
			printGitleaksFindings(reportPath)
			return fmt.Errorf("secrets found (exit 1)")
		case 126, 127:
			return fmt.Errorf("gitleaks not executable or not found: %w", runErr)
		}
	}
	return fmt.Errorf("gitleaks error: %w", runErr)
}

// gitleaksFinding mirrors the fields we care about from gitleaks' JSON output.
type gitleaksFinding struct {
	RuleID    string `json:"RuleID"`
	File      string `json:"File"`
	StartLine int    `json:"StartLine"`
	Match     string `json:"Match"`
}

// printGitleaksFindings reads the JSON report written by gitleaks and prints
// a compact human-readable summary. Falls back to a terse message on parse errors.
func printGitleaksFindings(reportPath string) {
	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil || len(data) == 0 {
		fmt.Fprintln(os.Stderr, "  (finding details unavailable — check gitleaks output above)")
		return
	}

	var findings []gitleaksFinding
	if jsonErr := json.Unmarshal(data, &findings); jsonErr != nil {
		fmt.Fprintf(os.Stderr, "  (could not parse gitleaks report: %v)\n", jsonErr)
		return
	}
	for _, f := range findings {
		m := f.Match
		if len(m) > 80 {
			m = m[:77] + "..."
		}
		fmt.Fprintf(os.Stderr, "  ✗ %-12s  %s:%d  match: %s\n", f.RuleID, f.File, f.StartLine, m)
	}
}

// runNarrowSecretScan is the fallback when gitleaks is unavailable. It uses
// the internal/scrub regex patterns against staged files and emits a warning
// that the scan is less thorough than gitleaks.
func runNarrowSecretScan() error {
	fmt.Printf("  patterns checked: %s\n\n", strings.Join(scrub.Patterns(), ", "))

	staged, err := stagedFileContents()
	if err != nil {
		return fmt.Errorf("list staged files: %w", err)
	}
	if len(staged) == 0 {
		fmt.Println("  (no staged files)")
		return nil
	}

	problems := 0
	for file, contents := range staged {
		_, count := scrub.String(contents)
		if count > 0 {
			fmt.Fprintf(os.Stderr, "  ✗ %s — %d secret pattern(s) found\n", file, count)
			problems++
		}
	}

	fmt.Println()
	if problems == 0 {
		fmt.Println("  ✓ no patterns matched in staged files (narrow scan only)")
		return nil
	}
	return fmt.Errorf("narrow scan found %d file(s) with secret patterns", problems)
}

// stagedFileContents returns a map of path → content for all staged files.
func stagedFileContents() (map[string]string, error) {
	out, err := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMRT").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff-index: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := make(map[string]string, len(lines))
	for _, line := range lines {
		f := strings.TrimSpace(line)
		if f == "" {
			continue
		}
		blob, blobErr := exec.Command("git", "show", ":"+f).Output()
		if blobErr != nil {
			continue
		}
		result[f] = string(blob)
	}
	return result, nil
}
