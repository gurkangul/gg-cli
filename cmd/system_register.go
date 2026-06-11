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

	if prevRoot, dup := reg.DuplicateRootFor(cfg.ProjectID, root); dup {
		fmt.Printf("⚠ project_id %s was already registered at %s — re-pointing it to %s\n", cfg.ProjectID, prevRoot, root)
	}
	reg.Add(cfg.ProjectID, root)
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Printf("✓ registered %s (%s)\n", filepath.Base(root), cfg.ProjectID)
	return nil
}

// registryEntryStatus classifies one registry entry by stat'ing its root and
// parsing .gg/config.yaml. Shared by --list and the stale-count surfacing so
// both report identical ok/missing/invalid semantics. "ok" means the config
// parses (config.ParseFromGGDir) — a registered project is healthy even if it
// omits runtime-only fields; "invalid" is reserved for unreadable/unparseable
// config, which a blind --prune must not silently remove.
func registryEntryStatus(e config.ProjectEntry) config.EntryStatus {
	return e.Status(config.ParseFromGGDir)
}

// registryStats aggregates ok/missing/invalid counts across the whole registry.
func registryStats(reg *config.Registry) config.RegistryStats {
	return reg.Stats(config.ParseFromGGDir)
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
	fmt.Printf("%-8s  %-20s  %-36s  %s\n", "STATUS", "NAME", "PROJECT_ID", "ROOT")
	var stats config.RegistryStats
	stats.Total = len(projects)
	for _, p := range projects {
		st := registryEntryStatus(p)
		switch st {
		case config.EntryMissing:
			stats.Missing++
		case config.EntryInvalid:
			stats.Invalid++
		default:
			stats.OK++
		}
		fmt.Printf("%-8s  %-20s  %-36s  %s\n", string(st), p.Name, p.ID, p.Root)
	}
	printRegistryStaleHint(stats)
	return nil
}

// printRegistryStaleHint emits the aggregate counts line and, when there are
// stale (missing) entries, points at the real prune command. Invalid entries
// are surfaced separately because a blind prune does not fix them.
func printRegistryStaleHint(s config.RegistryStats) {
	fmt.Printf("\nRegistry: %d project%s (%d ok, %d stale, %d invalid)\n",
		s.Total, plural(s.Total, "", "s"), s.OK, s.Missing, s.Invalid)
	if s.Missing > 0 {
		fmt.Println("Run `gg system register --prune` to remove stale entries (missing root directory).")
	}
	if s.Invalid > 0 {
		fmt.Println("Invalid entries have an unreadable .gg/config.yaml — inspect them; --prune will not remove them.")
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
