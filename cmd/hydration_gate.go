package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/projectstate"
)

const taskHydrationWindow = 30 * time.Minute

func isAgentTaggedSession() bool {
	return strings.TrimSpace(os.Getenv("GG_AGENT")) != "" || strings.TrimSpace(os.Getenv("GG_ROLE")) != ""
}

func checkTaskDoneHydrationGate(runtimeDir, taskID string, now time.Time) *ExitError {
	return checkTaskHydrationGate(runtimeDir, taskID, "task done", now)
}

func checkBugHydrationGate(runtimeDir, bugID, action string, now time.Time) *ExitError {
	if !isAgentTaggedSession() {
		return nil
	}
	if strings.TrimSpace(action) == "" {
		action = "bug state change"
	}
	ok, _, err := projectstate.HasRecentHydration(runtimeDir, "bug", bugID, taskHydrationWindow, now)
	if err != nil {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"compact hydration gate could not verify full context for %s before %s: %v\n"+
					"Run 'gg bug get %s' or 'gg bug triage %s' to hydrate the full bug before changing state, then retry.",
				bugID, action, err, bugID, bugID),
		}
	}
	if ok {
		return nil
	}
	return &ExitError{
		Code: ExitVerifyFailed,
		Message: fmt.Sprintf(
			"compact hydration gate rejected %s for %s: no recent full bug hydration found.\n"+
				"Compact/list/search output is an index view, not source-of-truth. Run 'gg bug get %s' or 'gg bug triage %s' and read the full detail before retrying.\n"+
				"Hydration proof window: %s. Set GG_ENFORCEMENT=off with GG_BYPASS_RATIONALE for emergency bypass.",
			action, bugID, bugID, bugID, taskHydrationWindow),
	}
}

func checkTaskHydrationGate(runtimeDir, taskID, action string, now time.Time) *ExitError {
	if !isAgentTaggedSession() {
		return nil
	}
	if strings.TrimSpace(action) == "" {
		action = "task state change"
	}
	ok, _, err := projectstate.HasRecentHydration(runtimeDir, "task", taskID, taskHydrationWindow, now)
	if err != nil {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"compact hydration gate could not verify full context for %s before %s: %v\n"+
					"Run 'gg task get %s' to hydrate the full task before changing state, then retry.",
				taskID, action, err, taskID),
		}
	}
	if ok {
		return nil
	}
	return &ExitError{
		Code: ExitVerifyFailed,
		Message: fmt.Sprintf(
			"compact hydration gate rejected %s for %s: no recent full task hydration found.\n"+
				taskHydrationInstruction(action, taskID)+"\n"+
				"Hydration proof window: %s. Set GG_ENFORCEMENT=off with GG_BYPASS_RATIONALE for emergency bypass.",
			action, taskID, taskHydrationWindow),
	}
}

func taskHydrationInstruction(action, taskID string) string {
	if action == "task ready-for-live" {
		return fmt.Sprintf("Run 'gg task get %s' before ready-for-live; 'gg context --for-task %s' alone is not enough.", taskID, taskID)
	}
	return fmt.Sprintf("Compact/list/search output is an index view, not source-of-truth. Run 'gg task get %s' and read the full detail before retrying.", taskID)
}

func enforceTaskHydrationGate(w io.Writer, cache *hookConfig, taskID, action, bypassGate string) error {
	if cache == nil {
		cache = &hookConfig{}
	}
	_, cfg, cfgErr := cache.load(w)
	if cfgErr != nil || cfg == nil {
		if cfgErr == nil {
			cfgErr = fmt.Errorf("config not loaded")
		}
		return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("compact hydration gate could not load project config for %s: %v", taskID, cfgErr)}
	}
	runtimeDir, rtErr := cfg.RuntimeDir()
	if rtErr != nil {
		return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("compact hydration gate could not find runtime dir for %s: %v", taskID, rtErr)}
	}
	if !enforcement.Enabled() {
		if isAgentTaggedSession() {
			if strings.TrimSpace(bypassGate) == "" {
				bypassGate = "compact-hydration-task-state"
			}
			if rej := emitGuardSkipEvent(bypassGate, taskID); rej != nil {
				return rej
			}
		}
		return nil
	}
	if rej := checkTaskHydrationGate(runtimeDir, taskID, action, time.Now().UTC()); rej != nil {
		return rej
	}
	return nil
}

func enforceBugHydrationGate(w io.Writer, cache *hookConfig, bugID, action, bypassGate string) error {
	if cache == nil {
		cache = &hookConfig{}
	}
	_, cfg, cfgErr := cache.load(w)
	if cfgErr != nil || cfg == nil {
		if cfgErr == nil {
			cfgErr = fmt.Errorf("config not loaded")
		}
		return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("compact hydration gate could not load project config for %s: %v", bugID, cfgErr)}
	}
	runtimeDir, rtErr := cfg.RuntimeDir()
	if rtErr != nil {
		return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("compact hydration gate could not find runtime dir for %s: %v", bugID, rtErr)}
	}
	if !enforcement.Enabled() {
		if isAgentTaggedSession() {
			if strings.TrimSpace(bypassGate) == "" {
				bypassGate = "compact-hydration-bug-state"
			}
			if rej := emitGuardSkipEvent(bypassGate, ""); rej != nil {
				return rej
			}
		}
		return nil
	}
	if rej := checkBugHydrationGate(runtimeDir, bugID, action, time.Now().UTC()); rej != nil {
		return rej
	}
	return nil
}
