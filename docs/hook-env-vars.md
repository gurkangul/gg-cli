# Hook environment variables

This reference covers every `GG_*` environment variable that hooks and
agents interact with. It exists to prevent a class of regressions where a
variable was silently dropped (or assumed to be present) across process
boundaries — see the TASK-308 timeline for a concrete example.

## Variables injected by gg into every hook

These are set automatically by `gg task done` (and other lifecycle commands)
before running any script in `.gg/hooks/pre-task-done.d/` or
`.gg/hooks/task-done.d/`. Hook scripts can read them without setting them.

| Variable | Value | Example |
|---|---|---|
| `GG_TASK_ID` | The task ID being transitioned | `TASK-042` |
| `GG_TASK_SUMMARY` | The summary string passed to `gg task done` | `"implement foo"` |
| `GG_PROJECT_ID` | The project UUID from `.gg/config.yaml` | `a1b2c3d4-...` |
| `GG_ACTOR` | `$GG_ROLE` when set; falls back to `$GG_AGENT` | `claude-code` |
| `GG_INSIDE_HOOK` | Always `"1"` inside hooks invoked by a `gg` parent process | `1` |

**Why `GG_INSIDE_HOOK` matters.** When `gg task done` runs the verify
hook, the parent `gg` process already holds an open embedded-store handle and
holds the project lock. A hook that also opens connections (e.g. by running
`go test ./...` with live-state tests) races the parent. Set
`GG_INSIDE_HOOK=1` detection in any hook that calls `go test` to
force `-short` mode:

```sh
TEST_FLAGS="-count=1 -timeout 120s"
if [ "${GG_INSIDE_HOOK:-0}" = "1" ]; then
  TEST_FLAGS="$TEST_FLAGS -short"
fi
go test ./... $TEST_FLAGS
```

The installed `10-go-verify.sh` template already does this. **Do not remove
this check** — its absence was the root cause behind two TASK-308 reopens.

---

## Per-gate control variables

Each gate has an opt-out or mode variable. All default to a safe value so
the gate runs unless you explicitly change behaviour.

### `GG_AC_ATTESTATION` — AC attestation gate (`50-ac-attestation.sh`)

| Value | Effect |
|---|---|
| `on` *(default)* | Gate runs; exits 7 when ACs are unmatched |
| `warn` | Gate runs; prints warning but does not block |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_ALLOW_INCOMPLETE_AC="<reason>"` to skip this
one call. The bypass is written to the brain via `gg record` so it appears
in future `gg search` results and cannot be silently forgotten.

```sh
GG_ALLOW_INCOMPLETE_AC="TASK-042: analytical task, no code ACs" \
  gg task done TASK-042 "..."
```

### `GG_DECIDE_GATE` — decision capture gate (`20-decide-capture.sh`)

| Value | Effect |
|---|---|
| `warn` *(default)* | Prints warning when no linked decision exists; does not block |
| `on` | Exits 7 when no linked decision exists |
| `off` | Gate is skipped entirely |

### `GG_REVIEW_GATE` — review-required gate (`40-review-required.sh`)

| Value | Effect |
|---|---|
| `warn` *(default)* | Prints warning when task is not in `approved` state; does not block |
| `on` | Exits 7 when task has not been reviewed |
| `off` | Gate is skipped entirely |

### `GG_FILE_SIZE_GATE` — file-size gate (`30-file-size.sh`)

| Value | Effect |
|---|---|
| `warn` *(default)* | Prints warning for oversized files; does not block |
| `block` | Exits non-zero when any file exceeds the line limit |
| `off` | Gate is skipped entirely |

**Bypass in block mode (audited):** set `GG_ALLOW_FILE_SIZE="<reason>"`.
The bypass is recorded via `gg record`.

```sh
GG_FILE_SIZE_GATE=block GG_ALLOW_FILE_SIZE="TASK-042: generated file, exempt" \
  gg task done TASK-042 "..."
```

**Warning band (BUG-107).** In every mode except `off`, the gate also prints a
non-blocking `approaching limit` list for changed files at or above 90% of their
limit (450 source / 720 test):

```
[file-size] approaching limit (not a violation):
  band.go  (495 lines, limit 500 — 5 left)
[file-size] split these on the next touch rather than at the wall.
```

The band never affects the exit code, in `warn` or `block` mode, and there is no
knob to disable it separately — it only ever covers files the current task
changed. It exists because a limit with no approach warning reports "compliant"
right up to the wall: a file could sit at 499/500 producing no signal at all
until the next two-line edit turned it into a hard violation.

Project-wide, `gg audit file-size` prints the same band, and
`gg audit file-size --over 450` reports every file above an arbitrary threshold.

### `GG_STUB_GATE` — stub-scan gate (`35-stub-scan.sh`)

| Value | Effect |
|---|---|
| `warn` *(default)* | Lists stub markers this task added; does not block |
| `block` | Exits 7 when the task's diff adds a stub marker |
| `off` | Gate is skipped entirely |

Enforces the engineering-baseline line "ship no TODO stubs, dead flags, or
half-wired paths described as done". Matches `TODO`, `FIXME`, `XXX`, `HACK`,
`not implemented` and `unimplemented`.

Scope is deliberately narrow so the gate stays honest:

- **Added lines only.** A marker already living in a file you happen to touch is
  somebody else's debt; blocking on it would punish whoever walks past next.
- **Source extensions only** (`.go .ts .tsx .js .jsx .py .rs .java`), so prose in
  a `.md` file or a hook that merely names the markers is never a finding.
- **Whole tokens only** — `TODOS_ENDPOINT` is an identifier, not a stub.

**Bypass in block mode (audited):** set `GG_ALLOW_STUB="<reason>"`.

```sh
GG_STUB_GATE=block GG_ALLOW_STUB="TASK-042: remainder tracked in TASK-043" \
  gg task done TASK-042 "..."
```

### `GG_DEP_GATE` — dependency-justification gate (`45-dependency-justification.sh`)

| Value | Effect |
|---|---|
| `warn` *(default)* | Lists new dependencies with no linked decision; does not block |
| `block` | Exits 7 when a new dependency has no linked decision naming it |
| `off` | Gate is skipped entirely |

Enforces the engineering-baseline line that a new dependency "must buy something
concrete; put that reason in the `gg record`". The lockfile preserves the fact
that a dependency was added and never the reason, which is the part a future
agent cannot reconstruct.

Manifests watched: `go.mod`, `package.json`, `composer.json`,
`requirements.txt`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `pom.xml`,
`build.gradle(.kts)`.

A dependency counts as *justified* when `gg task decisions $GG_TASK_ID` mentions
its name (the trailing path segment also matches, so a decision saying
`gorilla/mux` covers `github.com/gorilla/mux`). **Version bumps never trigger
the gate** — the name appears on both sides of the diff, so only genuinely new
names are reported.

**Bypass in block mode (audited):** set `GG_ALLOW_DEP="<reason>"`.

```sh
GG_DEP_GATE=block GG_ALLOW_DEP="TASK-042: transitive, pulled by go mod tidy" \
  gg task done TASK-042 "..."
```

### `GG_LINT_GATE` — lint regression gate (`60-lint-gate.sh`)

| Value | Effect |
|---|---|
| `on` *(default)* | Exits 7 when lint error count increased |
| `warn` | Prints warning; does not block |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_ALLOW_LINT_REGRESSIONS="<reason>"`. Recorded
via `gg record`.

**Arming is a separate step from installing.** With no `.gg/lint-baseline.json`
the gate exits 0 on every run — installed, but unable to block anything. `gg
init` deploys the hook without capturing a baseline, so a fresh project starts
unarmed by design (it must not be blocked by debt it has never measured). Arm it
once the project is clean enough to hold a line:

```sh
gg doctor --capture-lint-baseline
```

`gg doctor` reports the state as a `lint gate` check — `armed (baseline N
issue(s))`, `installed but NOT armed`, or `golangci-lint is not on PATH`. Once
armed, the gate is a ratchet: it blocks only when the count *increases*, and it
shrinks the baseline automatically whenever the count drops, so existing debt is
grandfathered and paid down without further bookkeeping.

The file-size gate is not symmetric here: a missing
`.gg/file-size-baseline.json` makes `30-file-size.sh` *stricter* (it
grandfathers nothing), so its absence is not a finding.

### `GG_SECRET_SCAN` — secret scan gate (`20-secret-scan.sh`)

Controls whether the pre-commit secret scan hook blocks commits on findings.

| Value | Effect |
|---|---|
| `on` *(default)* | Exits 7 when gitleaks (or the narrow-regex fallback) finds secrets |
| `warn` | Prints warning; does not block the commit |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_BYPASS_RATIONALE="<reason>"`. Recorded via `gg record`.

Install gitleaks first for full coverage: `gg doctor --install-secret-scanner`.
Without gitleaks the hook falls back to the narrow-regex patterns in `internal/scrub`.

### `GG_GITLEAKS_BIN` — gitleaks binary override

Override the gitleaks binary path used by the pre-commit hook and
`gg doctor --check-secrets`. Takes precedence over `~/.gg/bin/gitleaks`
and the `$PATH` lookup.

```sh
GG_GITLEAKS_BIN=/opt/homebrew/bin/gitleaks git commit ...
```

### `GG_COMMIT_MSG_GATE` — commit-message convention gate (`commit-msg.d/30-commit-msg.sh`)

Checks the commit subject (length, no file paths/source filenames, optional
prefix). This is a `commit-msg` hook (it needs the message, which `pre-commit`
never sees). **Default off** so it is inert until a project opts in — safe to
propagate everywhere via `gg system sync`.

| Value | Effect |
|---|---|
| `off` *(default)* | Gate is skipped entirely (inert) |
| `warn` | Prints convention advice; does not block the commit |
| `on` | Exits 7 (blocks the commit) on a violation |

Per-project config can also live in `.gg/commit-msg.conf` (shell `key=value`,
sourced by the hook); a real environment variable overrides the file.

**Bypass (audited):** set `GG_BYPASS_RATIONALE="<reason>"`.

### `GG_COMMIT_MSG_MAX_SUBJECT` — max subject length

Maximum commit subject length before the gate flags it. Default `72`.

### `GG_COMMIT_MSG_PREFIX` — required subject prefix (ERE)

Optional extended-regex the subject must match, e.g.
`^(feat|fix|chore|docs|refactor|test|perf|build|ci)(\(.+\))?: `. Empty (default)
means the prefix is not checked — projects set their own format.

### `GG_COMMIT_MSG_ALLOW_FILENAMES` — allow file names in the subject

Set to `1` to disable the "no file path / source filename in subject" check.
Default `0` (the check is active when the gate is on/warn).

### `GG_IMPACT_ATTESTATION` — impact-analysis gate (`60-impact-attestation.sh`)

Requires an `Impact-Reviewed:` trailer in the commit body when ≥3 source
files change OR any changed file has ≥5 graph dependents. Below the
thresholds it prints an advisory and exits 0.

| Value | Effect |
|---|---|
| `on` *(default)* | Exits 7 when trailer is missing at mandatory threshold |
| `warn` | Prints advisory; does not block |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_BYPASS_RATIONALE="<reason>"`. Recorded via
`gg record`. The bypass is the same env var used by the global gate runner.

Sample commit trailer:
```
Impact-Reviewed: cmd/task_status.go — 2 callers, tests green
Impact-Reviewed: internal/store/client.go — 0 callers
```

### `GG_REVIEW_CONVERGENCE` — review-convergence gate (`70-review-convergence.sh`)

Requires a `Review-Convergence:` trailer in the commit body before
`gg task done` can close the task. The trailer attests that the implementer
ran the pre-done matrix: behavior matrix, negative path, legacy compatibility,
stale-string sweep, docs/templates/generated artifacts, live smoke, and
test/diff evidence.

| Value | Effect |
|---|---|
| `on` *(default)* | Exits 7 when trailer is missing |
| `warn` | Prints the missing-trailer checklist; does not block |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_ALLOW_INCOMPLETE_REVIEW="<reason>"`. Recorded
via `gg record`.

Sample commit trailer:
```
Review-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified
```

### `GG_ENFORCEMENT` — regression repro gate (`90-bug-repros.sh`) + global kill-switch

`GG_ENFORCEMENT` is used in two overlapping ways:

1. **`90-bug-repros.sh` mode:**

   | Value | Effect |
   |---|---|
   | `on` *(default)* | Runs `gg bug run-repros`; exits 7 on any regression |
   | `warn` | Runs repros; prints warning but does not block |
   | `off` | Skips the repro run entirely |

2. **Global enforcement kill-switch** (used by the CLI gate runner and
   bypass rationale check): `GG_ENFORCEMENT=off` disables structural gates
   (inbox, verify). As of TASK-317, `GG_ENFORCEMENT=off` alone is
   **rejected** — you must also set `GG_BYPASS_RATIONALE` or
   `GG_BYPASS_RATIONALE_RECORD`. See the [bypass variables](#bypass-variables)
   section.

### `GG_BUG_REPRO_BUDGET` — repro time budget

Sets the wall-clock budget (seconds) passed to `gg bug run-repros --budget`.
Default `30`. Raise when the number of fixed bugs has grown beyond the default
budget:

```sh
GG_BUG_REPRO_BUDGET=60 gg task done TASK-042 "..."
```

### `GG_NO_SMOKE` — smoke-test gate (`05-smoke-e2e.sh`)

| Value | Effect |
|---|---|
| `0` *(default)* | Gate runs `make test-smoke` if the Makefile target exists |
| `1` | Gate is skipped; no-op exit |

The gate is already a no-op when there is no `Makefile` or no `test-smoke`
target, so `GG_NO_SMOKE=1` is only needed as an intentional per-session
opt-out.

## Bypass variables

These interact with the global enforcement gate and are validated by the CLI,
not by an individual hook script.

### `GG_BYPASS_RATIONALE`

Free-form rationale text required when `GG_ENFORCEMENT=off`. For task-scoped
gates the value **must** begin with `TASK-NNN:` matching the task being
closed — cross-task rationale recycling is rejected.

```sh
GG_ENFORCEMENT=off \
GG_BYPASS_RATIONALE="TASK-042: agent-lifecycle gate misfires on master role" \
  gg task done TASK-042 "..."
```

The CLI auto-promotes the rationale text to a brain record and links its UUID
into the bypass audit entry (TASK-318). The bypass is visible in
`gg doctor --bypass-audit`.

### `GG_BYPASS_RATIONALE_RECORD`

Integrity-grade alternative to `GG_BYPASS_RATIONALE`. Provide an existing
gg record UUID instead of free-form text. The UUID is stored directly in
`BypassEntry.RationaleRecordID`, making the bypass permanently searchable via
`gg search`.

```sh
GG_ENFORCEMENT=off \
GG_BYPASS_RATIONALE_RECORD=<record-uuid> \
  gg task done TASK-042 "..."
```

Either `GG_BYPASS_RATIONALE` or `GG_BYPASS_RATIONALE_RECORD` satisfies the
gate. Providing both is fine; `GG_BYPASS_RATIONALE_RECORD` takes precedence.

### `GG_ALLOW_INBOX_SKIP`

Bypasses the inbox handoff-evidence gate. Use only when the unread role-targeted
messages have already been read or answered elsewhere; otherwise future agents
may miss blockers, review requests, or evidence handoffs. Value is a free-form
reason string.

```sh
GG_ALLOW_INBOX_SKIP="continuing async sprint, messages already triaged" \
  gg task start TASK-099
```

### `GG_INBOX_GATE_WINDOW`

Bounds how far back the inbox handoff gate blocks on role-targeted handoffs
(BUG-103). Once agent identity is resolved per session/tab, a fresh identity has
an empty per-recipient read set, so without a window the gate would re-block on
the entire accumulated handoff history for every new tab. The window makes the
candidate set proportional to recent activity by a wall-clock cutoff, independent
of identity/cursor/read-state.

- Unset/empty → **14d** (default, on).
- A parseable duration overrides — Go's `time.ParseDuration` plus a `d` (day)
  suffix: `7d`, `48h`, `30m`.
- `0` or `off` → **disabled** (legacy unbounded: block role-targeted handoffs of
  any age — the maximum-safety setting).
- A parse failure falls back to 14d (never unbounded), so a typo cannot silently
  resurrect the backlog.

A handoff older than the window is not lost — it still appears in
`gg inbox --role <role> --include-agents` (no window there) and, if task-linked,
via `gg next`; the gate is a backstop, not the sole notifier.

```sh
GG_INBOX_GATE_WINDOW=7d gg task start TASK-099   # narrower window
GG_INBOX_GATE_WINDOW=0  gg task start TASK-099    # unbounded (legacy)
```

---

## Pre-tool-use hook variables

These are injected by the **Claude Code harness** (not by gg) before running
scripts in `.gg/hooks/pre-tool-use.d/`. They are only available in that hook
category, not in `pre-task-done.d/` or `task-done.d/`.

| Variable | Value | Example |
|---|---|---|
| `GG_TOOL_NAME` | The tool being invoked by the agent | `Edit`, `Write`, `MultiEdit` |

---

## Notification and CI variables

| Variable | Effect |
|---|---|
| `GG_NO_AUTO_NOTIFY=1` | Suppresses the cross-agent `gg tell` broadcast on gate failure. Exit code 7 and NDJSON event still fire. Use in CI, reentrant hook scripts, or tests that can't depend on a live store. |
| `GG_DEBUG=1` | Prints a one-line diagnostic to stderr when the gate silently falls through (`.gg` missing, unreadable config). |

---

## Agent identity variables

These are typically set once per shell session or injected via
`settings.json` (see `gg doctor --install-hooks`).

| Variable | Effect |
|---|---|
| `GG_AGENT` | Tags every gg call as agent-initiated in telemetry. Set to `claude-code`, `gsd`, `cursor`, etc. Required for agent-initiated call separation in `gg status` metrics. Also the fallback provenance stamped on durable writes. |
| `GG_ROLE` | Role of the current session (`developer`, `master`, `qa`, …). Used as `GG_ACTOR` in hook env; also read by inbox routing, and the preferred provenance on durable writes. |
| `GG_REQUIRE_AUTHOR` | Opt-in strict provenance. `1`/`true`/`yes`/`on` makes an unattributable write (`gg record`, `decide`, `reject`, `bug report`, `task create`, `task cancel`, `canon set`) fail instead of landing anonymously. Default off. |

`GG_ACTOR` inside a hook is always derived from these two: `GG_ROLE` takes
priority; `GG_AGENT` is the fallback. **Never set `GG_ACTOR` directly** in a
hook — the runner sets it for you.

### Provenance on durable writes (BUG-106)

Every durable write stamps an author, resolved through one ladder:

```
--from <role>  →  $GG_ROLE  →  the agent identity (GG_AGENT, sharpened per tab)  →  "" 
```

The role wins over the agent id on purpose: an exported `GG_ROLE` is the
provenance the operator *means*, while the agent id is merely the runtime that
executed the command. `gg tell` uses the same ladder with `user` as its final
fallback, which is truthful only in a bare human shell.

Before BUG-106 the ladder stopped at `GG_ROLE`, so an agent that never exported
a role wrote `author=""` — even though `requireAgentIdentity()` had already
accepted that same session's `GG_AGENT` at the door. The identity was verified
and then discarded. An author that still cannot be resolved now renders as
`[anonymous]` rather than being silently omitted, mirroring how absent evidence
renders `[unverified]`.

Projects with a written provenance convention should set
`GG_REQUIRE_AUTHOR=1` so the convention is enforced by the tool rather than by
memory.

---

## Observability variables

| Variable | Effect |
|---|---|
| `GG_TRACE=1` | Appends span records to `.gg/traces/YYYY-MM-DD.jsonl`. Disable: unset or `GG_TRACE=0`. View: `gg trace show` / `gg trace summary`. |
| `GG_TELEMETRY=0` | Disables local call-count telemetry written to `~/.gg/projects/<id>/telemetry.json`. Override: `GG_TELEMETRY=1` always enables regardless of `.gg/config.yaml`. |
| `GG_COMPACT=1` | Forces compact output (single-line-per-item) on supported commands regardless of terminal width. Accepts `1/true/yes/on`; disable with `0/false/no/off`. |
| `GG_QUIET=1` | Suppresses informational banners on commands that emit them (scripting / CI contexts). |
| `GG_SESSION_ID` | Overrides the session ID used for telemetry grouping. Normally derived from `CLAUDE_SESSION_ID` automatically; set this when running outside Claude Code. |
| `GG_UPDATE_CHECK=1` | Enables the optional `gg session-start` public version check. This performs a Go module network lookup and only prints a notice; it never installs automatically. |

---

## Variable propagation across process boundaries (TASK-308 lesson)

The TASK-308 regression happened because `GG_INSIDE_HOOK` was not being
propagated into the subprocess spawned by the hook runner. The fix
(`taskHookEnv` in `cmd/task_hooks.go`) now explicitly constructs the env map
and merges it over `os.Environ()`. This is the correct pattern.

**Rules for hook authors:**

1. Variables you rely on from the _parent_ process (`GG_ROLE`, `GG_AGENT`,
   `GG_ENFORCEMENT`, etc.) are inherited automatically via `os.Environ()`.
   You do not need to re-export them.

2. Variables injected by `taskHookEnv` (`GG_TASK_ID`, `GG_TASK_SUMMARY`,
   `GG_PROJECT_ID`, `GG_ACTOR`, `GG_INSIDE_HOOK`) are always present in hook
   subprocesses. If you spawn a child process _from inside a hook_, you must
   forward these yourself:

   ```sh
   # Wrong — child loses GG_INSIDE_HOOK:
   ./build-helper.sh
   
   # Correct — forward hook context:
   GG_INSIDE_HOOK=1 GG_TASK_ID="$GG_TASK_ID" ./build-helper.sh
   ```

3. When writing a new hook that reads a control variable (e.g.
   `MY_GATE_MODE`), document it here so it appears in the canonical list.
   An undocumented variable is an undiscoverable variable — the next agent
   will disable the gate by accident.

---

## Quick reference

| Variable | Where set | Read by |
|---|---|---|
| `GG_TASK_ID` | `taskHookEnv` (runner) | all hooks |
| `GG_TASK_SUMMARY` | `taskHookEnv` (runner) | all hooks |
| `GG_PROJECT_ID` | `taskHookEnv` (runner) | all hooks |
| `GG_ACTOR` | `taskHookEnv` (runner, derived from `GG_ROLE`/`GG_AGENT`) | all hooks |
| `GG_INSIDE_HOOK` | `taskHookEnv` (runner) | `10-go-verify.sh`, test helpers |
| `GG_AC_ATTESTATION` | caller / session | `50-ac-attestation.sh` |
| `GG_AC_ATTESTATION_REASON` | caller / required rationale for `=off` (audited) | `50-ac-attestation.sh` |
| `GG_ALLOW_INCOMPLETE_AC` | caller / one-shot bypass | `50-ac-attestation.sh` |
| `GG_DECIDE_GATE` | caller / session | `20-decide-capture.sh` |
| `GG_REVIEW_GATE` | caller / session | `40-review-required.sh` |
| `GG_FILE_SIZE_GATE` | caller / session | `30-file-size.sh` |
| `GG_ALLOW_FILE_SIZE` | caller / one-shot bypass | `30-file-size.sh` |
| `GG_STUB_GATE` | caller / session | `35-stub-scan.sh` |
| `GG_ALLOW_STUB` | caller / one-shot bypass | `35-stub-scan.sh` |
| `GG_DEP_GATE` | caller / session | `45-dependency-justification.sh` |
| `GG_ALLOW_DEP` | caller / one-shot bypass | `45-dependency-justification.sh` |
| `GG_LINT_GATE` | caller / session | `60-lint-gate.sh` |
| `GG_SECRET_SCAN` | caller / session | `20-secret-scan.sh` |
| `GG_GITLEAKS_BIN` | caller / override | `20-secret-scan.sh`, `gg doctor --check-secrets` |
| `GG_ALLOW_LINT_REGRESSIONS` | caller / one-shot bypass | `60-lint-gate.sh` |
| `GG_IMPACT_ATTESTATION` | caller / session | `60-impact-attestation.sh` |
| `GG_REVIEW_CONVERGENCE` | caller / session | `70-review-convergence.sh` |
| `GG_REVIEW_CONVERGENCE_REASON` | caller / required rationale for `=off` (audited) | `70-review-convergence.sh` |
| `GG_ALLOW_INCOMPLETE_REVIEW` | caller / one-shot bypass | `70-review-convergence.sh` |
| `GG_ENFORCEMENT` | caller / session | `90-bug-repros.sh`, CLI gate runner |
| `GG_BUG_REPRO_BUDGET` | caller / session | `90-bug-repros.sh` |
| `GG_NO_SMOKE` | caller / session | `05-smoke-e2e.sh` |
| `GG_BYPASS_RATIONALE` | caller / bypass | CLI gate runner |
| `GG_BYPASS_RATIONALE_RECORD` | caller / bypass | CLI gate runner |
| `GG_ALLOW_INBOX_SKIP` | caller / bypass | inbox gate |
| `GG_NO_AUTO_NOTIFY` | CI / session | gate runner |
| `GG_DEBUG` | caller | gate runner |
| `GG_AGENT` | `settings.json` / session | telemetry, inbox cursor, actor, write provenance |
| `GG_REQUIRE_AUTHOR` | caller / session | write verbs (`record`, `decide`, `reject`, `bug report`, `task create`, `task cancel`, `canon set`) |
| `GG_ROLE` | session | inbox routing, actor |
| `GG_TRACE` | session | `internal/trace` |
| `GG_TELEMETRY` | session | `internal/telemetry` |
| `GG_COMPACT` | session | compact renderer |
| `GG_QUIET` | CI / session | banner printer |
| `GG_SESSION_ID` | session | telemetry session grouping |
| `GG_UPDATE_CHECK` | session | optional session-start update notice |
