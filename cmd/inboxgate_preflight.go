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
		return fmt.Errorf("identify yourself: export GG_AGENT=<runtime-name> (e.g. codex, cursor, gsd)")
	}
	return nil
}

// runInboxGatePreflight checks whether the calling agent has unread role-targeted
// inbox messages that must be handled before proceeding. Returns a non-nil error
// if the gate blocks. When GG_ALLOW_INBOX_SKIP is set the bypass is logged to
// the bypass audit and the function returns nil.
//
// commandName is used in the bypass audit log (gate field).
func runInboxGatePreflight(ctx context.Context, client *store.Client, commandName string) error {
	role := resolveActorRole()
	result, err := enforcement.CheckInboxGate(ctx, client, role)
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

func runInboxGatePreflightForTaskAck(ctx context.Context, client *store.Client, taskID string) error {
	role := resolveActorRole()
	result, err := enforcement.CheckInboxGate(ctx, client, role)
	if err != nil {
		return nil
	}
	if result.Bypassed {
		logInboxBypass("task ack", result.BypassReason, role)
		return nil
	}
	if !result.Blocked {
		return nil
	}

	blocking, handledIDs := splitTaskAckInboxBlockers(result.Messages, taskID)
	if len(handledIDs) > 0 {
		if err := client.MarkMessagesRead(ctx, handledIDs); err != nil {
			return fmt.Errorf("mark matching task assignment handled: %w", err)
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	result.Messages = blocking
	result.Count = len(blocking)
	return fmt.Errorf("%s", enforcement.FormatBlockMessage(role, result))
}

func splitTaskAckInboxBlockers(messages []store.Message, taskID string) ([]store.Message, []string) {
	taskID = strings.TrimSpace(taskID)
	var blocking []store.Message
	var handledIDs []string
	for _, m := range messages {
		if messageMatchesTaskAck(m, taskID) {
			if id := strings.TrimSpace(m.ID); id != "" {
				handledIDs = append(handledIDs, id)
			}
			continue
		}
		blocking = append(blocking, m)
	}
	return blocking, handledIDs
}

func messageMatchesTaskAck(m store.Message, taskID string) bool {
	if taskID == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(m.TaskID), taskID) {
		return true
	}
	return strings.Contains(strings.ToUpper(m.Content), strings.ToUpper(taskID))
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
	_ = projectstate.AppendBypass(runtimeDir, "inbox-obey:"+gate, "", actor, "", "", "")
	fmt.Fprintf(os.Stderr, "⚠ inbox gate bypassed (GG_ALLOW_INBOX_SKIP=%s)\n", reason)
}
