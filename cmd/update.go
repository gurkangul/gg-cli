package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	ggModulePath      = "github.com/gurkangul/gg-cli"
	ggInstallPkgBase  = "github.com/gurkangul/gg-cli/cmd/gg"
	updateCheckEnvVar = "GG_UPDATE_CHECK"
)

var (
	updateCheckTimeout = 5 * time.Second
	updateSkipSync     bool
	updateForce        bool
	updateYes          bool
	updateFromSource   bool

	updateCommandContext = exec.CommandContext
	updateLookPath       = exec.LookPath
	updateExecutable     = os.Executable
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update gg to the latest public release",
	Long: `Checks the latest public gg module version and, when needed, runs:

  go install github.com/gurkangul/gg-cli/cmd/gg@<latest-version>

For a local gg-cli checkout with unreleased changes, use:

  gg update --from-source

After installing, gg refreshes registered project artifacts with system sync
unless --skip-sync is passed. Network access happens only when this command
or 'gg update check' is run explicitly.`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

var updateCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check whether a newer public gg release is available",
	Args:  cobra.NoArgs,
	RunE:  runUpdateCheck,
}

func init() {
	updateCmd.Flags().BoolVar(&updateSkipSync, "skip-sync", false,
		"skip post-install managed artifact sync")
	updateCmd.Flags().BoolVar(&updateForce, "force", false,
		"run go install even when the current version appears up to date")
	updateCmd.Flags().BoolVar(&updateFromSource, "from-source", false,
		"rebuild and install gg from the local gg-cli source checkout instead of the latest public release")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false,
		"accepted for automation compatibility; gg update never prompts")
	updateCmd.AddCommand(updateCheckCmd)
	rootCmd.AddCommand(updateCmd)
}

type updateInfo struct {
	Current      string   `json:"current"`
	Latest       string   `json:"latest"`
	Update       bool     `json:"update_available"`
	Comparable   bool     `json:"comparable"`
	InstallHint  string   `json:"install_hint,omitempty"`
	LatestSource string   `json:"latest_source,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

type updateResult struct {
	updateInfo
	Installed     bool     `json:"installed"`
	Synced        bool     `json:"synced"`
	SkipReason    string   `json:"skip_reason,omitempty"`
	InstallReason string   `json:"install_reason,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

type goListModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

func runUpdateCheck(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), updateCheckTimeout)
	defer cancel()

	info, err := checkLatestGG(ctx, rootCmd.Version)
	if err != nil {
		return err
	}
	renderUpdateInfo(info)
	return nil
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	if updateFromSource {
		return runUpdateFromSource(cmd)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	info, err := checkLatestGG(ctx, rootCmd.Version)
	if err != nil {
		return err
	}
	result := updateResult{updateInfo: info, Warnings: append([]string(nil), info.Warnings...)}
	install, reason := shouldInstallUpdate(info, updateForce)
	if !install {
		result.SkipReason = reason
		if jsonOutput {
			return writeJSON(result)
		}
		renderUpdateInfo(info)
		fmt.Println("gg is already up to date.")
		return nil
	}

	result.InstallReason = reason
	if !jsonOutput {
		renderUpdateInfo(info)
	}
	if !info.Comparable && !updateForce && !jsonOutput {
		fmt.Println("Current version is not a released semver build; installing latest anyway.")
	}

	installPkg := ggInstallPackage(info)
	installDirect := ggInstallUseDirect(info)
	if !jsonOutput {
		fmt.Printf("\nInstalling: go install %s\n", installPkg)
		if installDirect {
			fmt.Println("Using GOPROXY=direct because the direct lookup returned the selected latest release.")
		}
	}
	out := os.Stdout
	if jsonOutput {
		out = os.Stderr
	}
	if err := runGoInstall(ctx, installPkg, installDirect, out, os.Stderr); err != nil {
		return err
	}
	result.Installed = true
	if !jsonOutput {
		fmt.Println("✓ gg install command completed")
	}

	target, targetErr := goInstallBinaryPath(ctx)
	if targetErr == nil {
		if !jsonOutput {
			fmt.Printf("installed binary: %s\n", target)
		}
		expected := info.Latest
		if v, vErr := binaryVersion(ctx, target); vErr != nil {
			warning := fmt.Sprintf("could not verify installed binary version at %s: %v", target, vErr)
			result.Warnings = append(result.Warnings, warning)
			if !jsonOutput {
				fmt.Println("warning: " + warning)
			}
		} else if cmp, ok := compareSemver(v, expected); !ok || cmp < 0 {
			warning := fmt.Sprintf("installed binary at %s reports %s, expected at least %s", target, v, expected)
			result.Warnings = append(result.Warnings, warning)
			if !jsonOutput {
				fmt.Println("warning: " + warning)
			}
		}
		if warning := installedBinaryWarning(target); warning != "" {
			result.Warnings = append(result.Warnings, warning)
			if !jsonOutput {
				fmt.Println("warning: " + warning)
			}
		}
	} else {
		warning := fmt.Sprintf("could not resolve Go install target: %v", targetErr)
		result.Warnings = append(result.Warnings, warning)
		if !jsonOutput {
			fmt.Println("warning: " + warning)
		}
	}

	if updateSkipSync {
		if jsonOutput {
			return writeJSON(result)
		}
		return nil
	}

	syncBin := "gg"
	if targetErr == nil {
		syncBin = target
	}
	if !jsonOutput {
		fmt.Println("\nRefreshing gg-managed project artifacts...")
	}
	if err := runUpdatedGG(ctx, syncBin, out, os.Stderr, "system", "sync"); err != nil {
		warning := fmt.Sprintf("system sync failed: %v; run manually: gg system sync", err)
		result.Warnings = append(result.Warnings, warning)
		if jsonOutput {
			return writeJSON(result)
		}
		fmt.Printf("warning: system sync failed: %v\n", err)
		fmt.Println("         run manually: gg system sync")
		return nil
	}
	result.Synced = true
	if jsonOutput {
		return writeJSON(result)
	}
	return nil
}

func shouldInstallUpdate(info updateInfo, force bool) (bool, string) {
	if force {
		return true, "forced"
	}
	if !info.Comparable {
		return true, "current version is not comparable to public semver"
	}
	if info.Update {
		return true, "update available"
	}
	return false, "up to date"
}

func ggInstallPackage(info updateInfo) string {
	latest := strings.TrimSpace(info.Latest)
	if latest == "" {
		latest = "latest"
	}
	return ggInstallPkgBase + "@" + latest
}

func ggInstallUseDirect(info updateInfo) bool {
	return info.LatestSource == "go-list-direct"
}

func checkLatestGG(ctx context.Context, current string) (updateInfo, error) {
	latest, source, warnings, err := latestGGModuleVersion(ctx)
	if err != nil {
		return updateInfo{}, err
	}
	info := updateInfo{
		Current:      strings.TrimSpace(current),
		Latest:       latest,
		InstallHint:  "gg update",
		LatestSource: source,
		Warnings:     warnings,
	}
	if info.Current == "" {
		info.Current = "unknown"
	}
	cmp, ok := compareSemver(info.Current, info.Latest)
	info.Comparable = ok
	info.Update = ok && cmp < 0
	return info, nil
}

func latestGGModuleVersion(ctx context.Context) (string, string, []string, error) {
	proxyVersion, proxyErr := latestGGModuleVersionVia(ctx, false)
	directVersion, directErr := latestGGModuleVersionVia(ctx, true)

	switch {
	case proxyErr != nil && directErr != nil:
		return "", "", nil, fmt.Errorf("check latest gg version: proxy: %v; direct: %v", proxyErr, directErr)
	case proxyErr != nil:
		return directVersion, "go-list-direct", []string{fmt.Sprintf("Go proxy latest lookup failed; used GOPROXY=direct: %v", proxyErr)}, nil
	case directErr != nil:
		return proxyVersion, "go-list-proxy", []string{fmt.Sprintf("GOPROXY=direct latest lookup failed; Go proxy may be stale: %v", directErr)}, nil
	}

	cmp, ok := compareSemver(proxyVersion, directVersion)
	if ok && cmp < 0 {
		return directVersion, "go-list-direct", []string{fmt.Sprintf("Go proxy reported %s but GOPROXY=direct reported %s; using direct to avoid proxy cache lag", proxyVersion, directVersion)}, nil
	}
	if ok && cmp > 0 {
		return proxyVersion, "go-list-proxy", []string{fmt.Sprintf("GOPROXY=direct reported older version %s; using Go proxy result %s", directVersion, proxyVersion)}, nil
	}
	return proxyVersion, "go-list-proxy", nil, nil
}

func latestGGModuleVersionVia(ctx context.Context, direct bool) (string, error) {
	cmd := updateCommandContext(ctx, "go", "list", "-m", "-json", ggModulePath+"@latest")
	if direct {
		cmd.Env = append(os.Environ(), "GOPROXY=direct")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			if strings.Contains(detail, "Authentication failed") ||
				strings.Contains(detail, "Repository not found") ||
				strings.Contains(detail, "terminal prompts disabled") ||
				strings.Contains(detail, "private repository") {
				detail += "\nrepository must be public, or your Go/Git credentials must allow reading it"
			}
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", fmt.Errorf("%w", err)
	}
	var mod goListModule
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", fmt.Errorf("parse go list output: %w", err)
	}
	if mod.Version == "" {
		return "", fmt.Errorf("go list returned no version for %s", ggModulePath)
	}
	return mod.Version, nil
}

func runGoInstall(ctx context.Context, pkg string, direct bool, stdout, stderr io.Writer) error {
	cmd := updateCommandContext(ctx, "go", "install", pkg)
	if direct {
		cmd.Env = append(os.Environ(), "GOPROXY=direct")
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	return nil
}

func runUpdatedGG(ctx context.Context, bin string, stdout, stderr io.Writer, args ...string) error {
	cmd := updateCommandContext(ctx, bin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", bin, strings.Join(args, " "), err)
	}
	return nil
}

func binaryVersion(ctx context.Context, bin string) (string, error) {
	cmd := updateCommandContext(ctx, bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func goInstallBinaryPath(ctx context.Context) (string, error) {
	gobin, err := goEnv(ctx, "GOBIN")
	if err != nil {
		return "", err
	}
	if gobin != "" {
		return filepath.Join(gobin, "gg"), nil
	}
	gopath, err := goEnv(ctx, "GOPATH")
	if err != nil {
		return "", err
	}
	if gopath == "" {
		return "", fmt.Errorf("go env GOPATH returned empty value")
	}
	first := strings.Split(gopath, string(os.PathListSeparator))[0]
	return filepath.Join(first, "bin", "gg"), nil
}

func goEnv(ctx context.Context, key string) (string, error) {
	cmd := updateCommandContext(ctx, "go", "env", key)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func installedBinaryWarning(target string) string {
	target = filepath.Clean(target)
	if resolved, rErr := filepath.EvalSymlinks(target); rErr == nil {
		target = resolved
	}
	if pathGG, pErr := updateLookPath("gg"); pErr == nil && pathGG != "" {
		if resolved, rErr := filepath.EvalSymlinks(pathGG); rErr == nil {
			pathGG = resolved
		}
		if !samePathString(filepath.Clean(pathGG), target) {
			return fmt.Sprintf("PATH resolves gg to %s; ensure %s is first on PATH", pathGG, filepath.Dir(target))
		}
	}
	return ""
}

func samePathString(a, b string) bool {
	aa, aErr := filepath.Abs(a)
	bb, bErr := filepath.Abs(b)
	if aErr == nil {
		a = aa
	}
	if bErr == nil {
		b = bb
	}
	return a == b
}

func renderUpdateInfo(info updateInfo) {
	if jsonOutput {
		_ = writeJSON(info)
		return
	}
	fmt.Println("Update Check")
	fmt.Println("──────────────────────────────────────────────────")
	fmt.Printf("current: %s\n", info.Current)
	fmt.Printf("latest:  %s\n", info.Latest)
	if info.LatestSource != "" {
		fmt.Printf("source:  %s\n", info.LatestSource)
	}
	if len(info.Warnings) > 0 {
		for _, warning := range info.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
	}
	switch {
	case !info.Comparable:
		fmt.Println("status:  current version is not comparable to public semver")
	case info.Update:
		fmt.Println("status:  update available")
		fmt.Printf("run:     %s\n", info.InstallHint)
	default:
		fmt.Println("status:  up to date")
	}
}

func emitUpdateNotice(w io.Writer) {
	if !envTruthy(os.Getenv(updateCheckEnvVar)) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	info, err := checkLatestGG(ctx, rootCmd.Version)
	if err != nil || !info.Update {
		return
	}
	fmt.Fprintf(w, "─── GG UPDATE AVAILABLE: %s → %s ───\n", info.Current, info.Latest)
	fmt.Fprintln(w, "  run: gg update")
	fmt.Fprintln(w)
}
