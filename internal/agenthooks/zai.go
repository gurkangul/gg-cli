package agenthooks

// Zai and generic LLM CLIs have no persistent config surface we can safely
// write to — every shell has its own rc file and the agent has no
// SessionStart-equivalent API. Rather than silently do nothing or
// clobber user shell rc files, the installer returns an advisory result
// with the exact shell-alias the user must install themselves.
//
// This keeps the "tier 3 / fallback" category visible in the install
// report without the installer overreaching.

type zaiInstaller struct{}

func (z *zaiInstaller) Name() string { return "zai" }
func (z *zaiInstaller) Tier() Tier   { return TierFallback }

// Detect always returns false: there is no reliable project-local
// artifact that identifies a Zai user. The installer is reachable only
// via --agent=zai / --force, which is exactly the right UX — users who
// don't opt in get nothing written, users who do get a clear advisory.
func (z *zaiInstaller) Detect(_ string) bool { return false }

func (z *zaiInstaller) Install(_ string, opts Options) (Result, error) {
	res := Result{
		Action: ActionUpToDate, // nothing to write
		Notes: []string{
			"zai has no project-local config surface. Add to your shell rc:",
			"  alias zai='GG_AGENT=zai zai'",
			"or prefix invocations: `GG_AGENT=zai zai …`",
			"This tags zai's gg calls as agent-initiated; the actual protocol",
			"lives in AGENTS.md (ask zai to read it at session start).",
		},
	}
	if opts.DryRun {
		res.Action = ActionDryRun
	}
	return res, nil
}
