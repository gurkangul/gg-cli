package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/spf13/cobra"
)

// detectGSD returns true when GSD tooling is available in the project.
// Detection: presence of .mcp.json referencing a gsd-workflow server, OR
// the gsd binary is on PATH.
func detectGSD(projectDir string) bool {
	// Check .mcp.json for a gsd-workflow entry.
	mcpPath := filepath.Join(projectDir, ".mcp.json")
	if data, err := os.ReadFile(mcpPath); err == nil {
		if strings.Contains(string(data), "gsd-workflow") {
			return true
		}
	}
	// Check for gsd binary on PATH.
	if _, err := exec.LookPath("gsd"); err == nil {
		return true
	}
	return false
}

// detectCmux returns true when cmux is available (required for GSD+cmux transport).
func detectCmux(_ string) bool {
	_, err := exec.LookPath("cmux")
	return err == nil
}

// ensureDeveloperConfig writes the developer block to .gg/config.yaml when
// it is not already set. Detects GSD + cmux and sets defaults accordingly;
// in non-interactive mode defaults to "unconfigured" with a warning.
func ensureDeveloperConfig(cmd *cobra.Command, ggDir string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Already configured — do not overwrite user's setting.
	if cfg.Developer.Agent != "" {
		return nil
	}

	cwd := filepath.Dir(ggDir)
	gsdOK := detectGSD(cwd)
	cmuxOK := detectCmux(cwd)

	if gsdOK && cmuxOK {
		cfg.Developer.Agent = "gsd-sonnet-4.6"
		cfg.Developer.Transport = "cmux"
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save developer config: %w", err)
		}
		fmt.Println("✓ Developer agent: gsd-sonnet-4.6 (cmux)")
		return nil
	}

	// Non-interactive: --yes flag or non-TTY stdin defaults to "unconfigured".
	if initYes || !isatty(cmd) {
		cfg.Developer.Agent = "unconfigured"
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save developer config: %w", err)
		}
		fmt.Println("⚠ No GSD detected. developer.agent=unconfigured")
		fmt.Println("  Override later: gg config set developer.agent gsd-sonnet-4.6")
		return nil
	}

	// Interactive prompt.
	fmt.Println()
	fmt.Println("No GSD detected. Pick developer agent:")
	fmt.Println("  [1] Install GSD (see https://github.com/getgsd/gsd-pi)")
	fmt.Println("  [2] Use Claude Sonnet 4.5 in side session (manual)")
	fmt.Println("  [3] Skip (developer.agent=unconfigured)")
	fmt.Print("Choice [3]: ")

	var choice string
	_, _ = fmt.Fscan(cmd.InOrStdin(), &choice)
	switch strings.TrimSpace(choice) {
	case "1":
		cfg.Developer.Agent = "unconfigured"
		_ = cfg.Save()
		fmt.Println("  Install GSD: https://github.com/getgsd/gsd-pi")
		fmt.Println("  Then re-run: gg config set developer.agent gsd-sonnet-4.6")
	case "2":
		cfg.Developer.Agent = "claude-sonnet-4.5"
		cfg.Developer.Transport = "side-session-prompt"
		_ = cfg.Save()
		fmt.Println("  Developer agent set to claude-sonnet-4.5 (side-session-prompt)")
	default:
		cfg.Developer.Agent = "unconfigured"
		_ = cfg.Save()
		fmt.Println("  developer.agent=unconfigured (override later: gg config set developer.agent <id>)")
	}
	return nil
}

// isatty reports whether the command's stdin is a terminal.
func isatty(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
