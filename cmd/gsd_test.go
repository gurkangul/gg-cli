package cmd

import (
	"strings"
	"testing"
)

func TestGSDAuditHelp_IsAdvisoryScratchpadContract(t *testing.T) {
	text := gsdAuditCmd.Short + "\n" + gsdAuditCmd.Long
	for _, want := range []string{
		"scratchpad",
		"advisory",
		"durable gg task",
		"0  audit completed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("gsd audit help missing %q in:\n%s", want, text)
		}
	}
	removed := []string{
		"exactly one gg task " + "mirror",
		"drift " + "found",
		"all GSD tasks are " + "mirrored",
		"missing " + "mirrors",
	}
	for _, removed := range removed {
		if strings.Contains(text, removed) {
			t.Fatalf("gsd audit help still contains retired mirror text %q in:\n%s", removed, text)
		}
	}
}
