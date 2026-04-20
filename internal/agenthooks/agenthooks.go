// Package agenthooks installs agent-side configuration that enforces gg-cli
// usage at the start of every agent session.
//
// gg does not ship a daemon and cannot intercept agent behavior at runtime.
// What this package CAN do is write config files that each agent reads on
// its own, using the agent's native hook/rule mechanism. Each writer is
// idempotent and constrained to the minimum change required for the
// target agent to self-enforce the gg protocol.
//
// Tiers (strength of enforcement — honest about limits):
//
//	TierHard      — the agent runtime refuses to proceed until the gg
//	                command runs (Claude Code SessionStart hook, Cursor
//	                alwaysApply rule).
//	TierSoft      — the agent reads the injected content but may ignore
//	                it (Codex via AGENTS.md managed-block).
//	TierFallback  — no config surface; best-effort via shell wrapper
//	                printed for the user to install manually.
//
// Concrete installers live in sibling files (claude.go, cursor.go, …).
// Add a new agent by appending an Installer to the package registry.
package agenthooks

import (
	"fmt"
	"path/filepath"
)

// Tier ranks the enforcement strength of an installer. See package doc.
type Tier int

const (
	TierUnknown  Tier = 0
	TierHard     Tier = 1
	TierSoft     Tier = 2
	TierFallback Tier = 3
)

func (t Tier) String() string {
	switch t {
	case TierHard:
		return "hard"
	case TierSoft:
		return "soft"
	case TierFallback:
		return "fallback"
	default:
		return "unknown"
	}
}

// Action is the outcome of a single Install call for a single agent.
type Action string

const (
	ActionCreated     Action = "created"      // config file did not exist, created fresh
	ActionUpdated     Action = "updated"      // existing config was merged with gg block
	ActionUpToDate    Action = "up-to-date"   // config already contains the gg block
	ActionNotDetected Action = "not-detected" // agent not present in project, skipped
	ActionFailed      Action = "failed"       // install raised an error (see Result.Err)
	ActionDryRun      Action = "dry-run"      // --dry-run: would have applied
	ActionSuggested   Action = "suggested"    // global install detected, project hook missing — suggestion printed
)

// Options controls installer side-effects.
type Options struct {
	// DryRun reports what would change without writing anything.
	DryRun bool
	// Force bypasses the auto-detection and installs even when the agent is
	// not present in the project. Useful when the user wants to pre-configure
	// for an agent they plan to use.
	Force bool
}

// Result is one installer outcome.
type Result struct {
	AgentName string
	// DisplayName overrides AgentName in report output. Used when the logical
	// display label differs from the registry key (e.g. "AGENTS.md" for codex).
	DisplayName string
	Tier        Tier
	Action      Action
	// Path is the config file the installer wrote or would have written.
	// Empty for ActionNotDetected.
	Path string
	// Notes are human-readable advisories (e.g. "added read: AGENTS.md").
	Notes []string
	// Err is non-nil iff Action == ActionFailed.
	Err error
}

// Installer is the contract each supported agent implements. Implementations
// live in the per-agent files (claude.go, cursor.go, …) and register
// themselves via the package registry below.
type Installer interface {
	// Name returns the stable installer key ("claude", "cursor", …).
	Name() string
	// Tier reports the enforcement strength this installer provides.
	Tier() Tier
	// Detect reports whether this agent appears to be in use in projectRoot.
	Detect(projectRoot string) bool
	// Install writes or merges the agent's config. Must be idempotent:
	// re-running on an already-installed project produces ActionUpToDate.
	Install(projectRoot string, opts Options) (Result, error)
	// ContractPath returns the path to the file that holds the managed
	// contract block for this agent. Used by --check-contract drift detection.
	ContractPath(projectRoot string) string
}

// registry is the ordered list of known installers. InstallDetected and
// InstallNamed iterate in this order so report output is stable.
var registry = []Installer{
	&claudeInstaller{},
	&cursorInstaller{},
	&codexInstaller{},
	&bmadInstaller{},
	&gsdInstaller{},
}

// AllNames returns the registry keys in registration order.
func AllNames() []string {
	out := make([]string, 0, len(registry))
	for _, inst := range registry {
		out = append(out, inst.Name())
	}
	return out
}

// InstallDetected runs every registered installer against projectRoot,
// skipping those that fail Detect. Results are returned in registration
// order; a Result is always produced for every registered agent so the
// caller can render a complete report (including "not-detected" rows).
func InstallDetected(projectRoot string, opts Options) []Result {
	results := make([]Result, 0, len(registry))
	for _, inst := range registry {
		r := runOne(inst, projectRoot, opts, false)
		results = append(results, r)
	}
	return results
}

// InstallNamed installs only the agents whose names appear in names.
// Unknown names become failed results so the caller can surface them.
// Detection is bypassed (Force semantics) — the user explicitly asked.
func InstallNamed(projectRoot string, names []string, opts Options) []Result {
	results := make([]Result, 0, len(names))
	for _, name := range names {
		inst := findInstaller(name)
		if inst == nil {
			results = append(results, Result{
				AgentName: name,
				Action:    ActionFailed,
				Err:       fmt.Errorf("unknown agent %q (known: %v)", name, AllNames()),
			})
			continue
		}
		results = append(results, runOne(inst, projectRoot, opts, true))
	}
	return results
}

func findInstaller(name string) Installer {
	for _, inst := range registry {
		if inst.Name() == name {
			return inst
		}
	}
	return nil
}

// runOne dispatches a single installer and defensively translates panics
// or errors into an ActionFailed result so one bad installer cannot tank
// the whole report.
func runOne(inst Installer, projectRoot string, opts Options, forceDetect bool) Result {
	if forceDetect {
		// User explicitly named this agent — treat as Force so Install can
		// skip suggestion logic and write unconditionally.
		opts.Force = true
	}
	if !opts.Force && !inst.Detect(projectRoot) {
		return Result{
			AgentName: inst.Name(),
			Tier:      inst.Tier(),
			Action:    ActionNotDetected,
		}
	}
	r, err := inst.Install(projectRoot, opts)
	r.AgentName = inst.Name()
	r.Tier = inst.Tier()
	if err != nil {
		r.Action = ActionFailed
		r.Err = err
	}
	return r
}

// pathIn is a small helper used by installers to stay rooted under
// projectRoot and avoid accidentally writing outside it.
func pathIn(projectRoot string, parts ...string) string {
	all := append([]string{projectRoot}, parts...)
	return filepath.Join(all...)
}
