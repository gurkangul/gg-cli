package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/session"
)

// detectAgentHint picks the most likely "current runtime agent" from the
// install report so the paste-block example uses the right GG_AGENT value.
// First successful HARD-tier install wins; falls back to claude-code.
func detectAgentHint(results []agenthooks.Result) string {
	for _, r := range results {
		if r.Tier != agenthooks.TierHard {
			continue
		}
		if r.Action == agenthooks.ActionCreated || r.Action == agenthooks.ActionUpdated {
			switch r.AgentName {
			case "claude":
				return "claude-code"
			case "cursor":
				return "cursor"
			}
		}
	}
	return "claude-code"
}

// detectLangHint returns the most likely index language based on files in cwd.
func detectLangHint(dir string) string {
	checks := []struct {
		file string
		lang string
	}{
		{"go.mod", "go"},
		{"package.json", "typescript"},
		{"pyproject.toml", "python"},
		{"setup.py", "python"},
		{"requirements.txt", "python"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.lang
		}
	}
	return "go"
}

// printBootstrapPrompt emits the paste-block users copy into their agent's
// chat to trigger gg protocol compliance on first use. When a SessionStart
// hook is also installed, the block is reinforcement; for agents without a
// hook surface (Codex, Zai), it is the primary handoff.
func printBootstrapPrompt(agentHint, langHint string, indexed bool) {
	fmt.Println()
	fmt.Println("Paste this into your AI agent's chat (works for any agent):")
	fmt.Println()
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Print(session.PasteBlock(agentHint))
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()
	if !indexed {
		fmt.Printf("Next: run `gg index --lang %s` to populate the code graph.\n", langHint)
		fmt.Println()
	}
	fmt.Println("Forgot the prompt? Run `gg doctor` — it shows it again.")
	fmt.Println("Re-install hooks later: `gg doctor --install-agent-hooks`.")
}
