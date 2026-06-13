package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrMissingProjectID is returned by Validate when project_id is empty —
// typically a pre-refactor config. Callers can detect this with errors.Is
// to provide tailored migration guidance.
var ErrMissingProjectID = errors.New("project_id is required")

const (
	DirName       = ".gg"
	ConfigFile    = "config.yaml"
	SharedDirName = ".gg" // ~/.gg/ for shared infrastructure
)

type QdrantConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Embedding-backend types, defaults, and validation live in embedding.go to
// keep this file focused. See EmbeddingConfig / VoyageConfig there.

type MemgraphConfig struct {
	URI      string `yaml:"uri"`      // Bolt URI, e.g. bolt://localhost:7687
	Username string `yaml:"username"` // empty string = no auth (Memgraph default)
	Password string `yaml:"password"` // empty string = no auth
}

type BackupConfig struct {
	// Enabled toggles session-start auto backup. Default true.
	Enabled *bool `yaml:"enabled,omitempty"`
	// Interval is passed to `gg brain export --if-stale=<duration>`. Default 24h.
	Interval string `yaml:"interval"`
	// Timeout limits each backup subprocess duration. Default 30s.
	Timeout string `yaml:"timeout"`
}

// HooksConfig controls post-action hook execution.
type HooksConfig struct {
	// Strict controls whether hook failures block the triggering command.
	// Default false (warn-only). Set to true in CI for enforcement.
	Strict bool `yaml:"strict"`
}

// DoctorConfig controls `gg doctor` behaviour.
type DoctorConfig struct {
	// HookInstall tunes `gg doctor --install-task-hooks`.
	HookInstall HookInstallConfig `yaml:"hook_install"`
}

// HookInstallConfig tunes the `--install-task-hooks` manifest walk.
type HookInstallConfig struct {
	// SkipDirs lists directory names the walk must never descend into
	// when hunting for go.mod / package.json. Leave empty to use the
	// built-in default (DefaultHookInstallSkipDirs).
	SkipDirs []string `yaml:"skip_dirs"`
	// MaxDepth caps how deep the walk recurses from the project root.
	// Zero means "use the built-in default" (3).
	MaxDepth int `yaml:"max_depth"`
}

// DefaultHookInstallSkipDirs lists directory names the installer never
// descends into by default. Users can override / extend via config.yaml.
// Framework build-artifact dirs are pruned so emitted `package.json`
// manifests inside bundle output (e.g. Nuxt's web/.output/server) do not
// get wired as pre-task-done hooks — a broken workspace there blocks every
// `gg task done` via npm ci -w failure (BUG-017).
var DefaultHookInstallSkipDirs = []string{
	".git", ".gg", ".gsd", ".claude",
	"node_modules", "vendor", "dist", "build",
	"_bmad", "_bmad-output",
	// Framework build outputs (carry synthetic package.json files):
	".output",     // Nuxt / Nitro
	".next",       // Next.js
	".vercel",     // Vercel framework output
	".svelte-kit", // SvelteKit
	".astro",      // Astro
	".turbo",      // Turborepo
	".nuxt",       // Nuxt dev cache
	".cache",      // Remix / generic tool cache
	"out",         // Next.js export target
}

// DefaultHookInstallMaxDepth is the default recursion cap (covers
// packages/<x>/manifest and services/<x>/manifest without a full tree walk).
const DefaultHookInstallMaxDepth = 3

// BugsConfig controls bug-fix workflow enforcement.
type BugsConfig struct {
	// RequireBrokenRef, when true, makes --repro-broken-ref mandatory on
	// every `gg bug fix` call. Default false (warn-only) for back-compat.
	RequireBrokenRef bool `yaml:"require_broken_ref"`
	// DupeThreshold is the cosine-similarity threshold above which `gg bug report`
	// warns about near-duplicate bugs. Valid range [0, 1]. Zero means "use the
	// built-in default" (0.85). Lower values cast a wider net; raise to suppress
	// false positives on small corpora.
	DupeThreshold float64 `yaml:"dupe_threshold"`
}

// TasksConfig controls task-lifecycle enforcement gates.
//
// Both flags are opt-in (default false) so existing projects and their 212
// historical task closures keep working without change. A project adopting
// the ready_for_live gate flips them together in .gg/config.yaml:
//
//	tasks:
//	  require_ready_for_live: true
//	  verifier_separation: true
//
// Rationale: dogfood audit 2026-04-19 surfaced a premature-closure pattern —
// same-actor verification, where an implementing agent marks its own work
// done on unit-test-green before any live verification. The gate structurally
// separates implementer ("ready for live") from verifier ("done").
type TasksConfig struct {
	// RequireReadyForLive, when true, refuses `gg task done` unless the task
	// has already been transitioned to status "ready_for_live" via
	// `gg task ready-for-live`. Default false for back-compat.
	RequireReadyForLive bool `yaml:"require_ready_for_live"`

	// VerifierSeparation, when true, requires `gg task done --verifier <role>`
	// and refuses when <role> matches the actor that set ready_for_live.
	// Prevents an implementer from also acting as its own verifier. Only
	// effective when RequireReadyForLive is also true.
	VerifierSeparation bool `yaml:"verifier_separation"`
}

// AuditThresholdsConfig configures the numeric thresholds used by `gg audit health`.
// All fields have built-in defaults; override in .gg/config.yaml under audit.thresholds.
type AuditThresholdsConfig struct {
	// ReopenRateWarn is the reopen-rate fraction above which gg audit health
	// emits a "stabilize before adding" warning. Default 0.20 (20%).
	ReopenRateWarn float64 `yaml:"reopen_rate_warn"`
	// ReopenRateFreeze is the reopen-rate fraction above which gg audit health
	// emits a "freeze new features" recommendation. Default 0.40 (40%).
	ReopenRateFreeze float64 `yaml:"reopen_rate_freeze"`
	// SurfacePressureP95 is the p95 distinct-files-per-fix count above which
	// gg audit health emits a "centralize common state" warning. Default 3.
	SurfacePressureP95 int `yaml:"surface_pressure_p95"`
}

// AuditConfig groups all `gg audit` behaviour knobs.
type AuditConfig struct {
	Thresholds AuditThresholdsConfig `yaml:"thresholds"`
	RepeatWork RepeatWorkConfig      `yaml:"repeat_work"`
}

// RepeatWorkConfig configures the `gg audit repeat-work` hotspot detector.
// All fields have built-in defaults; override in .gg/config.yaml under
// audit.repeat_work.
type RepeatWorkConfig struct {
	// WindowDays is the outer lookback window for tier-B/C signals. Default 7.
	WindowDays int `yaml:"window_days"`
	// RecordsThreshold is the minimum number of decisions on the same task
	// within RecordsWindowDays to trigger a tier-A signal. Default 5.
	RecordsThreshold int `yaml:"records_threshold"`
	// RecordsWindowDays is the inner window for the task-record signal.
	// Default 3 (days). Tighter than WindowDays because 5 records in 3 days
	// signals more acute thrash than 5 records in 2 weeks.
	RecordsWindowDays int `yaml:"records_window_days"`
	// ReopensThreshold is the minimum reopen_count on a single bug to
	// trigger a tier-A signal. Default 2.
	ReopensThreshold int `yaml:"reopens_threshold"`
	// FilesThreshold is the minimum distinct bugs touching the same file
	// within WindowDays to trigger a tier-B signal. Default 3.
	FilesThreshold int `yaml:"files_threshold"`
	// TagThreshold is the minimum decisions sharing a non-trivial tag
	// within WindowDays to trigger a tier-C signal. Default 5.
	TagThreshold int `yaml:"tag_threshold"`
}

// TrackerConfig declares which task-tracker is canonical for this project.
type TrackerConfig struct {
	// Canonical names the tracker agents must use. Set to "gg" to activate
	// the PreToolUse guard that blocks gsd_plan_* MCP calls. Leave empty
	// (default) to allow any tracker side-by-side.
	Canonical string `yaml:"canonical"`
}

// TelemetryConfig controls local-only usage telemetry.
type TelemetryConfig struct {
	// Enabled is a tri-state: nil (absent in YAML) → default ON, *true →
	// explicit on, *false → explicit off. Using a pointer lets the loader
	// distinguish "user wrote nothing" (keep default) from "user wrote false"
	// (explicit opt-out). Override at runtime with GG_TELEMETRY=1 or
	// GG_TELEMETRY=0 — the env var always wins. Default is ON because
	// telemetry is local-only (no network) and drives the project's own
	// dogfood North Star metric (BUG-018).
	Enabled *bool `yaml:"enabled,omitempty"`
}

type Config struct {
	// SchemaVersion stamps the .gg/config.yaml format so a future shape change
	// is detectable. Absent/zero in an older config is treated as the current
	// baseline (CurrentConfigSchemaVersion) — it never fails the load.
	// ApplyDefaults stamps it when missing. See docs/stability.md §3, §5.
	SchemaVersion int `yaml:"schema_version,omitempty"`
	// ProjectID is a unique per-project UUID used to namespace Qdrant
	// collections. Multiple projects share the same Qdrant instance but see
	// only their own decisions/tasks/messages/rejections.
	ProjectID string          `yaml:"project_id"`
	Qdrant    QdrantConfig    `yaml:"qdrant"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Memgraph  MemgraphConfig  `yaml:"memgraph"`
	Backup    BackupConfig    `yaml:"backup"`
	Hooks     HooksConfig     `yaml:"hooks"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
	Doctor    DoctorConfig    `yaml:"doctor"`
	Tracker   TrackerConfig   `yaml:"tracker"`
	// LinkedProjects lists read-only project IDs or paths that search/context
	// may consult only when the caller passes --include-linked.
	LinkedProjects []LinkedProjectConfig `yaml:"linked_projects,omitempty"`
	Bugs           BugsConfig            `yaml:"bugs"`
	Tasks          TasksConfig           `yaml:"tasks"`
	Audit          AuditConfig           `yaml:"audit"`
	// AutoIndexHooks, when explicitly set to false, opts a project out of the
	// CodeGraph git hooks that `gg init` auto-installs (pre-push/post-merge/
	// post-commit, TASK-502). Nil/absent or true = install (the default). The
	// --no-index-hooks flag and GG_NO_INDEX_HOOKS env are equivalent opt-outs.
	AutoIndexHooks *bool `yaml:"auto_index_hooks,omitempty"`
}

func DefaultConfig() *Config {
	backupEnabled := true
	return &Config{
		Qdrant: QdrantConfig{
			Host: "localhost",
			Port: 6334,
		},
		Embedding: EmbeddingConfig{
			Backend: BackendOllama,
			Host:    "http://localhost:11434",
			Model:   "nomic-embed-text",
		},
		Memgraph: MemgraphConfig{
			URI:      "bolt://localhost:7687",
			Username: "",
			Password: "",
		},
		Backup: BackupConfig{
			Enabled:  &backupEnabled,
			Interval: "24h",
			Timeout:  "30s",
		},
	}
}

func (b BackupConfig) AutoEnabled() bool {
	return b.Enabled == nil || *b.Enabled
}

func (c *Config) ApplyDefaults() {
	if c.SchemaVersion == 0 {
		// Absent/zero in an older config: stamp the current baseline so the
		// next Save persists it. Treating zero as baseline never fails load.
		c.SchemaVersion = CurrentConfigSchemaVersion
	}
	if strings.TrimSpace(c.Backup.Interval) == "" {
		c.Backup.Interval = "24h"
	}
	if strings.TrimSpace(c.Backup.Timeout) == "" {
		c.Backup.Timeout = "30s"
	}
	// Embedding backend defaults (empty Backend → offline Ollama; Voyage
	// sub-defaults stamped). See embedding.go.
	c.Embedding.applyEmbeddingDefaults()
}

// FindRoot walks up from cwd looking for a project-local .gg directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isProjectGGDir(filepath.Join(dir, DirName)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(".gg not found — run 'gg init' first")
		}
		dir = parent
	}
}

// isProjectGGDir reports whether the given path is a project-local .gg dir
// (contains config.yaml), as opposed to the shared ~/.gg/ infra dir.
// It explicitly refuses the shared dir even if the user somehow created a
// config.yaml there, preventing accidental data writes to home.
func isProjectGGDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	// Refuse if the path equals the shared infra dir (including via symlink).
	// Fail closed when HOME is unresolvable: without it we cannot prove the
	// path isn't actually ~/.gg.
	shared, err := SharedDir()
	if err != nil {
		return false
	}
	if SamePath(path, shared) {
		return false
	}
	_, err = os.Stat(filepath.Join(path, ConfigFile))
	return err == nil
}

// GGDir returns the absolute path to the project-local .gg directory.
func GGDir() (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, DirName), nil
}

// GGDirOrEmpty returns the project-local .gg directory path, or "" on error.
// Used by best-effort code paths that must not fail when the .gg dir is absent.
func GGDirOrEmpty() string {
	dir, _ := GGDir()
	return dir
}

// SharedDir returns the absolute path to ~/.gg/ — the shared infrastructure
// directory holding docker-compose.yaml and service volumes.
func SharedDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, SharedDirName), nil
}

// RuntimeDir returns the per-project runtime directory
// ~/.gg/projects/<ProjectID>/, creating it when absent. Runtime state
// (telemetry, cache) lives here so that .gg/ contains only repo-committed
// metadata.
func (c *Config) RuntimeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, SharedDirName, "projects", c.ProjectID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create runtime dir: %w", err)
	}
	return dir, nil
}

// SamePath reports whether two paths resolve to the same filesystem entry.
// It canonicalises via filepath.Abs and filepath.EvalSymlinks when possible;
// paths that don't yet exist fall back to lexical comparison.
func SamePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absA); err == nil {
		absA = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absB); err == nil {
		absB = resolved
	}
	return absA == absB
}

// Load reads the project-local config, applies env var overrides, and validates.
//
// Env var precedence (highest wins):
//
//	MEMGRAPH_PASSWORD — overrides memgraph.password
//	MEMGRAPH_USERNAME — overrides memgraph.username
//	MEMGRAPH_URI      — overrides memgraph.uri
//
// Passwords should never be stored in config.yaml. Leave memgraph.password
// empty and set MEMGRAPH_PASSWORD in the shell environment instead.
func Load() (*Config, error) {
	ggDir, err := GGDir()
	if err != nil {
		return nil, err
	}
	return LoadFromGGDir(ggDir)
}

// applyMemgraphEnvOverrides sets Memgraph credentials from environment variables
// when they are present, so that passwords are never required in config.yaml.
func applyMemgraphEnvOverrides(m *MemgraphConfig) {
	if v := os.Getenv("MEMGRAPH_PASSWORD"); v != "" {
		m.Password = v
	}
	if v := os.Getenv("MEMGRAPH_USERNAME"); v != "" {
		m.Username = v
	}
	if v := os.Getenv("MEMGRAPH_URI"); v != "" {
		m.URI = v
	}
}

// Save writes the config back to .gg/config.yaml in the current project.
func (c *Config) Save() error {
	ggDir, err := GGDir()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path := filepath.Join(ggDir, ConfigFile)
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config tmp to %s: %w", path, err)
	}
	return nil
}

// Validate ensures required fields are present and URLs/ports are well-formed.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.ProjectID) == "" {
		return fmt.Errorf("%w — run 'gg init' to generate one", ErrMissingProjectID)
	}
	if strings.TrimSpace(c.Qdrant.Host) == "" {
		return fmt.Errorf("qdrant.host is required")
	}
	if c.Qdrant.Port <= 0 || c.Qdrant.Port > 65535 {
		return fmt.Errorf("qdrant.port must be 1..65535, got %d", c.Qdrant.Port)
	}
	if err := c.Embedding.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Backup.Interval) == "" {
		return fmt.Errorf("backup.interval is required")
	}
	if _, err := time.ParseDuration(strings.TrimSpace(c.Backup.Interval)); err != nil {
		return fmt.Errorf("backup.interval must be a valid duration (e.g. 24h, 30m), got %q: %w", c.Backup.Interval, err)
	}
	if strings.TrimSpace(c.Backup.Timeout) == "" {
		return fmt.Errorf("backup.timeout is required")
	}
	if _, err := time.ParseDuration(strings.TrimSpace(c.Backup.Timeout)); err != nil {
		return fmt.Errorf("backup.timeout must be a valid duration (e.g. 30s, 2m), got %q: %w", c.Backup.Timeout, err)
	}
	return nil
}
