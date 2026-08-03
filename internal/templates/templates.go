package templates

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
)

//go:embed agent-contract.md
var AgentContract string

//go:embed docker-compose.yaml
var DockerCompose string

//go:embed rules.md
var RulesMD string

//go:embed agents.md
var AgentsMD string

//go:embed config.yaml
var ConfigYAML string

//go:embed task-done-go.sh
var TaskDoneGoHook string

//go:embed pre-task-done-go.sh
var PreTaskDoneGoHook string

//go:embed pre-task-done-node.sh
var PreTaskDoneNodeHook string

//go:embed pre-task-done-decide-capture.sh
var PreTaskDoneDecideCaptureHook string

//go:embed pre-task-done-review-required.sh
var PreTaskDoneReviewRequiredHook string

//go:embed pre-task-done-ac-attestation.sh
var PreTaskDoneACAttestationHook string

//go:embed 90-bug-repros.sh
var BugReprosHook string

//go:embed 05-smoke-e2e.sh
var SmokeE2EHook string

//go:embed 30-file-size.sh
var FileSizeGateHook string

//go:embed 35-stub-scan.sh
var StubScanGateHook string

//go:embed 45-dependency-justification.sh
var DependencyJustificationGateHook string

//go:embed pre-task-done-lint-gate.sh
var PreTaskDoneLintGateHook string

//go:embed pre-task-done-impact-attestation.sh
var PreTaskDoneImpactAttestationHook string

//go:embed pre-task-done-review-convergence.sh
var PreTaskDoneReviewConvergenceHook string

//go:embed makefile-test-tiers.mk
var MakefileTestTiers string

//go:embed pre-commit-secret-scan.sh
var PreCommitSecretScanHook string

//go:embed commit-msg-convention.sh
var CommitMsgGateHook string

// ArtifactSHAs returns a map of artifact key → SHA256 of the bundled template.
// Keys are paths relative to .gg/ (e.g. "RULES.md",
// "hooks/pre-task-done.d/05-smoke-e2e.sh"). This is the canonical set of
// artifacts that gg manages; gg doctor --sync-artifacts compares it against
// .gg/installed.json to surface drift.
func ArtifactSHAs() map[string]string {
	entries := map[string]string{
		"RULES.md":                                             RulesMD,
		"AGENTS.md":                                            AgentsMD,
		"hooks/pre-task-done.d/05-smoke-e2e.sh":                SmokeE2EHook,
		"hooks/task-done.d/90-bug-repros.sh":                   BugReprosHook,
		"hooks/pre-task-done.d/10-go-verify.sh":                PreTaskDoneGoHook,
		"hooks/pre-task-done.d/10-node-verify.sh":              PreTaskDoneNodeHook,
		"hooks/pre-task-done.d/20-decide-capture.sh":           PreTaskDoneDecideCaptureHook,
		"hooks/pre-task-done.d/30-file-size.sh":                FileSizeGateHook,
		"hooks/pre-task-done.d/35-stub-scan.sh":                StubScanGateHook,
		"hooks/pre-task-done.d/45-dependency-justification.sh": DependencyJustificationGateHook,
		"hooks/pre-task-done.d/40-review-required.sh":          PreTaskDoneReviewRequiredHook,
		"hooks/pre-task-done.d/50-ac-attestation.sh":           PreTaskDoneACAttestationHook,
		"hooks/pre-task-done.d/60-lint-gate.sh":                PreTaskDoneLintGateHook,
		"hooks/pre-task-done.d/60-impact-attestation.sh":       PreTaskDoneImpactAttestationHook,
		"hooks/pre-task-done.d/70-review-convergence.sh":       PreTaskDoneReviewConvergenceHook,
		"hooks/task-done.d/80-task-done-go.sh":                 TaskDoneGoHook,
		"templates/makefile-test-tiers.mk":                     MakefileTestTiers,
		"hooks/pre-commit.d/20-secret-scan.sh":                 PreCommitSecretScanHook,
		"hooks/commit-msg.d/30-commit-msg.sh":                  CommitMsgGateHook,
	}
	out := make(map[string]string, len(entries))
	for k, v := range entries {
		h := sha256.Sum256([]byte(v))
		out[k] = fmt.Sprintf("%x", h)
	}
	return out
}
