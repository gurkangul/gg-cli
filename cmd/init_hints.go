package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/session"
)

// detectAgentHint picks the most likely "current runtime agent" from the
// install report so the paste-block example uses the right GG_AGENT value.
// First successful HARD-tier install wins; falls back to a generic placeholder.
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
	return "agent"
}

// detectLangHint returns the most likely SUPPORTED index language based on
// files in cwd, or "" when nothing matches. Empty return means "do not propose
// gg index" — caller MUST check for "" before suggesting a language.
//
// Supported langs are the ones cmd/index.go actually has indexer entry points
// for: go, python, swift, typescript. Detection of other ecosystems (.NET, Java, Rust, …)
// lives in detectUnsupportedLang for messaging only.
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
		{"Package.swift", "swift"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.lang
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			ext := filepath.Ext(name)
			if ext == ".xcodeproj" || ext == ".xcworkspace" {
				return "swift"
			}
			continue
		}
		if filepath.Ext(name) == ".swift" {
			return "swift"
		}
	}
	return ""
}

// detectUnsupportedLang returns a human-readable label for ecosystems that
// gg index does NOT yet support, but which we can still recognize (so the
// bootstrap message says "detected <lang>, code graph not yet supported"
// instead of silently defaulting to Go). Returns "" when nothing matched.
//
// File checks are done in priority order; first match wins. Glob-style
// extensions are checked via a directory scan (single os.ReadDir).
func detectUnsupportedLang(dir string) string {
	exact := []struct {
		file string
		lang string
	}{
		{"global.json", "C# / .NET"},
		{"Directory.Build.props", "C# / .NET"},
		{"pom.xml", "Java"},
		{"build.gradle", "Java"},
		{"build.gradle.kts", "Kotlin / Gradle"},
		{"Cargo.toml", "Rust"},
		{"Gemfile", "Ruby"},
		{"composer.json", "PHP"},
		{"mix.exs", "Elixir"},
	}
	for _, c := range exact {
		if _, err := os.Stat(filepath.Join(dir, c.file)); err == nil {
			return c.lang
		}
	}
	// Extension-based fallback: scan top level only — keeps cost bounded for
	// huge monorepos; nested project files are out of scope for the hint.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch ext := filepath.Ext(e.Name()); ext {
		case ".csproj", ".sln", ".fsproj", ".vbproj":
			return "C# / .NET"
		}
	}
	return ""
}

// unsupportedLangNotice builds the early upfront notice (AC-3) shown when a
// project's ecosystem is recognized but NOT supported by gg index. Pure
// formatter (no I/O) so the wording is unit-testable. Returns "" when lang is
// empty (nothing recognized — caller stays silent).
//
// The supported set listed here mirrors internal/index/runner.SupportedLangs
// (go, typescript, python, swift); keep them in sync.
func unsupportedLangNotice(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return ""
	}
	return fmt.Sprintf(
		"Detected %s — gg indexing currently supports Go, TypeScript, Python, Swift; "+
			"the code graph will be limited/skipped for this project.",
		lang,
	)
}

// printUnsupportedLangNotice prints the early unsupported-language notice when
// the project is a recognized-but-unsupported ecosystem AND no supported
// language is detected. No-op for supported languages or unrecognized projects,
// so supported and polyglot repos see nothing.
func printUnsupportedLangNotice(dir string) {
	if detectLangHint(dir) != "" {
		return // a supported language is present — indexing will work.
	}
	if notice := unsupportedLangNotice(detectUnsupportedLang(dir)); notice != "" {
		fmt.Println(notice)
		fmt.Println()
	}
}

// printBootstrapPrompt emits the paste-block users copy into their agent's
// chat to trigger gg protocol compliance on first use. When a SessionStart
// hook is also installed, the block is reinforcement; for agents without a
// hook surface (Codex, Zai), it is the primary handoff.
//
// langHint == "" means no SUPPORTED language was detected; in that case we
// skip the "Next: run gg index --lang …" line entirely (per BUG-033 fix —
// previously this defaulted to "go" and proposed go indexer on .NET/Java
// projects, which always failed). When langHint == "" but an unsupported
// ecosystem was detected (e.g. .NET), we surface that explicitly so users
// know detection ran and gg index simply does not cover their stack yet.
func printBootstrapPrompt(agentHint, langHint, unsupportedHint string, indexed bool) {
	fmt.Println()
	fmt.Println("Paste this into your AI agent's chat (works for any agent):")
	fmt.Println()
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Print(session.PasteBlock(agentHint))
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()
	if !indexed && langHint != "" {
		if langHint == "swift" {
			fmt.Println("Next: provide a compatible external `scip-swift` binary, then run `gg index --lang swift` to populate the code graph.")
		} else {
			fmt.Printf("Next: run `gg index --lang %s` to populate the code graph.\n", langHint)
		}
		fmt.Println()
	} else if !indexed && unsupportedHint != "" {
		fmt.Printf("Detected %s — `gg index` supports go, python, swift, typescript only; code graph skipped.\n", unsupportedHint)
		fmt.Println()
	}
	fmt.Println("Forgot the prompt? Run `gg doctor` — it shows it again.")
	fmt.Println("Re-install hooks later: `gg doctor --install-agent-hooks`.")
}
