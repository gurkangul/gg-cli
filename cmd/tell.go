package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var tellCmd = &cobra.Command{
	Use:   `tell "role[,role2,...]" "message"`,
	Short: "Send a message to one or more agent roles",
	Long: `Send a message to one or more agent roles.

Targets can be comma-separated for fanout:
  gg tell qa,reviewer "TASK-042 ready for review"

@role mentions in the message body are auto-routed in addition to the primary target:
  gg tell all "@qa please review before merging"

Use --audience to control inbox visibility:
  gg tell all "TASK-016 picked up" --from developer --audience agents
  gg tell human "deploy is blocked, need approval" --from developer --audience human`,
	Args: cobra.ExactArgs(2),
	RunE: runTell,
}

var (
	tellFrom     string
	tellTask     string
	tellAudience string
)

var mentionRe = regexp.MustCompile(`@([A-Za-z][A-Za-z0-9_-]*)`)

func init() {
	tellCmd.Flags().StringVar(&tellFrom, "from", "", "sender role (defaults to $GG_ROLE, then 'user')")
	tellCmd.Flags().StringVar(&tellTask, "task", "", "related task ID")
	tellCmd.Flags().StringVar(&tellAudience, "audience", "all", "visibility: all | human | agents (agents = filtered from human inbox by default)")
	rootCmd.AddCommand(tellCmd)
}

// parseMentions extracts @role tokens from message content.
func parseMentions(content string) []string {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var roles []string
	for _, m := range matches {
		r := strings.ToLower(m[1])
		if !seen[r] {
			seen[r] = true
			roles = append(roles, r)
		}
	}
	return roles
}

// collectTargets merges comma-separated primary targets with @mention targets, deduped.
func collectTargets(rawTarget, content string) []string {
	seen := make(map[string]bool)
	var targets []string
	for _, t := range strings.Split(rawTarget, ",") {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" && !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	for _, r := range parseMentions(content) {
		if !seen[r] {
			seen[r] = true
			targets = append(targets, r)
		}
	}
	return targets
}

func runTell(cmd *cobra.Command, args []string) error {
	printProjectBanner()
	rawTarget, err := requireNonEmpty("role", args[0])
	if err != nil {
		return err
	}
	content, err := requireNonEmpty("message", args[1])
	if err != nil {
		return err
	}
	taskRef, err := normalizeTaskRef(tellTask)
	if err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	from := strings.TrimSpace(tellFrom)
	if from == "" {
		from = strings.TrimSpace(os.Getenv("GG_ROLE"))
	}
	if from == "" {
		from = "user"
	}

	audience := strings.TrimSpace(tellAudience)
	switch audience {
	case "all", "human", "agents":
	case "":
		audience = "all"
	default:
		return fmt.Errorf("--audience must be one of: all, human, agents (got %q)", audience)
	}

	targets := collectTargets(rawTarget, content)
	if len(targets) == 0 {
		return fmt.Errorf("no valid targets specified")
	}

	d, err := loadDepsOfflineSafe(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	for _, target := range targets {
		m := store.Message{
			FromRole: from,
			ToRole:   target,
			Content:  content,
			Audience: audience,
			TaskID:   taskRef,
		}
		if err := d.store.SendMessage(ctx, m); err != nil {
			var oq *store.OutboxQueued
			if errors.As(err, &oq) {
				queueBrainOutbox(oq, config.GGDirOrEmpty())
				warnBrainOutboxQueued(cmd.ErrOrStderr(), oq.Cause)
				continue
			}
			return fmt.Errorf("send message to %s: %w", target, err)
		}
	}

	if len(targets) == 1 {
		fmt.Printf("✓ Message sent from %s to %s: %s\n", from, targets[0], content)
	} else {
		fmt.Printf("✓ Message sent from %s to [%s]: %s\n", from, strings.Join(targets, ", "), content)
	}
	return nil
}
