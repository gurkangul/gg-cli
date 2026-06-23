package cmd

import (
	"context"
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
	Long: `Downloads the latest gg release binary for this platform from GitHub,
verifies its checksum, and atomically replaces the running executable.

For a local gg-cli checkout with unreleased changes, use:

  gg update --from-source

Source/dev builds are NOT clobbered by the release binary unless --force is
passed, so a maintainer's --from-source build survives. After updating, gg
refreshes registered project artifacts with system sync unless --skip-sync is
passed. Network access happens only when this command (or 'gg update check')
runs, or on a throttled session-start auto-update check.`,
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
		"install the latest release binary even on a source/dev build or when already up to date")
	updateCmd.Flags().BoolVar(&updateFromSource, "from-source", false,
		"rebuild and install gg from the local gg-cli source checkout instead of the latest public release")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false,
		"accepted for automation compatibility; gg update never prompts")
	updateCmd.AddCommand(updateCheckCmd)
	rootCmd.AddCommand(updateCmd)
}

type updateInfo struct {
	Current     string   `json:"current"`
	Latest      string   `json:"latest"`
	Update      bool     `json:"update_available"`
	Comparable  bool     `json:"comparable"`
	DevBuild    bool     `json:"dev_build"`
	InstallHint string   `json:"install_hint,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

type updateResult struct {
	updateInfo
	Installed  bool     `json:"installed"`
	Synced     bool     `json:"synced"`
	SkipReason string   `json:"skip_reason,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

func runUpdateCheck(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), updateCheckTimeout)
	defer cancel()

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	renderUpdateInfo(buildUpdateInfo(resolveVersion(), rel.TagName))
	return nil
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	if updateFromSource {
		return runUpdateFromSource(cmd)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	current := resolveVersion()
	info := buildUpdateInfo(current, rel.TagName)
	result := updateResult{updateInfo: info, Warnings: append([]string(nil), info.Warnings...)}

	// Dev-build guard: never clobber a source/dev build with a release binary
	// unless the user explicitly forces it.
	if info.DevBuild && !updateForce {
		result.SkipReason = "source/dev build"
		if jsonOutput {
			return writeJSON(result)
		}
		fmt.Printf("source build (%s); rebuild with `gg update --from-source`, or `gg update --force` to install the latest release binary.\n", current)
		return nil
	}

	// Up-to-date guard (skip when forcing).
	if !updateForce && info.Comparable && !info.Update {
		result.SkipReason = "up to date"
		if jsonOutput {
			return writeJSON(result)
		}
		fmt.Printf("already up to date (%s)\n", current)
		return nil
	}

	out, err := selfUpdateToRelease(ctx, rel, current)
	if err != nil {
		return err
	}
	result.Installed = true
	if !jsonOutput {
		fmt.Printf("updated %s → %s\n", out.OldVersion, out.NewVersion)
		if warning := installedBinaryWarning(out.ExePath); warning != "" {
			result.Warnings = append(result.Warnings, warning)
			fmt.Println("warning: " + warning)
		}
	} else if warning := installedBinaryWarning(out.ExePath); warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}

	if updateSkipSync {
		if jsonOutput {
			return writeJSON(result)
		}
		return nil
	}

	stdout := os.Stdout
	if jsonOutput {
		stdout = os.Stderr
	}
	if !jsonOutput {
		fmt.Println("\nRefreshing gg-managed project artifacts...")
	}
	if err := runUpdatedGG(ctx, out.ExePath, stdout, os.Stderr, "system", "sync"); err != nil {
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

// buildUpdateInfo compares the current build against a release tag. It marks
// dev/source builds (which must not be auto-clobbered) and computes whether a
// newer release is available.
func buildUpdateInfo(current, latestTag string) updateInfo {
	info := updateInfo{
		Current:     strings.TrimSpace(current),
		Latest:      strings.TrimSpace(latestTag),
		InstallHint: "gg update",
		DevBuild:    isDevBuild(current),
	}
	if info.Current == "" {
		info.Current = "unknown"
	}
	cmp, ok := compareSemver(info.Current, info.Latest)
	info.Comparable = ok
	info.Update = ok && cmp < 0
	return info
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
	if len(info.Warnings) > 0 {
		for _, warning := range info.Warnings {
			fmt.Printf("warning: %s\n", warning)
		}
	}
	switch {
	case info.DevBuild:
		fmt.Println("status:  source/dev build — `gg update --from-source` or `gg update --force`")
	case info.Update:
		fmt.Println("status:  update available")
		fmt.Printf("run:     %s\n", info.InstallHint)
	case !info.Comparable:
		fmt.Println("status:  current version is not comparable to public semver")
	default:
		fmt.Println("status:  up to date")
	}
}

// emitUpdateNotice prints an opt-in (GG_UPDATE_CHECK) one-shot notice when a
// newer release exists. Silent on errors, on fresh binaries, and on dev builds
// (a dev build has no release to "update" to without --force). This is distinct
// from the throttled session-start auto-update.
func emitUpdateNotice(w io.Writer) {
	if !envTruthy(os.Getenv(updateCheckEnvVar)) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return
	}
	info := buildUpdateInfo(resolveVersion(), rel.TagName)
	if info.DevBuild || !info.Update {
		return
	}
	fmt.Fprintf(w, "─── GG UPDATE AVAILABLE: %s → %s ───\n", info.Current, info.Latest)
	fmt.Fprintln(w, "  run: gg update")
	fmt.Fprintln(w)
}
