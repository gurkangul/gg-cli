# Pre-done verify gate

`gg task done` runs every executable `*.sh` in
`.gg/hooks/pre-task-done.d/` **before** updating the task state. If any
script exits non-zero, the transition is aborted with exit code `7`
(`ExitVerifyFailed`) and the task stays in its current state.

This turns gg from a passive notebook into an active checkpoint — an agent
cannot mark a task done if the build is broken, tests fail, or any custom
gate refuses the transition.

## One-command install

```
gg doctor --install-task-hooks
```

The installer walks the project up to depth 3, detects every `go.mod` and
`package.json`, and drops a per-subdirectory starter script into
`.gg/hooks/pre-task-done.d/`. Monorepo layouts (`lift-cli/go.mod`,
`packages/api/package.json`) are first-class; each landing script cds
into its manifest directory before running checks. Existing files are
never overwritten — user edits survive re-runs.

Skipped directories (never descended) include `.git`, `.gg`, `.gsd`,
`node_modules`, `vendor`, `dist`, `build`, `_bmad`, `_bmad-output`.
Override or extend via `.gg/config.yaml`:

```yaml
doctor:
  hook_install:
    skip_dirs: [".git", ".gg", "node_modules", "my-vendored-lib"]
    max_depth: 2
```

## Hook contract

Hooks are plain shell scripts, run in lexicographic order. Each receives
the following environment variables:

| Variable | Meaning |
|----------|---------|
| `GG_TASK_ID` | The task ID being closed, e.g. `TASK-042`. |
| `GG_TASK_SUMMARY` | The summary string passed to `gg task done`. |
| `GG_PROJECT_ID` | The project UUID from `.gg/config.yaml`. |
| `GG_ACTOR` | `$GG_ROLE` or `$GG_AGENT` of the caller. |

The same env contract is reused by future gates (e.g. `pre-review-approve.d`),
so scripts stay portable across gate stages.

## Built-in review-convergence gate

`gg doctor --install-task-hooks` installs `70-review-convergence.sh`.
It blocks `gg task done` unless the task commit contains a
`Review-Convergence:` trailer showing the implementer ran the pre-done
convergence matrix:

1. behavior matrix
2. negative path
3. legacy compatibility
4. stale-string sweep
5. docs/templates/generated artifacts
6. live smoke
7. test/diff evidence

Example commit body line:

```
Review-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified
```

Modes: `GG_REVIEW_CONVERGENCE=on|warn|off` (`on` by default).
Intentional incomplete review requires
`GG_ALLOW_INCOMPLETE_REVIEW="<reason>"`; the hook audits the bypass via
`gg record`.

## Rejection signals

Three independent signals fire in parallel on rejection, so naive callers
and structured parsers both react cleanly:

### 1. Exit code and human line

Exit code `7` plus a stderr message:

```
error: pre-task-done hook rejected TASK-042: 10-build.sh exited 1 (task state unchanged)
```

### 2. NDJSON event on stderr

Single-line, stable keys, human-friendly field order:

```json
{"event":"verify_failed","gate":"pre-task-done","task":"TASK-042","hook":"10-build.sh","exit":1,"ts":"2026-04-18T09:12:33Z","detail":"build: cannot find module foo"}
```

Field meanings:

| Key | Type | Stability | Meaning |
|-----|------|-----------|---------|
| `event` | string | stable | Always `"verify_failed"` today. |
| `gate` | string | stable | Which gate fired (`"pre-task-done"` today; more gates later share this envelope). |
| `task` | string | stable | Subject task ID. |
| `hook` | string | stable | Filename of the failing script (e.g. `10-build.sh`). |
| `exit` | number | stable | Hook's process exit code. |
| `ts` | string | stable | RFC3339 UTC timestamp of the rejection. |
| `detail` | string | best-effort | Trailing bytes of the hook's combined stdout/stderr, trimmed. |

Parse it with `jq`, `tail -1`, or any JSON reader. Agents should program
against the keys; the line ordering in stderr may include surrounding
hook output.

### 3. Auto-broadcast via `gg tell`

The CLI sends a message from role `verify-gate` to `all`, linked to the
task:

```
[verify-gate → all] TASK-042 blocked at pre-task-done: 10-build.sh (exit 1) — cannot find module foo
```

Parallel agent sessions see it in the next `gg inbox` / `gg status` — no
per-agent plumbing needed.

Best-effort: if the store is unreachable the notification is silently
dropped so a failed broadcast never masks the underlying verify failure.

## Escape valves

| Variable | Effect |
|----------|--------|
| `GG_NO_AUTO_NOTIFY=1` | Suppress the cross-agent broadcast. Exit code and NDJSON event still fire. Use in CI, reentrant hook scripts, or tests that can't depend on a live store. |
| `GG_DEBUG=1` | Print a one-line diagnostic to stderr when the gate silently falls through (missing `.gg`, unreadable config). |

## Exit codes

| Code | Constant | Meaning |
|------|----------|---------|
| `0` | `ExitOK` | Success. |
| `1` | `ExitGeneral` | General error. |
| `2` | `ExitNotFound` | Resource not found. |
| `3` | `ExitConfig` | Config / init error (run `gg init`). |
| `4` | `ExitService` | Service unreachable (Qdrant / Ollama / Memgraph). |
| `6` | `ExitStoreDown` | Store down — writes blocked, reads served from cache. |
| `7` | `ExitVerifyFailed` | Pre-task-done hook rejected the transition; task state unchanged. |
| `130` | `ExitSignal` | Interrupted (Ctrl+C). |

## AC attestation gate — simplified detection rationale

The original TASK-300 spec described detection rules in three broad buckets:

- **(a)** explicit `AC-N:` line in commit body
- **(b)** test name containing the AC number
- **(c)** file path or function reference implementing the AC

The shipped hook (`50-ac-attestation.sh`) applies five concrete rules. The
mapping from spec buckets to shipped rules is:

| Spec bucket | Shipped rules |
|-------------|---------------|
| (a) commit-body text | (a) `AC-N:` line, (b) `N.`/`N)` numbered line, (c) `AC N` phrase |
| (b) test name | (d) `TestACN_*` or `TestGapN_*` in diff added lines or changed file paths |
| (c) file/func ref | (e) `func acN_*`, `// AC-N`, `// Gap N` in diff added lines |

**Why the simplification:** the original (b)/(c) categories were implemented
as diff-content scans rather than static file-path heuristics. Static
file-path detection (e.g. "does `ac1_feature.go` exist in the repo?") was
considered but deferred for two reasons:

1. **Portability.** `git log -1 --name-only` is universally available in the
   hook environment; reading the working tree requires knowing the repo root
   and introduces edge cases around untracked files, renames, and submodules.
2. **Signal quality.** A file existing in the tree doesn't prove the commit
   *in question* touched it. Diff-content scanning (`git log -1 -p`) ties the
   evidence to the specific commit, preventing stale-file false positives.

The narrowing from "file path or function reference" to "added lines in the
commit diff" is deliberately tighter — it avoids matching unchanged files from
prior commits. This is a pragmatic trade-off, not an accidental omission.
Follow-on work (TASK-312) extended the parser to exclude FIX/REWORK-step
bullets from AC extraction.

## AC parser fixture matrix

The parser logic is regression-tested via a fixture matrix in
`cmd/testdata/ac_parser/`. Each fixture pair is:

- `<name>.txt` — a Detail field fed verbatim to the Python parser
- `<name>.expected.json` — `{"count": N, "labels": ["AC-1", …]}`

The test (`TestACParserFixtures` in `cmd/hook_ac_parser_fixtures_test.go`)
extracts the Python block directly from the hook script, runs it against each
fixture, and asserts the count and sequential labels match.

Adding a fixture prevents a class of regression: silent parser tightening
that changes what counts as an AC without a test failing. To add a new
fixture, drop a `.txt` + `.expected.json` pair into the directory — no test
code changes needed.

Current fixture categories:

| Prefix | What it covers |
|--------|---------------|
| `happy_` | Normal inputs: explicit `AC-N:` lines, ACCEPTANCE bullets, numbered items, code-block prose |
| `edge_` | Boundary cases: empty AC list, single AC, inline prose references, ACs spread across paragraphs |
| `adversarial_` | Parser traps: ACs inside code fences (still counted), FIX/REWORK bullets (excluded), narrative Gap prose (excluded), mixed languages |

## Troubleshooting

- **Exit 7 in CI with no clear cause.** Grep stderr for
  `"event":"verify_failed"` — the NDJSON line names the failing hook and
  includes the trimmed stderr tail.
- **Hook never fires.** Check that the script is executable (`chmod +x`)
  and has a `.sh` suffix. Non-executable files are silently skipped.
- **Gate doesn't install in a subdirectory project.** Confirm the
  manifest (`go.mod` / `package.json`) lives within
  `doctor.hook_install.max_depth` levels of the repo root, and that no
  ancestor directory is in the skip list.
- **Want to bypass the gate temporarily.** Don't. If a hook is wrong,
  edit the script — the gate exists precisely to catch "I forgot to
  run tests before marking done" drift.

## Symlinks and nested gg projects

`findManifestDirs` does not follow symbolic links, so a symlinked package
directory (e.g. `packages/foo → ../../shared/foo`) is not descended into;
write a manual hook if your monorepo depends on symlinked subprojects.

A nested gg project at `services/api/.gg/` is invisible to the outer
walk by design (`.gg` is in the skip list), so each nested project has
its own installer run.

## Related

- `docs/hook-env-vars.md` — complete reference for every `GG_*` variable
  that hooks read or emit, including propagation rules and the TASK-308 lesson.
