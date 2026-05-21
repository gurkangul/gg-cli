package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

type updateFromSourceResult struct {
	SourceDir string   `json:"source_dir"`
	Target    string   `json:"target,omitempty"`
	Installed bool     `json:"installed"`
	Synced    bool     `json:"synced"`
	Warnings  []string `json:"warnings,omitempty"`
}

func runUpdateFromSource(cmd *cobra.Command) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	srcDir, err := findGGCLISourceDir()
	if err != nil {
		return fmt.Errorf("find local gg-cli source: %w", err)
	}
	result := updateFromSourceResult{SourceDir: srcDir}
	out := os.Stdout
	target, targetErr := sourceInstallTarget(ctx)
	if targetErr != nil {
		return fmt.Errorf("resolve source install target: %w", targetErr)
	}
	result.Target = target
	if jsonOutput {
		out = os.Stderr
	} else {
		fmt.Printf("Installing from source: %s\n", srcDir)
		fmt.Printf("Running: go build -o %s ./cmd/gg\n", target)
	}
	if err := runGoBuildSourceToTarget(ctx, srcDir, target, out, os.Stderr); err != nil {
		return err
	}
	result.Installed = true

	if warning := installedBinaryWarning(target); warning != "" {
		result.Warnings = append(result.Warnings, warning)
		if !jsonOutput {
			fmt.Println("warning: " + warning)
		}
	}
	if !jsonOutput {
		fmt.Printf("installed binary: %s\n", target)
	}

	if !updateSkipSync {
		if !jsonOutput {
			fmt.Println("\nRefreshing gg-managed project artifacts...")
		}
		if err := runUpdatedGG(ctx, target, out, os.Stderr, "system", "sync"); err != nil {
			warning := fmt.Sprintf("system sync failed: %v; run manually: gg system sync", err)
			result.Warnings = append(result.Warnings, warning)
			if !jsonOutput {
				fmt.Printf("warning: %s\n", warning)
			}
		} else {
			result.Synced = true
		}
	}
	if jsonOutput {
		return writeJSON(result)
	}
	return nil
}

func runGoInstallSource(ctx context.Context, srcDir string, stdout, stderr io.Writer) (string, error) {
	target, err := sourceInstallTarget(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve source install target: %w", err)
	}
	if err := runGoBuildSourceToTarget(ctx, srcDir, target, stdout, stderr); err != nil {
		return "", err
	}
	return target, nil
}

func sourceInstallTarget(ctx context.Context) (string, error) {
	if path, err := updateLookPath("gg"); err == nil && path != "" {
		if resolved, rErr := filepath.EvalSymlinks(path); rErr == nil {
			path = resolved
		}
		return filepath.Clean(path), nil
	}
	return goInstallBinaryPath(ctx)
}

func runGoBuildSourceToTarget(ctx context.Context, srcDir, target string, stdout, stderr io.Writer) error {
	if target == "" {
		return fmt.Errorf("empty install target")
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create install directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gg-install-*")
	if err != nil {
		return fmt.Errorf("create temporary binary in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temporary binary %s: %w", tmpPath, closeErr)
	}
	_ = os.Remove(tmpPath)

	cmd := updateCommandContext(ctx, "go", "build", "-o", tmpPath, "./cmd/gg")
	cmd.Dir = srcDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("go build ./cmd/gg failed: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temporary binary %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("install %s: %w", target, err)
	}
	return nil
}
