package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect or modify project configuration",
	Long: `Read and write .gg/config.yaml fields.

Subcommands:
  set   — write a single field`,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config field",
	Long: `Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  backup.enabled      — true/false session-start auto-backup toggle
  backup.interval     — staleness threshold for brain export (e.g. 24h, 6h)
  backup.timeout      — per-backup subprocess timeout (e.g. 30s, 2m)

Examples:
  gg config set backup.interval 6h`,
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
	return config.WithWriteLock(func() error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		switch key {
		case "backup.enabled":
			enabled, parseErr := strconv.ParseBool(value)
			if parseErr != nil {
				return fmt.Errorf("invalid backup.enabled %q — use true or false", value)
			}
			cfg.Backup.Enabled = &enabled
		case "backup.interval":
			if err := validateDurationConfigValue("backup.interval", value); err != nil {
				return err
			}
			cfg.Backup.Interval = value
		case "backup.timeout":
			if err := validateDurationConfigValue("backup.timeout", value); err != nil {
				return err
			}
			cfg.Backup.Timeout = value
		default:
			return fmt.Errorf("unknown config key %q — supported keys: backup.enabled, backup.interval, backup.timeout", key)
		}

		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		fmt.Printf("✓ %s = %s\n", key, value)
		return nil
	})
}

func validateDurationConfigValue(key, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("invalid %s: duration must not be empty", key)
	}
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("invalid %s %q — use a Go duration like 24h, 6h, 30m, or 30s: %w", key, value, err)
	}
	return nil
}
