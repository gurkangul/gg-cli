package cmd

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// agentIdentityPattern matches git identities that belong to an automated agent
// rather than a human — so commits would be mis-attributed (e.g. "Hermes Agent",
// hermes-agent@localhost). Human identities (real names/emails) never match.
var agentIdentityPattern = regexp.MustCompile(`(?i)\b(agent|bot)\b`)

// isAgentGitIdentity reports whether a git name/email looks like an automated
// agent identity (TASK-480: commits must be attributed to the human owner).
func isAgentGitIdentity(name, email string) bool {
	if agentIdentityPattern.MatchString(name) || agentIdentityPattern.MatchString(email) {
		return true
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@localhost") {
		return true
	}
	return false
}

func gitConfigValue(args ...string) string {
	out, err := exec.Command("git", append([]string{"config"}, args...)...).Output() //nolint:gosec // fixed git config args
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCommitIdentityWarning returns a non-empty message when the effective git
// commit identity for the current repo looks like an agent. Empty otherwise.
func gitCommitIdentityWarning() string {
	name := gitConfigValue("user.name")
	email := gitConfigValue("user.email")
	if !isAgentGitIdentity(name, email) {
		return ""
	}
	return fmt.Sprintf("git commit identity is %q <%s> — commits will be attributed to an agent, not you. Fix: gg doctor --fix-git-identity", name, email)
}

// runDoctorFixGitIdentity removes a repo-local agent identity override so commits
// fall back to the human's global git identity.
func runDoctorFixGitIdentity(_ *cobra.Command) error {
	localName := gitConfigValue("--local", "user.name")
	localEmail := gitConfigValue("--local", "user.email")
	if localName == "" && localEmail == "" {
		fmt.Println("no repo-local git identity override — commits already use your global identity.")
		return nil
	}
	if !isAgentGitIdentity(localName, localEmail) {
		fmt.Printf("repo-local git identity is %q <%s> — looks human, leaving as-is.\n", localName, localEmail)
		return nil
	}
	_ = exec.Command("git", "config", "--local", "--unset", "user.name").Run()  //nolint:gosec
	_ = exec.Command("git", "config", "--local", "--unset", "user.email").Run() //nolint:gosec

	newName := gitConfigValue("user.name")
	newEmail := gitConfigValue("user.email")
	if newName == "" {
		return fmt.Errorf("removed the agent identity, but no global git user is set — run: git config --global user.name \"Your Name\" && git config --global user.email \"you@example.com\"")
	}
	if isAgentGitIdentity(newName, newEmail) {
		return fmt.Errorf("git identity still looks like an agent (%q <%s>) — check your global git config", newName, newEmail)
	}
	fmt.Printf("✓ git commit identity reset to %s <%s>\n", newName, newEmail)
	return nil
}
