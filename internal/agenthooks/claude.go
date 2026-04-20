package agenthooks

// claudeInstaller writes Claude Code's SessionStart + UserPromptSubmit +
// PreToolUse + PostToolUse + Stop hooks into <project>/.claude/settings.json,
// and the shared contract block into <project>/CLAUDE.md. See:
//   - claude_hooks.go   — hook-schema constants + JSON mutation helpers
//   - claude_signals.go — detection helpers (project/global)

type claudeInstaller struct {
	// testHome overrides os.UserHomeDir() in unit tests so global-signal
	// detection doesn't depend on the test machine's actual Claude install.
	// Empty string (the zero value) means "use the real home directory".
	testHome string
	// testEnv overrides os.Getenv in unit tests so env-var signals can be
	// controlled without setting real environment variables. nil = os.Getenv.
	testEnv func(string) string
}

func (c *claudeInstaller) Name() string                           { return "claude" }
func (c *claudeInstaller) Tier() Tier                             { return TierHard }
func (c *claudeInstaller) ContractPath(projectRoot string) string { return pathIn(projectRoot, "CLAUDE.md") }

func (c *claudeInstaller) Detect(projectRoot string) bool {
	// Project-level signal: .claude/ directory exists in the project root.
	if c.hasProjectClaudeDir(projectRoot) {
		return true
	}
	// Global signals: Claude Code is installed on this machine even if this
	// particular project doesn't have a .claude/ directory yet. We still
	// return true so Install can produce an inline suggestion.
	return c.globalSignals()
}

func (c *claudeInstaller) Install(projectRoot string, opts Options) (Result, error) {
	path := pathIn(projectRoot, ".claude", "settings.json")
	res := Result{Path: path}

	// Global install detected but no project-level .claude/ directory — print
	// an inline suggestion rather than auto-creating files the user didn't ask for.
	// Skipped when Force=true: the user explicitly asked us to install.
	if !opts.Force && !c.hasProjectClaudeDir(projectRoot) && c.globalSignals() {
		res.Path = ""
		res.Action = ActionSuggested
		res.Notes = append(res.Notes,
			"global install detected — project hook not yet installed",
			"Run: gg doctor --install-agent-hooks --force --agent claude",
		)
		return res, nil
	}

	data, existed, err := loadJSONFile(path)
	if err != nil {
		return res, err
	}

	sessionStartPresent := claudeHasHook(data, claudeCommandMarker)
	inboxPresent := claudeHasUserPromptHook(data, claudeInboxCommandMarker)
	inboxStale := !inboxPresent && claudeHasUserPromptHook(data, claudeInboxStaleMarker)
	gsdGuardPresent := claudeHasPreToolUseHook(data, claudeGSDGuardCommandMarker)
	verifyPresent := claudeHasPostToolUseHook(data, claudeVerifyCommandMarker)
	auditTrackPresent := claudeHasPostToolUseHook(data, claudeAuditTrackCommandMarker)
	auditReportPresent := claudeHasStopHook(data, claudeAuditReportCommandMarker)

	if sessionStartPresent && inboxPresent && gsdGuardPresent && verifyPresent && auditTrackPresent && auditReportPresent {
		res.Action = ActionUpToDate
		res.Notes = append(res.Notes, "SessionStart hook already present")
		res.Notes = append(res.Notes, "UserPromptSubmit hook already present")
		res.Notes = append(res.Notes, "PreToolUse gsd-guard hook already present")
		res.Notes = append(res.Notes, "PostToolUse verify hook already present")
		res.Notes = append(res.Notes, "PostToolUse audit-track hook already present")
		res.Notes = append(res.Notes, "Stop audit-report hook already present")
		// Still check/update CLAUDE.md contract block even when hooks are up-to-date.
		claudeMDPath := pathIn(projectRoot, "CLAUDE.md")
		cAction, cNotes, cErr := writeContractBlock(claudeMDPath, opts.DryRun)
		if cErr != nil {
			return res, cErr
		}
		res.Notes = append(res.Notes, cNotes...)
		if cAction != ActionUpToDate {
			res.Action = cAction
		}
		return res, nil
	}

	if !sessionStartPresent {
		claudeAddHook(data, claudeCommand)
	}
	if inboxStale {
		claudeReplaceUserPromptHook(data, claudeInboxStaleMarker, claudeInboxCommand)
	} else if !inboxPresent {
		claudeAddUserPromptHook(data, claudeInboxCommand)
	}
	if !gsdGuardPresent {
		claudeAddPreToolUseHook(data, claudeMatcherGSDPlan, claudeGSDGuardCommand)
	}
	if !verifyPresent {
		claudeAddPostToolUseHook(data, claudeMatcherWriteTools, claudeVerifyCommand)
	}
	if !auditTrackPresent {
		claudeAddPostToolUseHook(data, claudeMatcherWriteTools, claudeAuditTrackCommand)
	}
	if !auditReportPresent {
		claudeAddStopHook(data, claudeAuditReportCommand)
	}

	// Write shared contract block to CLAUDE.md (respects DryRun) before any
	// early return so dry-run previews include the contract note alongside
	// the hook-change notes.
	claudeMDPath := pathIn(projectRoot, "CLAUDE.md")
	_, cNotes, cErr := writeContractBlock(claudeMDPath, opts.DryRun)
	if cErr != nil {
		return res, cErr
	}

	if opts.DryRun {
		res.Action = ActionDryRun
		if !sessionStartPresent {
			res.Notes = append(res.Notes, "would add SessionStart hook: "+claudeCommand)
		}
		if inboxStale {
			res.Notes = append(res.Notes, "would rewrite stale UserPromptSubmit hook: "+claudeInboxCommand)
		} else if !inboxPresent {
			res.Notes = append(res.Notes, "would add UserPromptSubmit hook: "+claudeInboxCommand)
		}
		if !gsdGuardPresent {
			res.Notes = append(res.Notes, "would add PreToolUse gsd-guard hook: "+claudeGSDGuardCommand)
		}
		if !verifyPresent {
			res.Notes = append(res.Notes, "would add PostToolUse verify hook: "+claudeVerifyCommand)
		}
		if !auditTrackPresent {
			res.Notes = append(res.Notes, "would add PostToolUse audit-track hook: "+claudeAuditTrackCommand)
		}
		if !auditReportPresent {
			res.Notes = append(res.Notes, "would add Stop audit-report hook: "+claudeAuditReportCommand)
		}
		res.Notes = append(res.Notes, cNotes...)
		return res, nil
	}

	if err := writeJSONFile(path, data); err != nil {
		return res, err
	}
	if existed {
		res.Action = ActionUpdated
	} else {
		res.Action = ActionCreated
	}
	if !sessionStartPresent {
		res.Notes = append(res.Notes, "SessionStart hook: "+claudeCommand)
	}
	if inboxStale {
		res.Notes = append(res.Notes, "UserPromptSubmit hook rewritten (stale → current): "+claudeInboxCommand)
	} else if !inboxPresent {
		res.Notes = append(res.Notes, "UserPromptSubmit hook: "+claudeInboxCommand)
	}
	if !gsdGuardPresent {
		res.Notes = append(res.Notes, "PreToolUse gsd-guard hook: "+claudeGSDGuardCommand)
	}
	if !verifyPresent {
		res.Notes = append(res.Notes, "PostToolUse verify hook: "+claudeVerifyCommand)
	}
	if !auditTrackPresent {
		res.Notes = append(res.Notes, "PostToolUse audit-track hook: "+claudeAuditTrackCommand)
	}
	if !auditReportPresent {
		res.Notes = append(res.Notes, "Stop audit-report hook: "+claudeAuditReportCommand)
	}
	res.Notes = append(res.Notes, cNotes...)
	// res.Action is always ActionUpdated or ActionCreated in this path (the
	// all-up-to-date case short-circuited above), so the contract block's
	// sub-action does not need to escalate it.
	return res, nil
}
