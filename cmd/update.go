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
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	ggModulePath      = "github.com/gurkangul/gg-cli"
	ggInstallPkg      = "github.com/gurkangul/gg-cli/cmd/gg@latest"
	updateCheckEnvVar = "GG_UPDATE_CHECK"
)

var (
	updateCheckTimeout = 5 * time.Second
	updateSkipSync     bool
	updateForce        bool
	updateYes          bool

	updateCommandContext = exec.CommandContext
	updateLookPath       = exec.LookPath
	updateExecutable     = os.Executable
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update gg to the latest public release",
	Long: `Checks the latest public gg module version and, when needed, runs:

  go install github.com/gurkangul/gg-cli/cmd/gg@latest

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
	updateCmd.Flags().BoolVar(&updateYes, "yes", false,
		"non-interactive: confirm update without prompts")
	updateCmd.AddCommand(updateCheckCmd)
	rootCmd.AddCommand(updateCmd)
}

type updateInfo struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Update      bool   `json:"update_available"`
	Comparable  bool   `json:"comparable"`
	InstallHint string `json:"install_hint,omitempty"`
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
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	info, err := checkLatestGG(ctx, rootCmd.Version)
	if err != nil {
		return err
	}
	renderUpdateInfo(info)
	if !info.Update && !updateForce {
		fmt.Println("gg is already up to date.")
		return nil
	}
	if !info.Comparable && !updateForce {
		fmt.Println("Current version is not a released semver build; installing latest anyway.")
	}

	fmt.Printf("\nInstalling: go install %s\n", ggInstallPkg)
	if err := runGoInstall(ctx, ggInstallPkg); err != nil {
		return err
	}
	fmt.Println("✓ gg install command completed")

	target, targetErr := goInstallBinaryPath(ctx)
	if targetErr == nil {
		fmt.Printf("installed binary: %s\n", target)
		warnIfInstalledBinaryNotActive(target)
	} else {
		fmt.Printf("warning: could not resolve Go install target: %v\n", targetErr)
	}

	if updateSkipSync {
		return nil
	}

	syncBin := "gg"
	if targetErr == nil {
		syncBin = target
	}
	fmt.Println("\nRefreshing gg-managed project artifacts...")
	if err := runUpdatedGG(ctx, syncBin, "system", "sync"); err != nil {
		fmt.Printf("warning: system sync failed: %v\n", err)
		fmt.Println("         run manually: gg system sync")
		return nil
	}
	return nil
}

func checkLatestGG(ctx context.Context, current string) (updateInfo, error) {
	latest, err := latestGGModuleVersion(ctx)
	if err != nil {
		return updateInfo{}, err
	}
	info := updateInfo{
		Current:     strings.TrimSpace(current),
		Latest:      latest,
		InstallHint: "gg update",
	}
	if info.Current == "" {
		info.Current = "unknown"
	}
	cmp, ok := compareSemver(info.Current, info.Latest)
	info.Comparable = ok
	info.Update = ok && cmp < 0
	return info, nil
}

func latestGGModuleVersion(ctx context.Context) (string, error) {
	cmd := updateCommandContext(ctx, "go", "list", "-m", "-json", ggModulePath+"@latest")
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
			return "", fmt.Errorf("check latest gg version: %w: %s", err, detail)
		}
		return "", fmt.Errorf("check latest gg version: %w", err)
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

func runGoInstall(ctx context.Context, pkg string) error {
	cmd := updateCommandContext(ctx, "go", "install", pkg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	return nil
}

func runUpdatedGG(ctx context.Context, bin string, args ...string) error {
	cmd := updateCommandContext(ctx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

func warnIfInstalledBinaryNotActive(target string) {
	target = filepath.Clean(target)
	active, err := updateExecutable()
	if err == nil {
		if resolved, rErr := filepath.EvalSymlinks(active); rErr == nil {
			active = resolved
		}
		if resolved, rErr := filepath.EvalSymlinks(target); rErr == nil {
			target = resolved
		}
		if !samePathString(active, target) {
			fmt.Printf("warning: this process ran from %s; ensure %s is first on PATH\n", active, filepath.Dir(target))
			return
		}
	}
	if pathGG, pErr := updateLookPath("gg"); pErr == nil && !samePathString(filepath.Clean(pathGG), target) {
		fmt.Printf("warning: PATH resolves gg to %s; ensure %s is first on PATH\n", pathGG, filepath.Dir(target))
	}
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

func compareSemver(a, b string) (int, bool) {
	av, apre, ok := parseSemverCore(a)
	if !ok {
		return 0, false
	}
	bv, bpre, ok := parseSemverCore(b)
	if !ok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1, true
		case av[i] > bv[i]:
			return 1, true
		}
	}
	switch {
	case apre == bpre:
		return 0, true
	case apre == "":
		return 1, true
	case bpre == "":
		return -1, true
	case apre < bpre:
		return -1, true
	default:
		return 1, true
	}
}

func parseSemverCore(s string) ([3]int, string, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "gg version ")
	if !strings.HasPrefix(s, "v") {
		return out, "", false
	}
	s = strings.TrimPrefix(s, "v")
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	pre := ""
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		pre = s[dash+1:]
		s = s[:dash]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, "", false
		}
		out[i] = n
	}
	return out, pre, true
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

func envTruthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	switch v {
	case "yes", "y", "on":
		return true
	default:
		return false
	}
}
