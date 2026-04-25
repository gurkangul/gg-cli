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
hook, the parent `gg` process already holds live Qdrant connections and
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

### `GG_LINT_GATE` — lint regression gate (`60-lint-gate.sh`)

| Value | Effect |
|---|---|
| `on` *(default)* | Exits 7 when lint error count increased |
| `warn` | Prints warning; does not block |
| `off` | Gate is skipped entirely |

**Bypass (audited):** set `GG_ALLOW_LINT_REGRESSIONS="<reason>"`. Recorded
via `gg record`.

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
Impact-Reviewed: cmd/spawn_worker.go — 2 callers, tests green
Impact-Reviewed: internal/store/client.go — 0 callers
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

### `GG_NO_MASTER_GUARD` — worker liveness check

| Value | Effect |
|---|---|
| `0` *(default)* | Worker verifies master heartbeat before closing a task |
| `1` | Liveness check is skipped |

Used in the `worker-liveness-check.sh` hook installed by `gg spawn worker`.

---

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

Bypasses the inbox-obedience gate (unread role-targeted messages must be
handled before starting new work). Value is a free-form reason string.

```sh
GG_ALLOW_INBOX_SKIP="continuing async sprint, messages already triaged" \
  gg task start TASK-099
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

## Spawn and queue advance variables

These are optional path hints injected by the queue runner or spawn subsystem.
When present they short-circuit a `gg spawn status` subprocess call inside the
hook, making the hook cheaper at high invocation rate.

| Variable | Value | Where set |
|---|---|---|
| `GG_SPAWN_DIR` | Absolute path to the spawn state directory (contains `queue.json`) | `gg spawn worker` session env |
| `GG_SPAWN_ADVANCE_DIR` | Absolute path to the advance-sentinel directory polled by the queue runner | `gg spawn worker` session env |

Both variables are optional. When absent, the scripts fall back to `gg spawn
status --json` or `gg config get runtime_dir` discovery.

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
| `GG_AGENT` | Tags every gg call as agent-initiated in telemetry. Set to `claude-code`, `gsd`, `cursor`, etc. Required for agent-initiated call separation in `gg status` metrics. |
| `GG_ROLE` | Role of the current session (`developer`, `master`, `qa`, …). Used as `GG_ACTOR` in hook env; also read by inbox routing. |

`GG_ACTOR` inside a hook is always derived from these two: `GG_ROLE` takes
priority; `GG_AGENT` is the fallback. **Never set `GG_ACTOR` directly** in a
hook — the runner sets it for you.

---

## Observability variables

| Variable | Effect |
|---|---|
| `GG_TRACE=1` | Appends span records to `.gg/traces/YYYY-MM-DD.jsonl`. Disable: unset or `GG_TRACE=0`. View: `gg trace show` / `gg trace summary`. |
| `GG_TELEMETRY=0` | Disables local call-count telemetry written to `~/.gg/projects/<id>/telemetry.json`. Override: `GG_TELEMETRY=1` always enables regardless of `.gg/config.yaml`. |
| `GG_COMPACT=1` | Forces compact output (single-line-per-item) on supported commands regardless of terminal width. Accepts `1/true/yes/on`; disable with `0/false/no/off`. |
| `GG_QUIET=1` | Suppresses informational banners on commands that emit them (scripting / CI contexts). |
| `GG_SESSION_ID` | Overrides the session ID used for telemetry grouping. Normally derived from `CLAUDE_SESSION_ID` automatically; set this when running outside Claude Code. |

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
| `GG_ALLOW_INCOMPLETE_AC` | caller / one-shot bypass | `50-ac-attestation.sh` |
| `GG_DECIDE_GATE` | caller / session | `20-decide-capture.sh` |
| `GG_REVIEW_GATE` | caller / session | `40-review-required.sh` |
| `GG_FILE_SIZE_GATE` | caller / session | `30-file-size.sh` |
| `GG_ALLOW_FILE_SIZE` | caller / one-shot bypass | `30-file-size.sh` |
| `GG_LINT_GATE` | caller / session | `60-lint-gate.sh` |
| `GG_ALLOW_LINT_REGRESSIONS` | caller / one-shot bypass | `60-lint-gate.sh` |
| `GG_IMPACT_ATTESTATION` | caller / session | `60-impact-attestation.sh` |
| `GG_ENFORCEMENT` | caller / session | `90-bug-repros.sh`, CLI gate runner |
| `GG_BUG_REPRO_BUDGET` | caller / session | `90-bug-repros.sh` |
| `GG_NO_SMOKE` | caller / session | `05-smoke-e2e.sh` |
| `GG_NO_MASTER_GUARD` | worker session | `worker-liveness-check.sh` |
| `GG_BYPASS_RATIONALE` | caller / bypass | CLI gate runner |
| `GG_BYPASS_RATIONALE_RECORD` | caller / bypass | CLI gate runner |
| `GG_ALLOW_INBOX_SKIP` | caller / bypass | inbox gate |
| `GG_NO_AUTO_NOTIFY` | CI / session | gate runner |
| `GG_DEBUG` | caller | gate runner |
| `GG_AGENT` | `settings.json` / session | telemetry, inbox cursor, actor |
| `GG_ROLE` | session | inbox routing, actor |
| `GG_TRACE` | session | `internal/trace` |
| `GG_TELEMETRY` | session | `internal/telemetry` |
| `GG_COMPACT` | session | compact renderer |
| `GG_QUIET` | CI / session | banner printer |
| `GG_SESSION_ID` | session | telemetry session grouping |
| `GG_TOOL_NAME` | Claude Code harness | `50-master-guard.sh` |
| `GG_SPAWN_DIR` | `gg spawn worker` session | `50-master-guard.sh` |
| `GG_SPAWN_ADVANCE_DIR` | `gg spawn worker` session | `45-queue-advance.sh` |
