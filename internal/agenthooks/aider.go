package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Aider reads .aider.conf.yml on startup. Adding AGENTS.md to the `read:`
// key makes Aider preload it into every session — effectively mandatory
// protocol visibility for the agent.
const (
	aiderConfigFile = ".aider.conf.yml"
	aiderReadKey    = "read"
	aiderReadValue  = "AGENTS.md"
)

type aiderInstaller struct{}

func (a *aiderInstaller) Name() string { return "aider" }
func (a *aiderInstaller) Tier() Tier   { return TierHard }

func (a *aiderInstaller) Detect(projectRoot string) bool {
	// Any of these indicates Aider is in use in this project.
	candidates := []string{".aider.conf.yml", ".aiderignore", ".aider.tags.cache.v4"}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(projectRoot, c)); err == nil {
			return true
		}
	}
	return false
}

func (a *aiderInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, aiderConfigFile)
	res := Result{Path: path}

	existed := true
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return res, err
		}
		existed = false
		raw = nil
	}

	var root map[string]any
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &root); err != nil {
			return res, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if root == nil {
		root = map[string]any{}
	}

	reads := aiderNormalizeRead(root[aiderReadKey])
	if containsString(reads, aiderReadValue) {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, aiderReadKey+": already contains "+aiderReadValue)
		return res, nil
	}
	reads = append(reads, aiderReadValue)
	root[aiderReadKey] = reads

	if opts.DryRun {
		res.Action = ActionDryRun
		res.Notes = append(res.Notes, "would add "+aiderReadValue+" to "+aiderReadKey)
		return res, nil
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return res, err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return res, err
	}
	if existed {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCreated
	}
	res.Notes = append(res.Notes, "added "+aiderReadValue+" to "+aiderReadKey)
	return res, nil
}

// aiderNormalizeRead converts the varied shapes the YAML decoder may
// produce for the `read:` field (nil, string, []any, []string) into a
// single []string we can dedup and append to.
func aiderNormalizeRead(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	default:
		return nil
	}
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
