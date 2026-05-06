package cmd

import (
	"bufio"
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
// it is not already set. It stores a generic command string instead of
// hard-coding agent/model semantics into gg.
func ensureDeveloperConfig(cmd *cobra.Command, ggDir string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Already configured — do not overwrite user's setting.
	if developerCommand(&cfg.Developer) != "" {
		return nil
	}

	cwd := filepath.Dir(ggDir)
	gsdOK := detectGSD(cwd)
	cmuxOK := detectCmux(cwd)

	if gsdOK && cmuxOK {
		cfg.Developer.Command = "gsd"
		cfg.Developer.Transport = "cmux"
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save developer config: %w", err)
		}
		fmt.Println("✓ Developer command: gsd (cmux)")
		return nil
	}

	// Non-interactive: --yes flag or non-TTY stdin defaults to unconfigured.
	if initYes || !isatty(cmd) {
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save developer config: %w", err)
		}
		fmt.Println("⚠ No developer command configured")
		fmt.Println(`  Override later: gg config set developer.command "<agent command>"`)
		return nil
	}

	// Interactive prompt.
	fmt.Println()
	fmt.Println("No developer command detected. Pick developer command:")
	fmt.Println("  [1] Enter custom command")
	fmt.Println("  [2] Use manual side session")
	fmt.Println("  [3] Skip")
	fmt.Print("Choice [3]: ")

	reader := bufio.NewReader(cmd.InOrStdin())
	choice, _ := reader.ReadString('\n')
	switch strings.TrimSpace(choice) {
	case "1":
		fmt.Print("Developer command: ")
		command, _ := reader.ReadString('\n')
		cfg.Developer.Command = strings.TrimSpace(command)
		cfg.Developer.Transport = "cmux"
		_ = cfg.Save()
		fmt.Printf("  developer.command=%s\n", cfg.Developer.Command)
	case "2":
		cfg.Developer.Transport = "side-session-prompt"
		_ = cfg.Save()
		fmt.Println("  Developer command left unconfigured (side-session-prompt)")
	default:
		_ = cfg.Save()
		fmt.Println(`  developer.command unset (override later: gg config set developer.command "<agent command>")`)
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
