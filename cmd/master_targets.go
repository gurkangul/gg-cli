package cmd

import (
	"os"
	"strings"
)

func masterMessageTargets() []string {
	role := os.Getenv("GG_MASTER_ROLE")
	if role == "" {
		role = os.Getenv("GG_ROLE")
	}
	agent := os.Getenv("GG_MASTER_AGENT")
	if agent == "" {
		agent = os.Getenv("GG_AGENT")
	}
	return uniqueNonEmpty(role, agent, "master", "claude-code")
}

func masterMessageTargetCSV() string {
	return strings.Join(masterMessageTargets(), ",")
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			v := strings.ToLower(strings.TrimSpace(part))
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
