package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/projectstate"
)

const taskDoneHydrationWindow = 30 * time.Minute

func isAgentTaggedSession() bool {
	return strings.TrimSpace(os.Getenv("GG_AGENT")) != "" || strings.TrimSpace(os.Getenv("GG_ROLE")) != ""
}

func checkTaskDoneHydrationGate(runtimeDir, taskID string, now time.Time) *ExitError {
	if !isAgentTaggedSession() {
		return nil
	}
	ok, _, err := projectstate.HasRecentHydration(runtimeDir, "task", taskID, taskDoneHydrationWindow, now)
	if err != nil {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"compact hydration gate could not verify full context for %s: %v\n"+
					"Run 'gg task get %s' to hydrate the full task before changing state, then retry.",
				taskID, err, taskID),
		}
	}
	if ok {
		return nil
	}
	return &ExitError{
		Code: ExitVerifyFailed,
		Message: fmt.Sprintf(
			"compact hydration gate rejected task done for %s: no recent full task hydration found.\n"+
				"Compact/list/search output is an index view, not source-of-truth. Run 'gg task get %s' and read the full detail before retrying.\n"+
				"Hydration proof window: %s. Set GG_ENFORCEMENT=off with GG_BYPASS_RATIONALE for emergency bypass.",
			taskID, taskID, taskDoneHydrationWindow),
	}
}
