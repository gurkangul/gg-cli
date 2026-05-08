package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
)

const runtimeHealthTimeout = 5 * time.Second

type spawnAgentResolution struct {
	Command string
	Source  string
	Reasons []string
}

type runtimeProfileCandidate struct {
	Name    string
	Profile config.RuntimeProfileConfig
}

func resolveSpawnAgentForRole(role, explicitCommand string) (spawnAgentResolution, error) {
	role = normalizeRuntimeRole(role)
	if role == "" {
		role = "developer"
	}
	if cmd := strings.TrimSpace(explicitCommand); cmd != "" {
		return spawnAgentResolution{Command: cmd, Source: "--agent"}, nil
	}
	if cmd := strings.TrimSpace(os.Getenv("GG_SPAWN_AGENT")); cmd != "" {
		return spawnAgentResolution{Command: cmd, Source: "GG_SPAWN_AGENT"}, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return spawnAgentResolution{}, err
	}

	profiles := runtimeProfilesForRole(cfg, role)
	var reasons []string
	for _, cand := range profiles {
		cmd := strings.TrimSpace(cand.Profile.Command)
		if cmd == "" {
			reasons = append(reasons, fmt.Sprintf("runtime profile %q skipped: command empty", cand.Name))
			continue
		}
		health := strings.TrimSpace(cand.Profile.HealthCommand)
		if health == "" {
			reasons = append(reasons, fmt.Sprintf("runtime profile %q selected without health_command; verify quota manually if the pane stalls", cand.Name))
			return spawnAgentResolution{Command: cmd, Source: "runtime_profiles." + cand.Name, Reasons: reasons}, nil
		}
		if err := runRuntimeHealthCheck(health); err != nil {
			reasons = append(reasons, fmt.Sprintf("runtime profile %q unhealthy: %v", cand.Name, err))
			continue
		}
		reasons = append(reasons, fmt.Sprintf("runtime profile %q healthy", cand.Name))
		return spawnAgentResolution{Command: cmd, Source: "runtime_profiles." + cand.Name, Reasons: reasons}, nil
	}

	if cmd := roleCommand(cfg, role); cmd != "" {
		if len(reasons) > 0 {
			reasons = append(reasons, fmt.Sprintf("falling back to legacy %s command", legacyRoleConfigKey(role)))
		}
		return spawnAgentResolution{Command: cmd, Source: legacyRoleConfigKey(role), Reasons: reasons}, nil
	}
	if len(reasons) > 0 {
		return spawnAgentResolution{Reasons: reasons}, fmt.Errorf("%s command is unconfigured and no healthy runtime profile is available — configure `runtime_profiles` or run `gg config set %s \"<agent command>\"`", role, legacyRoleConfigKey(role))
	}
	return spawnAgentResolution{}, roleCommandUnconfiguredError(role)
}

func runtimeProfilesForRole(cfg *config.Config, role string) []runtimeProfileCandidate {
	if cfg == nil || len(cfg.RuntimeProfiles) == 0 {
		return nil
	}
	var out []runtimeProfileCandidate
	for name, profile := range cfg.RuntimeProfiles {
		profileRole := normalizeRuntimeRole(profile.Role)
		if profileRole == "" {
			profileRole = "developer"
		}
		if profileRole == role {
			out = append(out, runtimeProfileCandidate{Name: name, Profile: profile})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].Profile.Priority, out[j].Profile.Priority
		if pi != pj {
			return pi < pj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func runRuntimeHealthCheck(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeHealthTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := c.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("health_command timed out after %s", runtimeHealthTimeout)
	}
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, detail)
	}
	return nil
}

func normalizeRuntimeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func legacyRoleConfigKey(role string) string {
	role = normalizeRuntimeRole(role)
	if role == "" || role == "developer" {
		return "developer.command"
	}
	return "roles." + role + ".command"
}

func printSpawnResolution(prefix string, res spawnAgentResolution) {
	if res.Source != "" {
		fmt.Printf("%s%s (source: %s)\n", prefix, res.Command, res.Source)
	}
	for _, reason := range res.Reasons {
		fmt.Printf("  - %s\n", reason)
	}
}
