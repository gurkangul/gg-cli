package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/gurkangul/gg-cli/internal/config"
)

var systemRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Add a project to the registry (or prune dead entries)",
	Long: `Normally 'gg init' auto-registers. Use this command for:

  - projects initialized before the registry existed (backfill)
  - rehoming a project whose root directory moved
  - removing entries that point at deleted / missing directories

Examples:
  gg system register                         (register cwd)
  gg system register --path ~/my/app         (register a specific dir)
  gg system register --prune                 (drop entries with missing roots)
  gg system register --list                  (show registry contents)
`,
	RunE: runSystemRegister,
}

var (
	systemRegisterPath  string
	systemRegisterPrune bool
	systemRegisterList  bool
)

func init() {
	systemRegisterCmd.Flags().StringVar(&systemRegisterPath, "path", "",
		"project root to register (defaults to cwd)")
	systemRegisterCmd.Flags().BoolVar(&systemRegisterPrune, "prune", false,
		"remove registry entries whose root directory no longer exists")
	systemRegisterCmd.Flags().BoolVar(&systemRegisterList, "list", false,
		"print the current registry and exit")
	systemCmd.AddCommand(systemRegisterCmd)
}

func runSystemRegister(cmd *cobra.Command, _ []string) error {
	reg, err := config.LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	if systemRegisterList {
		return printRegistryList(reg)
	}
	if systemRegisterPrune {
		return pruneRegistry(reg)
	}

	root := systemRegisterPath
	if root == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return cwdErr
		}
		root = cwd
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}

	cfgPath := filepath.Join(root, ".gg", "config.yaml")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s (run `gg init` first?): %w", cfgPath, err)
	}
	var cfg struct {
		ProjectID string `yaml:"project_id"`
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if cfg.ProjectID == "" {
		return fmt.Errorf("%s has no project_id — re-run `gg init`", cfgPath)
	}

	reg.Add(cfg.ProjectID, root)
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("✓ registered %s (%s)\n", filepath.Base(root), cfg.ProjectID)
	return nil
}

func pruneRegistry(reg *config.Registry) error {
	var removed int
	for _, p := range reg.Sorted() {
		if _, err := os.Stat(filepath.Join(p.Root, ".gg", "config.yaml")); err != nil {
			reg.Remove(p.ID)
			fmt.Printf("✗ pruned %s (%s — %v)\n", p.Name, p.Root, err)
			removed++
		}
	}
	if removed == 0 {
		fmt.Println("Registry is clean — nothing to prune.")
		return nil
	}
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("Removed %d stale entr%s.\n", removed, plural(removed, "y", "ies"))
	return nil
}

func printRegistryList(reg *config.Registry) error {
	projects := reg.Sorted()
	if len(projects) == 0 {
		fmt.Println("No projects registered.")
		return nil
	}
	fmt.Printf("%-20s  %-36s  %s\n", "NAME", "PROJECT_ID", "ROOT")
	for _, p := range projects {
		fmt.Printf("%-20s  %-36s  %s\n", p.Name, p.ID, p.Root)
	}
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
