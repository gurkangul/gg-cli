package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var taskAckCmd = &cobra.Command{
	Use:   `ack TASK-ID "AC-1: ...; AC-2: ..."`,
	Short: "Record a worker acceptance-criteria paraphrase before coding",
	Long: `Record the worker's acceptance-criteria paraphrase before implementation.

The ACK is stored as a task-linked decision, the task moves to in_progress, and
the paraphrase is sent to the active master target(s) so master can reply ACK-OK or ACK-FIX.`,
	Args: cobra.ExactArgs(2),
	RunE: runTaskAck,
}

func init() {
	addFromFlag(taskAckCmd)
	taskCmd.AddCommand(taskAckCmd)
}

func runTaskAck(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	ackText, err := requireNonEmpty("ack", args[1])
	if err != nil {
		return err
	}
	if err := requireAgentIdentity(); err != nil {
		return err
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := runInboxGatePreflight(ctx, d.store, "task ack"); err != nil {
		return err
	}

	// Reject ACK on terminal-state tasks — a done/ready_for_live/blocked task
	// cannot begin implementation, so accepting an ACK would be misleading.
	existing, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if err := validateTaskAckAllowed(taskID, existing.Status); err != nil {
		return err
	}

	text := formatTaskAckDecision(taskID, ackText)
	vector, err := d.embedder.Generate(ctx, text)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}
	author := resolveAuthor(cmd)
	dec := store.Decision{
		ID:     uuid.New().String(),
		Text:   text,
		Reason: "worker AC paraphrase before coding; master should reply ACK-OK or ACK-FIX",
		Status: "active",
		Tags:   []string{"worker-ack", "ac-paraphrase", taskID},
		TaskID: taskID,
		Author: author,
	}
	// 3-write sequence: decision → status → message. Writes are NOT atomic.
	// Partial failure semantics: if status or message write fails, the decision
	// is already persisted; a retry will attempt all three again. AddDecision is
	// idempotent on the same UUID, so the decision write is safe to repeat.
	if err := d.store.AddDecision(ctx, dec, vector); err != nil {
		return fmt.Errorf("store ack decision: %w", err)
	}
	// Transition to in_progress; ignore ErrAlreadyInState (task may already be
	// in_progress from a previous ACK-FIX rework cycle).
	if err := d.store.UpdateTaskStatus(ctx, taskID, "in_progress", "worker ACK recorded"); err != nil {
		if !errors.Is(err, store.ErrAlreadyInState) {
			return fmt.Errorf("set task in_progress: %w", err)
		}
	}
	targets := masterMessageTargets()
	for _, target := range targets {
		msg := store.Message{
			FromRole: author,
			ToRole:   target,
			Content:  text,
			Audience: "agents",
			TaskID:   taskID,
		}
		if err := d.store.SendMessage(ctx, msg); err != nil {
			return fmt.Errorf("send ack to %s: %w", target, err)
		}
	}

	return printJSON(dec, func() {
		fmt.Printf("✓ %s ACK recorded and sent to %s\n", taskID, strings.Join(targets, ","))
		fmt.Println("  Wait for ACK-OK or ACK-FIX before coding; after 5 minutes, proceed only with ACK-IMPLICIT in the commit body.")
	})
}

// validateTaskAckAllowed returns an error when status is a terminal state that
// must not accept an ACK — prevents misleading decisions from being persisted.
func validateTaskAckAllowed(taskID, status string) error {
	switch status {
	case "done", "ready_for_live", "blocked":
		return fmt.Errorf("cannot ACK task %s: task is %s — ACK is only valid for pending or in_progress tasks", taskID, status)
	}
	return nil
}

func formatTaskAckDecision(taskID, ackText string) string {
	return fmt.Sprintf("%s ACK: %s", taskID, strings.TrimSpace(ackText))
}
