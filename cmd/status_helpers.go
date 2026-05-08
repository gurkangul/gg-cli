package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

func pct(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}

func fmtCount(n uint64, err error) string {
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}

// renderRolesBlock formats the Roles section for gg status, showing the
// developer command, transport, and queue state. rtDir may be empty (queue line
// defaults to "not started" when the runtime dir is unavailable).
func renderRolesBlock(dev *config.DeveloperConfig, rtDir string) string {
	cfg := &config.Config{Developer: config.DeveloperConfig{}}
	if dev == nil {
		return ""
	}
	cfg.Developer = *dev
	return renderRolesBlockFromConfig(cfg, rtDir)
}

func renderRolesBlockFromConfig(cfg *config.Config, rtDir string) string {
	if cfg == nil {
		return ""
	}
	dev := &cfg.Developer
	developerLine := developerCommand(dev)
	developerTransport := dev.Transport
	if cfg.Roles != nil {
		if rc, ok := cfg.Roles["developer"]; ok && strings.TrimSpace(rc.Command) != "" && rc.Command != "unconfigured" {
			developerLine = rc.Command
			developerTransport = rc.Transport
		}
	}
	if developerLine == "" {
		developerLine = "⚠ unconfigured"
	} else if developerTransport != "" {
		developerLine = developerLine + " (" + developerTransport + ")"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Roles\n  Developer  %s\n", developerLine)
	for _, role := range sortedRoleNames(cfg.Roles) {
		if role == "developer" {
			continue
		}
		rc := cfg.Roles[role]
		if strings.TrimSpace(rc.Command) == "" {
			continue
		}
		line := rc.Command
		if rc.Transport != "" {
			line += " (" + rc.Transport + ")"
		}
		fmt.Fprintf(&b, "  %-9s %s\n", roleDisplayName(role), line)
	}
	if len(cfg.RuntimeProfiles) > 0 {
		for _, name := range sortedRuntimeProfileNames(cfg.RuntimeProfiles) {
			p := cfg.RuntimeProfiles[name]
			role := p.Role
			if role == "" {
				role = "developer"
			}
			health := "no health_command"
			if strings.TrimSpace(p.HealthCommand) != "" {
				health = "health_command"
			}
			fmt.Fprintf(&b, "  Profile   %s → %s p=%d (%s)\n", name, role, p.Priority, health)
		}
	}
	queueLine := queueStatusLine(rtDir)
	fmt.Fprintf(&b, "  Queue      %s\n\n", queueLine)
	return b.String()
}

func roleDisplayName(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "Role"
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func sortedRoleNames(roles map[string]config.RoleCommandConfig) []string {
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedRuntimeProfileNames(profiles map[string]config.RuntimeProfileConfig) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func roleCommand(cfg *config.Config, role string) string {
	if cfg == nil {
		return ""
	}
	role = strings.TrimSpace(role)
	if cfg.Roles != nil {
		if rc, ok := cfg.Roles[role]; ok && rc.Command != "" && rc.Command != "unconfigured" {
			return rc.Command
		}
	}
	if role == "" || role == "developer" {
		return developerCommand(&cfg.Developer)
	}
	return ""
}

func developerCommand(dev *config.DeveloperConfig) string {
	if dev == nil {
		return ""
	}
	if dev.Command != "" && dev.Command != "unconfigured" {
		return dev.Command
	}
	if dev.SpawnCommand != "" && dev.SpawnCommand != "unconfigured" {
		return dev.SpawnCommand
	}
	if dev.Agent == "gsd-sonnet-4.6" {
		return "gsd"
	}
	return ""
}

// queueStatusLine returns a one-line summary of the current queue state.
// rtDir may be empty; in that case "not started" is returned.
func queueStatusLine(rtDir string) string {
	if rtDir == "" {
		return "not started — single-pane mode"
	}
	sess, err := spawn.ReadQueue(rtDir)
	if err != nil {
		// ErrNoQueue or any read error → not started.
		return "not started — single-pane mode (run gg spawn queue start for parallel pickup)"
	}
	if sess.Paused {
		return fmt.Sprintf("paused (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
	}
	if sess.Done {
		return fmt.Sprintf("complete (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
	}
	return fmt.Sprintf("running (completed: %d, skipped: %d)", len(sess.Completed), len(sess.Skipped))
}
