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
  set   — write a single field (e.g. developer.command)`,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config field (e.g. developer.command 'gsd --model openai-codex/gpt-5.3-codex')",
	Long: `Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  developer.command    — any subprocess command used for worker panes
  roles.developer.command — explicit developer role command override
  roles.reviewer.command  — reviewer/verifier role command
  developer.transport  — allowlist: cmux, side-session-prompt
  developer.agent      — deprecated legacy alias for developer.command
  developer.spawn_command — deprecated legacy alias for developer.command

Examples:
  gg config set developer.command "gsd --model openai-codex/gpt-5.3-codex"
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
	case "developer.command":
		if err := validateDeveloperCommand(value); err != nil {
			return err
		}
		cfg.Developer.Command = value
	case "roles.developer.command", "roles.reviewer.command":
		if err := validateDeveloperCommand(value); err != nil {
			return err
		}
		if cfg.Roles == nil {
			cfg.Roles = map[string]config.RoleCommandConfig{}
		}
		role := strings.TrimPrefix(strings.TrimSuffix(key, ".command"), "roles.")
		rc := cfg.Roles[role]
		rc.Command = value
		cfg.Roles[role] = rc
	case "developer.agent":
		if value == "unconfigured" {
			cfg.Developer.Agent = value
			cfg.Developer.Command = ""
		} else {
			if err := validateDeveloperCommand(value); err != nil {
				return err
			}
			cfg.Developer.Agent = value
			cfg.Developer.Command = value
		}
	case "developer.transport":
		if err := validateDeveloperTransport(value); err != nil {
			return err
		}
		cfg.Developer.Transport = value
	case "developer.spawn_command":
		if err := validateDeveloperCommand(value); err != nil {
			return err
		}
		cfg.Developer.SpawnCommand = value
		cfg.Developer.Command = value
	default:
		return fmt.Errorf("unknown config key %q — supported keys: developer.command, roles.developer.command, roles.reviewer.command, developer.transport, developer.agent, developer.spawn_command", key)
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("✓ %s = %s\n", key, value)
	return nil
}

func validateDeveloperCommand(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("invalid developer.command: command must not be empty")
	}
	return nil
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
