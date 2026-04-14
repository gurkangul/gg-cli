package cmd

import (
	"fmt"
	"strings"

	"github.com/gurkangul/gg/internal/store"
	"github.com/spf13/cobra"
)

var discussCmd = &cobra.Command{
	Use:   "discuss",
	Short: "Manage open discussions — topics raised but not yet concluded",
	Long: "Discussions track conversations that haven't reached a decision/task/rejection.\n" +
		"Open them mid-conversation when a topic surfaces without a clear outcome; resolve\n" +
		"them by linking to the artifact that concluded them, or dismiss if superseded.",
}

// --- discuss open ---

var discussOpenCmd = &cobra.Command{
	Use:   `open "topic"`,
	Short: "Open a new discussion (unresolved topic)",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiscussOpen,
}

var (
	discussDetail string
	discussTags   string
)

// --- discuss list ---

var discussListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discussions (default: open only)",
	RunE:  runDiscussList,
}

var discussListStatus string

// --- discuss get ---

var discussGetCmd = &cobra.Command{
	Use:   "get DISC-ID",
	Short: "Show a single discussion",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiscussGet,
}

// --- discuss resolve ---

var discussResolveCmd = &cobra.Command{
	Use:   `resolve DISC-ID`,
	Short: "Close a discussion by linking it to a decision/task/rejection",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiscussResolve,
}

var (
	discussVia     string
	discussSummary string
)

// --- discuss dismiss ---

var discussDismissCmd = &cobra.Command{
	Use:   `dismiss DISC-ID`,
	Short: "Close a discussion without a resolution (topic superseded or irrelevant)",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiscussDismiss,
}

var discussDismissReason string

var validDiscussStatuses = map[string]bool{"open": true, "resolved": true, "dismissed": true}
var validDiscussVia = map[string]bool{"decision": true, "task": true, "rejection": true}

func init() {
	discussOpenCmd.Flags().StringVar(&discussDetail, "detail", "", "longer context")
	discussOpenCmd.Flags().StringVar(&discussTags, "tags", "", "comma-separated tags")

	discussListCmd.Flags().StringVar(&discussListStatus, "status", "open", "filter by status: open, resolved, dismissed, or all")

	discussResolveCmd.Flags().StringVar(&discussVia, "via", "", "what concluded this: decision | task | rejection")
	discussResolveCmd.Flags().StringVar(&discussSummary, "summary", "", "short conclusion summary (e.g. 'decided X, see gg search')")
	_ = discussResolveCmd.MarkFlagRequired("via")
	_ = discussResolveCmd.MarkFlagRequired("summary")

	discussDismissCmd.Flags().StringVar(&discussDismissReason, "reason", "", "why this discussion is no longer relevant")
	_ = discussDismissCmd.MarkFlagRequired("reason")

	discussCmd.AddCommand(discussOpenCmd, discussListCmd, discussGetCmd, discussResolveCmd, discussDismissCmd)
	rootCmd.AddCommand(discussCmd)
}

func normalizeDiscID(raw string) (string, error) {
	id := strings.ToUpper(strings.TrimSpace(raw))
	if id == "" {
		return "", fmt.Errorf("discussion ID is required (expected DISC-NNN)")
	}
	if _, err := store.ParseDiscID(id); err != nil {
		return "", err
	}
	return id, nil
}

func runDiscussOpen(cmd *cobra.Command, args []string) error {
	topic, err := requireNonEmpty("topic", args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	embedText := topic
	if strings.TrimSpace(discussDetail) != "" {
		embedText = topic + " " + discussDetail
	}
	vector, err := d.embedder.Generate(ctx, embedText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	disc := store.Discussion{
		Topic:  topic,
		Detail: strings.TrimSpace(discussDetail),
		Tags:   parseTags(discussTags),
	}

	id, err := d.store.OpenDiscussion(ctx, disc, vector)
	if err != nil {
		return fmt.Errorf("open discussion: %w", err)
	}

	fmt.Printf("• Discussion opened: %s — %s\n", id, topic)
	fmt.Println("  Resolve with: gg discuss resolve", id, "--via decision|task|rejection --summary '...'")
	fmt.Println("  Dismiss with: gg discuss dismiss", id, "--reason '...'")
	return nil
}

func runDiscussList(cmd *cobra.Command, args []string) error {
	status := strings.ToLower(strings.TrimSpace(discussListStatus))
	if status == "all" {
		status = ""
	} else if status != "" && !validDiscussStatuses[status] {
		return fmt.Errorf("invalid --status %q — use open, resolved, dismissed, or all", status)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	discussions, err := d.store.ListDiscussions(ctx, status)
	if err != nil {
		return fmt.Errorf("list discussions: %w", err)
	}
	if len(discussions) == 0 {
		fmt.Println("No discussions.")
		return nil
	}
	for _, disc := range discussions {
		fmt.Printf("%s %s — %s\n", discussStatusIcon(disc.Status), disc.ID, disc.Topic)
		if disc.Status == "resolved" && disc.ResolvedNote != "" {
			fmt.Printf("    ↳ resolved via %s: %s\n", disc.ResolvedVia, disc.ResolvedNote)
		}
		if disc.Status == "dismissed" && disc.DismissNote != "" {
			fmt.Printf("    ↳ dismissed: %s\n", disc.DismissNote)
		}
	}
	return nil
}

func runDiscussGet(cmd *cobra.Command, args []string) error {
	discID, err := normalizeDiscID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	disc, err := d.store.GetDiscussion(ctx, discID)
	if err != nil {
		return err
	}

	fmt.Printf("%s %s — %s\n", discussStatusIcon(disc.Status), disc.ID, disc.Topic)
	if disc.Detail != "" {
		fmt.Printf("  Detail: %s\n", disc.Detail)
	}
	if len(disc.Tags) > 0 {
		fmt.Printf("  Tags: %s\n", strings.Join(disc.Tags, ", "))
	}
	fmt.Printf("  Status: %s\n", disc.Status)
	if disc.Status == "resolved" {
		fmt.Printf("  Resolved via: %s\n", disc.ResolvedVia)
		fmt.Printf("  Summary: %s\n", disc.ResolvedNote)
	}
	if disc.Status == "dismissed" {
		fmt.Printf("  Dismiss reason: %s\n", disc.DismissNote)
	}
	fmt.Printf("  Opened: %s\n", disc.CreatedAt)
	if disc.UpdatedAt != disc.CreatedAt {
		fmt.Printf("  Closed: %s\n", disc.UpdatedAt)
	}
	return nil
}

func runDiscussResolve(cmd *cobra.Command, args []string) error {
	discID, err := normalizeDiscID(args[0])
	if err != nil {
		return err
	}
	via := strings.ToLower(strings.TrimSpace(discussVia))
	if !validDiscussVia[via] {
		return fmt.Errorf("invalid --via %q — use decision, task, or rejection", discussVia)
	}
	summary, err := requireNonEmpty("--summary", discussSummary)
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.ResolveDiscussion(ctx, discID, via, summary); err != nil {
		return err
	}

	fmt.Printf("✓ %s resolved (via %s): %s\n", discID, via, summary)
	return nil
}

func runDiscussDismiss(cmd *cobra.Command, args []string) error {
	discID, err := normalizeDiscID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("--reason", discussDismissReason)
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.DismissDiscussion(ctx, discID, reason); err != nil {
		return err
	}

	fmt.Printf("⊘ %s dismissed: %s\n", discID, reason)
	return nil
}

func discussStatusIcon(status string) string {
	switch status {
	case "resolved":
		return "✓"
	case "dismissed":
		return "⊘"
	default:
		return "•"
	}
}
