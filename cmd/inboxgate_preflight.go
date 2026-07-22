package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/projectstate"
	"github.com/gurkangul/gg-cli/internal/store"
)

// resolveActorRole returns the role string to use for inbox-gate checks.
// Preference order: GG_ROLE → GG_AGENT. Returns "" when both are unset.
func resolveActorRole() string {
	if r := strings.TrimSpace(os.Getenv("GG_ROLE")); r != "" {
		return r
	}
	return strings.TrimSpace(os.Getenv("GG_AGENT"))
}

// requireAgentIdentity returns an error when both GG_ROLE and GG_AGENT are
// unset. State-changing commands must call this before writing to the store.
func requireAgentIdentity() error {
	if resolveActorRole() == "" {
		return fmt.Errorf("missing durable actor identity: set GG_AGENT=<runtime-name> (e.g. codex, cursor, gsd) so shared evidence and handoffs are attributable")
	}
	return nil
}

// runInboxGatePreflight checks whether the calling agent has unread role-targeted
// handoff messages that may contain blockers, review requests, or evidence the
// next state change needs to preserve. Returns a non-nil error if the gate
// blocks. When GG_ALLOW_INBOX_SKIP is set the bypass is logged to the bypass
// audit and the function returns nil.
//
// commandName is used in the bypass audit log (gate field).
func runInboxGatePreflight(ctx context.Context, client *store.Client, commandName string) error {
	role := resolveActorRole()
	// BUG-102: the gate must query the SAME read-model gg inbox writes. A
	// message's read_by set is keyed by identity.Agent() (resolveInboxAgent),
	// NOT by the role string — so the reader passed here must be that identity,
	// or an agent that has read its mail would still be blocked.
	reader := resolveInboxAgent()
	result, err := enforcement.CheckInboxGate(ctx, client, role, reader)
	if err != nil {
		return nil // fail open: Qdrant unreachable should not block work
	}

	if result.Bypassed {
		// Log bypass to state.json — mirrors the pattern used by pre-task-done hooks.
		logInboxBypass(commandName, result.BypassReason, role)
		return nil
	}

	if result.Blocked {
		return fmt.Errorf("%s", enforcement.FormatBlockMessage(role, result))
	}
	return nil
}

// logInboxBypass records a GG_ALLOW_INBOX_SKIP bypass event in the project
// runtime state. Best-effort — errors are silently swallowed.
func logInboxBypass(gate, reason, actor string) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return
	}
	_ = projectstate.AppendBypass(runtimeDir, "inbox-handoff:"+gate, "", actor, "", "", "")
	fmt.Fprintf(os.Stderr, "⚠ inbox handoff gate bypassed (GG_ALLOW_INBOX_SKIP=%s)\n", reason)
}
