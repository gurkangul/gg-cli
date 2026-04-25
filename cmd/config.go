package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect or modify project configuration",
	Long: `Read and write .gg/config.yaml fields.

Subcommands:
  set   — write a single field (e.g. developer.agent)`,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config field (e.g. developer.agent gsd-sonnet-4.6)",
	Long: `Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  developer.agent      — allowlist: gsd-sonnet-4.6, claude-sonnet-4.5, claude-opus-4.7, unconfigured
  developer.transport  — allowlist: cmux, side-session-prompt
  developer.spawn_command — any string (custom agent launch command override)

Examples:
  gg config set developer.agent gsd-sonnet-4.6
  gg config set developer.transport cmux`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

func init() {
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigSet(_ *cobra.Command, args []string) error {
	key := strings.TrimSpace(args[0])
	value := strings.TrimSpace(args[1])

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch key {
	case "developer.agent":
		if err := validateDeveloperAgent(value); err != nil {
			return err
		}
		cfg.Developer.Agent = value
	case "developer.transport":
		if err := validateDeveloperTransport(value); err != nil {
			return err
		}
		cfg.Developer.Transport = value
	case "developer.spawn_command":
		cfg.Developer.SpawnCommand = value
	default:
		return fmt.Errorf("unknown config key %q — supported keys: developer.agent, developer.transport, developer.spawn_command", key)
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("✓ %s = %s\n", key, value)
	return nil
}

func validateDeveloperAgent(v string) error {
	for _, allowed := range config.ValidDeveloperAgents {
		if v == allowed {
			return nil
		}
	}
	return fmt.Errorf("invalid developer.agent %q — allowed: %s",
		v, strings.Join(config.ValidDeveloperAgents, ", "))
}

func validateDeveloperTransport(v string) error {
	allowed := []string{"cmux", "side-session-prompt"}
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("invalid developer.transport %q — allowed: %s",
		v, strings.Join(allowed, ", "))
}
