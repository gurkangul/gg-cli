# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.13.0] - 2026-08-07

2.12.0 made the checkable baseline lines enforceable. This release retires the
model those gates were built around: the second *window* is gone, the second
*party* stays (TASK-537).

> **Upgrade order matters: binary first, sync second.** Until a machine is
> running 2.13.0, its `gg` carries the 2.12.0 contract and will overwrite this
> release's contract block in all six agent entry points — and not only via
> `doctor --check-contract --fix`: the session-start resync rewrites the block
> too, so a stale binary reverts the contract with no user action at all
> (BUG-110). Update every machine's `gg` before running `gg system sync`.

### Added

- **The managed agent contract now describes single-session operation
  (TASK-537).** The two-window model — a master pane coordinating a side-pane
  worker — was written when the only way to get a second reader was a second
  process. That machinery is gone: the cmux/spawn orchestration was retired in
  May, `gg spawn` is not a command, and
  `.gg/hooks/pre-tool-use.d/50-master-guard.sh` turned out to be dead code no gg
  path ever invoked. One session may now drive a whole task — plan, implement,
  review — but it may never be its own reviewer. Independence is a property of
  context isolation, not of window count: the review belongs to a subagent that
  never saw the implementation reasoning, carrying its own `GG_AGENT`/`GG_ROLE`
  so the separation gate records a genuine second party. Nothing was loosened to
  get there — `verifier_separation`, `checkReadyForLiveGate` and
  `checkAgentLifecycleGate` are untouched, and final closure still belongs to
  the owner.

- **`RemoveObsoleteHooks` — retired managed hook commands are pruned from
  `.claude/settings.json`.** gg has always retired obsolete CLAUDE.md blocks;
  hook entries in JSON settings had no equivalent, so deleting an installer only
  stopped new installs and left every already-written entry in place. Cleanup
  runs from the same two call sites as the block cleanup — the session-start
  resync and `doctor --check-contract --fix` — so installed projects heal
  themselves without anyone running a repair command. Narrow by construction:
  only exact gg-owned commands are removed, a matcher entry that still runs
  something else keeps it, an entry that *arrived* with an empty hooks list is
  somebody's disabled hook and is left alone, and a settings file that will not
  parse is reported on stderr and skipped rather than rewritten — a bystander's
  syntax error must never abort a contract repair.

- **`gg task start` now pushes the task-scoped memory packet (TASK-538).** A
  successful claim prints an `=== Related Context ===` block: the top-3
  decisions, rejected approaches, and notes semantically related to the task
  being claimed. This is not a new capability — the block shipped in April
  behind `gg task get --with-context`, where it was measured at 1/518 calls and
  again at 0/482 calls, roughly 0% against its recorded 30% adoption gate. An
  opt-in read path is not read. Claiming is the first moment the topic is known,
  so the packet is pushed there instead of waiting to be asked for. The
  renderer, the top-3 limit, and the ~800-token cap are reused unchanged.

- **`--no-context` on `gg task start`.** Suppresses the block for scripted and
  CI callers that only want the lifecycle line.

- **`task-start memory push` line in `gg telemetry summary`.** Counts pushed
  packets and their bytes, reported separately from `--with-context`. The two
  answer different questions — what agents chose to pull versus what gg pushed
  whether they asked or not — and collapsing them would hide the adoption gap
  that motivated the push in the first place. The line splits delivered packets
  from empty or degraded ones and warns when over half carried no records: a
  push count that silently included outages would report memory flowing while
  nothing was reaching anyone.

### Fixed

- **A PreToolUse hook pointed at a command gg no longer ships (BUG-109).**
  `gg dev-role-guard` was wired with matcher `Bash`, so it ran — and exited 1 —
  on every Bash tool call in the project. Claude Code treats a PreToolUse exit 1
  as non-blocking, so nothing ever surfaced: the guard the project believed it
  had was not running at all. The retirement machinery above removes the stale
  entry at the next session start.

- **The telemetry summary no longer calls a healthy backend broken (TASK-543).**
  Pushed packets now split three ways instead of two — `delivered`, `empty`,
  `failed` — and only `failed` triggers the "check the embedding backend and
  vector store" warning. Previously an empty packet shared a bucket with a failed
  one, so a fresh project that had simply not recorded anything yet was told to
  go debug a working vector store. This is the same empty-vs-failed conflation
  fixed one layer down in the renderer; it needed fixing in both places. The
  verdict is stored as one `context_outcome` string rather than derived at read
  time, so entries written before it existed carry no verdict and stay out of
  all three buckets instead of having one backdated onto them.

- **Related-context records no longer inflate command usage (TASK-540).** A
  context packet is a side-record *about* an invocation, not an invocation, but
  the summary counted it as one — inventing a phantom `task-start` row beside the
  real `start`, colliding the pull path's record with the genuine top-level
  `task` verb, and overstating the `N calls` headline that the North Star
  adoption number is read from. These records now feed only their own counters.

- **A failed related-item search no longer reports itself as an empty brain.**
  `fetchRelatedContext` discarded the errors from its three searches, so a
  timeout or a missing collection rendered `(no related items found)` —
  indistinguishable from "nothing was ever decided here", which is the worst
  possible thing to tell an agent that is about to make a decision. Failures now
  render `(unavailable — related-item search failed; treat as unknown, not as
  empty)`, and a partial result is labelled incomplete. This was pre-existing
  and also affects `gg task get --with-context`, which shares the renderer; it
  became urgent because the block is now on the default path of every claim.

### Changed

- `gg task start --json` gained an additive `related_context` field. Existing
  keys are unchanged; the field is omitted under `--no-context`. A dead
  embedding backend degrades the block to a one-line notice and still exits 0 —
  the memory packet must never cost you the claim.

- The related-context packet moved from `cmd/task_list.go` to
  `cmd/task_related_context.go`. It has two consumers now — pulled by
  `gg task get --with-context`, pushed by `gg task start` — so it belongs to
  neither renderer, and the move keeps `task_list.go` under the 500-line limit.

### Removed

- `.gg/hooks/pre-tool-use.d/50-master-guard.sh`. No gg code path iterates
  `.gg/hooks/pre-tool-use.d/` — `RunHooks` is only ever called with `task-done`
  and `pre-task-done` — so the script never ran, and it routed implementation to
  a `gg spawn` command that does not exist. `docs/hook-env-vars.md` now says the
  directory is not executed, and drops its claim that the harness injects
  `GG_TOOL_NAME`: nothing sets that variable, and Claude Code passes the tool
  name as the stdin JSON field `tool_name`.

## [2.12.0] - 2026-08-03

2.11.0 wrote the engineering baseline down. This release makes the two lines
that *can* be checked stop being promises (TASK-536).

### Added

- **`35-stub-scan.sh` — stub gate (`GG_STUB_GATE`, default `warn`).** Flags stub
  markers the task's diff *adds* to a source file: `TODO`, `FIXME`, `XXX`,
  `HACK`, `not implemented`, `unimplemented`. The scope is where the value is.
  Only **added** lines are read, so a marker already living in a file you happen
  to touch is somebody else's debt and never blocks you — a whole-file scan
  would punish whoever walks past next instead of whoever left it. Only source
  extensions are scanned, so prose in a `.md` file naming the markers is never a
  finding. Matching is whole-token, so `TODOS_ENDPOINT` is an identifier rather
  than a stub.

- **`45-dependency-justification.sh` — dependency gate (`GG_DEP_GATE`, default
  `warn`).** When a package manifest gains a dependency, the gate asks whether a
  decision linked to the task names it. A lockfile records the fact that a
  dependency was added and never the reason, and the reason is the part a future
  agent cannot reconstruct. Version bumps do **not** trigger it: the name is
  present on both sides of the diff, so only genuinely new names are reported.
  Watches `go.mod`, `package.json`, `composer.json`, `requirements.txt`,
  `pyproject.toml`, `Cargo.toml`, `Gemfile`, `pom.xml` and `build.gradle(.kts)`.

  Both gates warn by default, escalate to exit 7 with `=block`, disappear with
  `=off`, and take an audited one-shot bypass (`GG_ALLOW_STUB`, `GG_ALLOW_DEP`)
  that is logged through `gg record`.

### Changed

- **The two baseline lines now name their gates.** The contract already did this
  for the 500-line file-size rule, and the reasoning holds here: a rule that
  lives only in prose can be missed, while a gate cannot. Contract version moves
  again, so projects need one more `gg system sync`.

  Worth recording how the stub gate was verified, because it caught its own
  author: run against this very diff, it flagged the registration comment in
  `cmd/doctor_install.go`, which spelled the marker words out literally in a
  `.go` file. That is the gate working correctly, not a bug — so the comment was
  reworded rather than an ignore-pragma invented for it. The marker list lives
  in the hook and in `docs/hook-env-vars.md`, which the gate does not scan.

## [2.11.0] - 2026-08-03

The agent contract stops being only about what to remember and starts carrying
the engineering baseline every agent was assumed to already have.

### Added

- **`ENGINEERING BASELINE` in the managed agent contract.** The contract told
  agents how to *remember* and never how to *build*, so the quality floor of
  every project was whatever each model happened to bring with it. Nine lines
  now travel with the memory contract into every registered project's
  `CLAUDE.md`, `AGENTS.md`, `GEMINI.md`, `.cursor/rules/gg-mandatory.mdc`,
  `.openhands/microagents/gg.md` and `.gsd/KNOWLEDGE.md`: ceremony scaled to
  blast radius, reuse before writing, scope-may-be-cut but quality-may-not,
  stay inside your diff, fail loudly, an explicit tie-break order, no invented
  APIs, report what actually happened, and decide rather than defer.

  The tie-break order is deliberate: correctness → security → reliability →
  simplicity → maintainability → consistency → performance. The widely-copied
  form of this list ranks security *below* simplicity and consistency, which
  contradicts its own "never weaken security for convenience" rule the moment a
  simpler-but-weaker implementation shows up. Security sits second here so the
  ordering and the rule cannot disagree. For the same reason "always improve the
  codebase" is bounded to the diff you are already changing — unbounded, it
  licenses an unrequested refactor riding along in someone else's bugfix — and
  "ask when requirements are unclear" carries a threshold, because a rule
  without one stalls an autonomous worker on every ambiguity.

  Two of the nine are mechanically checkable rather than aspirational: "ship no
  TODO stubs" and "a new dependency needs a recorded reason". TASK-536 tracks
  binding those to `pre-task-done` gates. The other seven are judgment calls no
  gate can score — they depend on the model reading them, which is exactly why
  they belong in the contract and not in a hook.

  Existing projects report the block as `STALE` and repair with
  `gg doctor --check-contract --fix`; `gg system sync --contract-only` reaches
  every registered project in one pass. Note that the contract body is embedded
  in the binary: a host still running an older `gg` sees the new block as
  `EXTENDED` and will overwrite it (backing the file up under `.gg/backups/`
  first), so update the binary before syncing.

## [2.10.0] - 2026-07-31

The follow-through on 2.9.0: the paths that unification missed, and the two
places where a mechanism could not see its own drift.

### Fixed

- **`gg bug fix --from` and `gg bug wontfix --from` were dead flags (BUG-106
  follow-up).** `store.FixBug`/`WontFixBug` took no author, so `updateBugStatus`
  passed `""` into the mutation path — which inherits the existing record's
  author when none is supplied. Every fix was therefore attributed to the bug's
  **reporter**. That is worse than a missing field: a flag that promises
  provenance and returns someone else's name is a false answer, not an absent
  one, in the one lifecycle whose job is recording who is accountable for a fix.

  The author now resolves through the full `--from` → `$GG_ROLE` → agent
  identity ladder (so `GG_REQUIRE_AUTHOR` applies too) and is stored as a
  distinct `fixed_by`, so reporter and fixer never collapse. Bugs closed before
  this change render `Fixed by: [anonymous]` rather than silently showing the
  reporter — the accurate history. `StartFixingBug` deliberately keeps no
  author: "fixing" is a state change, not a closure claim.

- **`docs/cli` kept orphaned pages for deprecated commands (BUG-108).** cobra's
  `GenMarkdownTree` skips any command whose `Deprecated` field is set, but the
  page generated *before* the deprecation stays on disk and is never rewritten,
  so its flag documentation freezes. CI could not catch it either: the drift
  check runs the generator and then `git diff --exit-code docs/cli/`, and a file
  nobody rewrites produces no diff — the rot was invisible to exactly the
  mechanism meant to prevent it. `tools/docs-gen` now deletes any page that does
  not correspond to a command it just emitted, which is what gives that check
  something to see.

- **`gg inbox --group-by sender` no longer fragments across tabs.** 2.9.0 gave
  messages the per-tab identity, which is the accurate provenance, but the
  grouping keyed on it verbatim so three tabs of one runtime produced three
  one-message groups. Fixed in the **display**: `identity.CoarsenAgent` reverses
  the sharpening for aggregation only, and its prefix check is exact rather than
  "strip the last dash segment", so a legitimate role like `backend-dev` is
  never mangled. Each message still shows its exact sender. Storing the coarse
  form instead would have re-introduced the BUG-084 collapse that silently
  defeated task leases, per-recipient read state and verifier separation.

### Added

- **`fixed_by` on bugs**, rendered by `gg bug get` for fixed and wontfix bugs.

### Internal

- `internal/graph/crud.go` was 504 lines — over this project's own 500-line rule,
  in the repo that ships the rule, and not grandfathered. Split at the type
  boundary already present in the file: node operations stay in `crud.go` (345
  lines), the `Edge` type and its operations move to `crud_edge.go` (171). Pure
  code motion. The 14 files now visible in the 2.9.0 warning band are
  deliberately left alone — that band's policy is "split on the next touch", and
  refactoring compliant files en masse trades real regression risk for nothing.

## [2.9.0] - 2026-07-31

### Upgrade note

- **Run `gg doctor --refresh-hooks` in each project after updating.** The
  `30-file-size.sh` template changed, so every project with it deployed will
  report hook-template **drift** — and drift is a `fail`, which makes
  `gg doctor` exit non-zero until the hook is refreshed. `gg system sync`
  detects the drift and prints the reminder but does **not** auto-refresh
  `pre-task-done.d` hooks (it only refreshes agent, task and index hooks), so
  this one step is manual per project.

### Fixed

- **Durable writes no longer land anonymous when `GG_ROLE` is unset (BUG-106).**
  `resolveAuthor` resolved only `--from` then `$GG_ROLE`, so any runtime that
  never exported a role wrote `author=""` — silently, and indistinguishably from
  a record whose author simply was not printed by that view. The sharp edge is
  that gg had **already accepted** that session's identity: `requireAgentIdentity()`
  refuses any state-changing write unless `GG_ROLE` or `GG_AGENT` is set, "so
  shared evidence and handoffs are attributable". gg verified the identity at the
  door and then stamped the record from a different variable. 436 records in this
  repo's own ledger were anonymous *despite passing that check* — decisions
  160/643, tasks 221/516, bugs 32/107, rejections 23/38.

  Every durable stamp now resolves through one ladder:
  `--from` → `$GG_ROLE` → the runtime's agent identity (`GG_AGENT`, sharpened
  per tab by BUG-084) → `""`. The role stays ahead of the agent id deliberately:
  an exported `GG_ROLE` is the provenance the operator *means*, while the agent
  id is only the runtime that happened to execute the command.

  That covers five paths that each had their own ladder — `gg record` /
  `decide` / `reject` / `bug report` / `task create` / `task cancel` /
  `canon set`, plus `gg tell` (which fell through to the literal `"user"`,
  signing agent traffic as the human), the auto-written bypass-rationale record,
  the task-lifecycle broadcast (which said plain `claude-code` while the same
  command stamped the task owner `claude-code-<sid>`), `task ready-for-live`,
  and `task review` (which invented the literal `"reviewer"`).

  Existing anonymous records are **not** backfilled — they now render visibly
  anonymous, which is the accurate history.

- **`gg audit file-size --over N` is a real threshold (BUG-107).** It was
  documented as "a custom threshold instead of the per-type defaults" but
  implemented as a filter over the already-computed >500 violation set, so it
  could only ever *narrow* that list: `--over 100` and `--over 440` returned
  byte-identical output, and no file below its limit could be reported at any
  value of N. It now replaces the per-type limit, and ignores the grandfather
  list — filtering a raw size query through a list that exists to excuse rule
  violations is the same silent omission.

### Added

- **A warning band at 90% of the file-size limit** (450 source / 720 test), in
  both `gg audit file-size` and the `30-file-size.sh` pre-task-done hook. The
  rule previously fired only strictly *above* the limit, so a file could sit at
  499/500 producing no signal at all and the next two-line edit flipped it
  straight to a violation — a limit with no approach warning reports "compliant"
  right up to the wall. The band is signal only: it prints in `warn` and `block`
  mode alike, never touches the exit code, and in the hook covers only files the
  current task changed. The baseline does not suppress it — a grandfathered file
  is exempt from failing, not from being visible. This repo was sitting on 14
  in-band files with nothing reporting them, including one test file seven lines
  from a hard block.

- **`gg doctor` reports a lint gate that is installed but not armed.**
  `60-lint-gate.sh` exits 0 with "no baseline — skipping gate" when
  `.gg/lint-baseline.json` is absent, which is correct runtime behaviour but
  makes a deployed gate indistinguishable from an armed one. A fresh `gg init`
  lands in exactly that state, so it was the default condition of every new
  project. Four states are now reported: armed with the baseline count,
  installed but unarmed, baseline present but unreadable, and `golangci-lint`
  missing from PATH. Arm with `gg doctor --capture-lint-baseline`. Deliberately
  not symmetric with the file-size baseline, whose absence makes that gate
  *stricter*, not inert.

- **`GG_REQUIRE_AUTHOR=1`** — opt-in strict provenance for projects that adopted
  a written convention. An unattributable write fails instead of landing
  anonymously. Default off, so a human in a bare shell and CI are never blocked
  by a convention they did not adopt.

### Changed

- **An unresolvable author renders `[anonymous]` instead of being dropped.**
  Every renderer guarded on `Author != ""`, so an anonymous record looked
  identical to one whose author that view simply did not print. gg already
  prints an explicit marker for its other missing-provenance signal — absent
  evidence renders `[unverified]` — and author was the lone exception. Compact
  renderers are unchanged: they never show author at all, so they carry no
  present/absent asymmetry.

## [2.8.0] - 2026-07-24

### Added

- **`gg task unblock TASK-ID` — the non-destructive inverse of `gg task block`.**
  A blocked task previously had no CLI path back to active work: `start` rejects a
  blocked task, `release`/`renew` require `in_progress`, leaving only `done` (the
  verify gate) or `cancel` (which destroys the task). `unblock` returns a blocked
  task to `in_progress` under the caller with a fresh lease and clears the stored
  block reason, mirroring `start`'s ownership and lease semantics, and records an
  `unblocked` task event. `gg reconcile` folds the new event as a re-claim, so a
  `block` → `unblock` lifecycle no longer surfaces as false projection drift (and
  `gg reconcile --apply` can no longer revert an unblocked task back to blocked).

## [2.7.8] - 2026-07-20

### Fixed

- **`gg doctor --sync-artifacts` no longer claims every artifact is missing when
  only `installed.json` is absent.** The drift check decides from the manifest
  alone and never stats the filesystem, and the report path had no
  empty-manifest guard — so a project whose manifest was never written listed all
  17 artifacts as missing even with every file present. Beyond the false alarm,
  the report advised `--apply`, which would have rewritten those files including
  locally customised hooks on the strength of a claim never checked against disk.
  The report now says plainly that drift cannot be assessed and points at the
  directories to inspect; `--apply` still runs, since re-installing is how a
  missing manifest gets established. `doctorCheckArtifactDrift` already guarded
  this case, so the two paths now agree.

## [2.7.7] - 2026-07-20

### Internal

- Verification for the 2.7.0 trust tiers, which had shipped unchecked. The tier a
  decision renders with is time-dependent, and Go boundary tests were ruled out
  by the project's no-unit-tests policy — so the gap was written down as needing
  a policy exception. It did not: the clock never needed faking, the record dates
  did. `testdata/regression/trust-tiers.sh` writes decisions with chosen
  `created_at` values and asserts the rendered labels through the real
  `gg context` path, covering fresh, aging, stale, evidence-less and
  pinned-exempt. That exercises `trustTier`, the exemption rules and the renderer
  together — stronger than the unit test it replaces.

## [2.7.6] - 2026-07-20

### Performance

- **`gg search` no longer walks the code graph when it is provably fresh.**
  Emitting the stale-graph notice meant collecting graph status on EVERY
  interactive search — a filesystem walk, git subprocesses and a graph.db ping,
  bounded at ~3s — even when the graph was up to date and no notice could print.
  A cheap pre-check now compares the recorded index SHA against HEAD and skips
  the collection when they match. Measured 293ms → 201ms (~31%) on five warm runs.
  Deliberately conservative: missing or unreadable state, an empty-tree SHA, a git
  failure, or any genuine mismatch all fall through to the full walk, so the gate
  can only save time and never suppress a notice that was due — verified against
  fresh, corrupted-SHA and missing-state cases.

  This optimisation was written on 2026-06-17 but never reached main; it was
  recovered from a stale worktree during cleanup, one commit away from being
  deleted.

### Internal

- Lint debt cleared: the five standing findings are resolved (four scoped
  suppressions that each name the rule and why it does not apply, one genuine
  simplification) and the baseline is recaptured at 0, so a new finding now fails
  the gate instead of hiding under accepted debt.

## [2.7.5] - 2026-07-20

### Added

- **`gg search` now finds messages on the healthy path.** Messages were the one
  brain kind with no vector search at all, and TASK-516's lexical tier had left
  them out — so a handoff captured in a `gg tell` was findable when the vector
  store was DOWN (the JSONL fallback does scan messages) and invisible when it
  was healthy, the exact inverse of what anyone would expect. The command's help
  has always advertised "semantic search across decisions, tasks, and messages";
  it is now true. `--include-linked` and the last-known-good cache carry them too.
- **Degraded vectors now repair themselves on read.** 2.7.0 made partial coverage
  visible and pointed at `gg reembed`, but that remedy assumes a human — a
  headless agent saw the warning, had nobody to run the command, and the project
  stayed degraded indefinitely. Reads now re-embed a bounded batch of placeholder
  vectors AFTER serving their results, so a slow embedder can never delay an
  answer. Capped at 8 points and 5s per read and single-flighted on the existing
  reconcile lock, so coverage converges over several reads instead of stalling
  one, and parallel agents share the work rather than stampeding the embedder.
  It is not a reembed — only the broken points are touched. `GG_NO_HEAL=1` opts
  out.

## [2.7.4] - 2026-07-20

### Fixed

- **The AC-attestation gate's own bypass no longer makes a task impossible to
  close.** `50-ac-attestation.sh` documents `GG_ALLOW_INCOMPLETE_AC=<reason>` as
  its audited escape hatch, but the pre-task-done chain then runs the repro gate
  in the SAME environment, and the `TestACAttestation_*` helpers built their env
  from `os.Environ()`. The exported bypass therefore leaked into the hook under
  test, flipped it onto the bypass path and failed seven tests — so satisfying
  one gate guaranteed the next one refused. Both helpers now neutralise
  `GG_ALLOW_INCOMPLETE_AC` and `GG_AC_ATTESTATION` before applying their own
  overrides, so a test that genuinely wants the bypass still gets it.

### Internal

- Silenced a self-introduced gosec G703 on the config write-back's atomic temp
  write; the path is internally derived (`filepath.Join(ggDir, ConfigFile)`), so
  the taint warning does not apply. Lint is back to the 5-issue baseline.

## [2.7.3] - 2026-07-20

### Fixed

- **`gg record` no longer silently drops a decision when stdin is `/dev/null`.**
  `isTerminal()` tested only `ModeCharDevice`, and the null device IS a character
  device — so a headless agent was misdetected as interactive: on a near-duplicate
  gg printed the dedup prompt, read EOF as an empty answer, took the "cancel"
  default and discarded the record, all while exiting 0 so the caller believed it
  had been saved. Piping into stdin took the correct non-interactive path, so the
  same headless context produced two opposite outcomes — and the losing path is
  the common one, since most agent harnesses close stdin or wire it to
  `/dev/null`. An immediate EOF with no input now also creates rather than
  cancels: a duplicate is recoverable, a silently dropped record is not.
- **Task transitions now record who made them.** `UpdateTaskStatus` had no actor
  parameter, so the `TaskEvent` it appends left `Actor` empty. Claim events
  already carried theirs, so the audit trail named who STARTED a task but not who
  CLOSED it — the terminal transition `verifier_separation` exists to police.
  `gg task done` now records `--verifier` (falling back to the running agent) and
  `gg task block` records the acting agent.

## [2.7.2] - 2026-07-20

### Added

- **`gg doctor` now warns when commits on HEAD were never released.** Committing
  and pushing a fix is not the same as shipping it: `gg update` installs the
  newest RELEASE binary and syncs every registered project from it, so work
  sitting behind the latest tag reaches nobody — not other projects, not other
  machines, not even the maintainer's own shell — while looking completely done
  in git. The 2.7.1 cycle walked into exactly this: BUG-096 was fixed, committed
  and pushed while the installed binary still carried the bug, because no tag
  followed. The check is maintainer-facing (silent outside a local gg-cli source
  checkout) and advisory — unreleased work is a normal mid-development state, so
  it warns and never changes doctor's exit code.

## [2.7.1] - 2026-07-20

### Fixed

- **`gg reembed` now writes the migrated model and dimension back to
  `config.yaml`.** It probed the configured model, rebuilt every collection at
  its dimension and rewrote `embedding-meta.json` — but left `embedding.model`
  naming the OLD model, so a migration never actually finished: the config
  contradicted its own vectors permanently, and `GG_EMBED_MODEL` stayed
  mandatory in every shell. Measured across one host, 8 of 11 registered
  projects had drifted this way. 2.7.0 made recall survive the drift by
  resolving the query model from `embedding-meta.json`; this removes the drift at
  its source. The update is a targeted line rewrite rather than `Save()`, which
  round-trips through `yaml.Marshal` and would discard every comment in the file,
  and it refreshes the stale `<N>-dim` claim in the trailing comment. Only the
  `model:` key directly under `embedding:` is touched — `embedding.voyage.model`
  is a different setting and is left alone. Ollama-only and best-effort: a config
  that cannot be written never fails a migration that already succeeded.

### Internal

- `docs/cli/` regenerated for the 2.7.0 command surface (`gg backlinks`,
  `gg related`, `gg audit rot`, `gg graph export --view`). The docs-drift CI job
  had been red since 2026-07-02, when the `gg system sync` stage-5 line first
  drifted.

## [2.7.0] - 2026-07-20

The memory layer becomes recallable, linked and self-auditing. Until now gg had a
rich graph for CODE (REFERENCES/DEFINES/IMPORTS) and a flat list for MEMORY:
decisions could only be reached by semantic search, never walked, and a search
that came back empty was indistinguishable from a project with no history.

### Added

- **`gg backlinks <ref>` — reverse link traversal.** Answers "what links TO this
  task/bug/decision", which gg could never answer before. It recognises the
  relations gg already writes (`task_id`, `depends_on`, `blocks`) *and* prose
  refs: an Obsidian-style `[[wiki link]]` or a bare `TASK-042` / `BUG-084`
  mention in a decision's reason now creates a real, reversible edge with no
  extra flag. `--outgoing` shows the other direction, `--unlinked` surfaces
  entries whose prose names the anchor without linking it. Computed live from the
  folded JSONL, so it needs neither the embedder nor Memgraph and cannot drift
  from the ledger. On the gg-cli ledger itself this made existing history
  walkable with zero backfill (`gg backlinks BUG-064` → 4 inbound records, all
  found through prose mentions alone).
- **`gg related <ref> --hops N` — multi-hop walk over the link graph.** `gg
  context` and `gg impact` surface "related decisions" by vector similarity —
  things that *sound* alike. This walks what is actually *connected*, which is a
  different question: a decision can be the direct cause of a bug and share
  almost no vocabulary with it. Traversal is undirected with per-edge direction
  reported; references that resolve to nothing are kept as dangling rather than
  silently dropped.
- **`gg audit rot` — read-only sweep for ledger decay.** Reports three kinds of
  rot: `stale` (evidence old enough to re-check), `unproven` (pinned or
  policy-tagged decisions carrying no evidence at all — the entries every session
  inherits first, never verified), and `orphan` (active decisions with no link in
  either direction). Strictly a report: it never supersedes, retags or rewrites,
  and always exits 0.
- **`gg graph export --view code|memory|all`.** The offline visualization
  rendered only files and symbols — every memory node was filtered out, so the
  one picture gg shipped showed the code and none of the reasoning behind it.
  Memory nodes are derived from the JSONL link graph (not read back from
  Memgraph), so the view includes prose refs and renders for a project that has
  never been code-indexed. Default stays `code`; memory is opt-in.

### Changed

- **`gg search` now runs an always-on lexical tier over BRAIN RECORDS**, fused
  with the vector results and deduped by record id. The lexical JSONL scan
  already existed but only ever fired as a break-glass fallback when the store
  was down or the collection missing; on the healthy path the only lexical tier
  covered code symbols. That left a silent recall hole — a record that was never
  embedded (written while the embedder was unreachable), one carrying a degraded
  placeholder vector, or one below the semantic cutoff simply printed "No results
  found." with no error, which an agent reads as "never decided" and re-decides.
  Ranking is unchanged and stays vector-primary, and the status filters mirror
  the vector path exactly so superseded/rejected decisions and fixed/wontfix bugs
  still cannot leak into results (BUG-064).
- **Reads now warn when part of the brain is missing from the semantic index.**
  `gg search` and `gg context` print a one-line notice when the outbox has
  unembedded records or degraded placeholder vectors are being filtered out, with
  the `gg reembed` remedy. Partial recall is no longer indistinguishable from an
  empty project. Bounded and best-effort; silent when coverage is complete or
  under `GG_QUIET=1`.
- **Evidence now carries a verification AGE, not just presence.** A decision used
  to render either `Evidence: …` or `[unverified]`, treating a claim checked this
  morning and one checked eight months ago as equally solid. Decisions now read
  `[verified]`, `[verified · aging]` or `[verified · stale — reverify]`. The tier
  is derived, never stored, and affects only rendering and — as the last
  tie-break between two decisions — ranking. It never changes validity, never
  hides anything and never mutates status. Pins and
  `constraint`/`convention`/`policy`/`canon` tags bypass decay entirely: a
  recorded rule is not a measurement that expires.
- **CodeGraph git hooks now refresh the graph DETACHED (in the background)** so
  `git push` / `commit` / `merge` never block on indexing. The hooks already ran
  the incremental `gg index --changed` (the graph *write* only touches changed
  files + their 1-hop dependents), but the SCIP generation step
  (`scip-go`/`scip-typescript`) has **no per-file mode** — it re-analyzes the
  whole module every run — so the wait was ~constant regardless of diff size and
  felt like a full re-index. The hooks now launch `gg index --changed` as
  `nohup … >>.gg/index-hook.log 2>&1 </dev/null &` and return immediately; the
  refresh finishes in the background and logs to `.gg/index-hook.log`. `git`
  wall-time on the hook drops to ~0s (measured 4s → 0s against a slow-index
  stand-in). The hook still always `exit 0` and never aborts git.
- **`gg index` runs are now concurrency-guarded** by a per-project lock
  (`.gg/index.lock`). Because the detached hooks fire-and-forget, a quick
  commit→push can start a second `gg index` before the first finishes; the guard
  makes the later run **skip** instead of racing the graph DB (it also yields to a
  live `gg index --watch`). The skipped delta is picked up by the running index
  or the next git op.
- **`gg doctor --install-index-hooks` now UPGRADES an outdated gg-owned hook** in
  place instead of skipping it whenever the marker is present. Previously a new
  hook template never reached existing installs; re-running the installer (or
  `gg doctor --fix-index`) now rewrites a stale gg hook to the current
  (detached) form. Foreign hooks are still only appended to, never overwritten.
- **`gg system sync` now refreshes the CodeGraph git hooks** across every
  host-local project (new stage 5) so a hook-template change like the
  foreground→detached switch propagates without cd'ing into each repo. It is
  opt-in-preserving: the refresh runs **only where an index hook is already
  installed**, so a project set up with `init --no-index-hooks` is never
  force-installed into.

### Fixed

- **The query embedding model is now resolved from `embedding-meta.json`** — the
  model the corpus was actually built with — instead of a `config.yaml` that may
  have drifted from it. A drifted config previously made every brain command
  hard-fail with a model mismatch, and the only remedy was for a human to export
  `GG_EMBED_MODEL` in every shell, which made recall depend on operator memory.
  An explicit `GG_EMBED_MODEL` is still honoured as migration intent, so a
  genuine mismatch still trips `CheckMeta` and points at `gg reembed`.
- **`decisionFromJSONLEntry` dropped `Evidence`, `Pinned` and
  `RejectedAlternatives`** even though `AddDecision` writes all three to the
  payload. Any decision served from the ledger rather than the vector store
  therefore rendered as `[unverified]` and lost its pin — wrong on the offline
  fallback, and newly relevant now that the lexical tier builds live results
  through the same converter.
- **The stabilization freeze no longer fires on any task title containing the
  substring "meta".** The guard matched bare `meta`, so real product work was
  blocked for merely naming a file (`embedding-meta.json`). It now matches on
  intent (`meta-task` / `meta-work`); RE2 has no lookbehind and `\bmeta\b` does
  not help because a hyphen is itself a word boundary, so narrowing the token is
  the only formulation that separates the two cases. Everything the freeze
  targets — `AGENTS.md`, `CHANGELOG`, tracker-rule, hook, parity, enforcement and
  genuine meta-task titles — is still blocked.

## [2.6.0] - 2026-06-30

### Added

- **Symbol-level `REFERENCES` edges + `gg uses <name>`** — the indexer now
  persists a `(File)-[:REFERENCES]->(Symbol)` edge for every cross-file symbol
  reference SCIP already reports (previously collapsed into a file→file `IMPORTS`
  edge and discarded). This gives a **symbol-exact, barrel-proof reverse
  blast-radius**: `gg uses <name>` (and the read-only **`gg_uses`** MCP tool)
  answers "which files use symbol X" by matching the specific Symbol — so a
  barrel/re-export (`export * from './X'`) never over-reports consumers of a
  *sibling* symbol the way 2-hop file-level `gg impact` does. Reverse traversal
  rides the existing edge dst-index; `gg index` populates the edges (re-index
  required), and incremental `--changed` runs resolve targets outside the write
  set so cross-file edges to unchanged files are not dropped. REFERENCES is the
  semantic (SCIP) tier only, so an empty result on an unbuilt/syntactic graph is
  reported as a warning, not silent "no users".
- **`gg def <name>`** — resolve a symbol name to where it is defined (file + kind)
  straight from the code graph, offline. The grep-free answer to "where is X
  defined", and the static complement to the live `gg lsp def`. Also exposed as
  the read-only **`gg_def`** MCP tool, so MCP-connected agents get it in their
  tool list. Empty results are reported explicitly (run `gg index`), never as a
  silent "not found".

### Changed

- The synced **agent contract** now tells agents to get a shared symbol's blast
  radius from the code graph — `gg impact` / `gg lsp` / `gg def`, not grep —
  before editing it. The rule propagates to every CLAUDE.md / AGENTS.md /
  `.cursorrules` at the next session-start via the contract-version hash. Closes
  the affordance gap where the code-graph tools existed but agents reached for
  text search instead.
- **MCP `gg_impact`** no longer returns silently-empty dependents on a missing or
  unbuilt graph: it adds a `warnings` entry so an agent never reads "no
  dependents" as proof when the graph simply has not been indexed.
- **MCP tool descriptions** (`gg_impact`, `gg_search`) rewritten to position them
  against grep/ripgrep — `gg_impact` follows the real import graph and catches
  re-exports/barrels and aliased imports that text search misses; `gg_search`
  surfaces rejected approaches grep cannot.

## [2.5.1] - 2026-06-25

### Changed

- The durable-memory contract (injected into every project's `AGENTS.md` /
  `CLAUDE.md` / etc. and refreshed by `gg doctor --check-contract --fix`) now
  tells agents to **self-enable the commit-message gate** when the project has a
  recorded commit convention: if a decision or canon entry (e.g. tagged
  `convention`/`policy`) defines a commit format, the agent turns the gate on via
  `.gg/commit-msg.conf` instead of trusting memory — closing the loop between the
  surfaced rule (2.5.0) and mechanical enforcement. Run `gg system sync` to push
  the updated contract to existing projects.

### Internal

- Extracted the commit-time git-hook installation (secret-scan + commit-message
  gate + their `.git/hooks` dispatchers) into `installCommitGitHooks`, keeping
  `cmd/doctor_install.go` under the 500-line source limit. Behaviour-neutral.

## [2.5.0] - 2026-06-25

### Added

- Durable rules and policies now reliably survive in the session-start canon.
  `gg record --pin` is documented (in `--help` and the agent protocol) as the way
  to keep a rule — commit conventions, ownership rules, invariants — in the
  always-surfaced PROJECT CANON, and the auto-canon importance heuristic now also
  recognizes any tag containing `convention`, `policy`, or `workflow` (in addition
  to the existing `architecture`/`constraint`/`invariant`/`canon`/`security`),
  matched as a substring — so `commit-convention`, `naming-convention`, or
  `team-policy` all qualify without the recording agent guessing one exact magic
  word. Closes the gap that let a recorded commit convention fall off session-start
  and get missed in review. (TASK-513)
- New **commit-message convention gate** (`commit-msg` hook at
  `.gg/hooks/commit-msg.d/30-commit-msg.sh`). It checks the commit subject for
  length (default 72), file paths / source filenames, and an optional
  project-configurable prefix regex. It is **off by default** — opt in per project
  with `GG_COMMIT_MSG_GATE=warn|on` (or `.gg/commit-msg.conf`) — so propagating it
  across projects via `gg system sync` never surprise-blocks a commit. Implemented
  as a `commit-msg` hook (not `pre-commit`, which runs before the message exists);
  `gg init` / `gg doctor --install-task-hooks` now also wire a
  `.git/hooks/commit-msg` dispatcher. Knobs documented in `docs/hook-env-vars.md`.
  (TASK-514)

### Changed

- `gg status` / `gg session-start` no longer truncate **RECENT DECISIONS**
  silently. When more active decisions exist than are shown, the header reads
  `RECENT DECISIONS (5 of N shown)` and a trailing breadcrumb points to
  `gg decisions`, `gg search "<topic>"`, and `gg canon show` — so an older durable
  decision that has slipped past the top-5 window stays discoverable instead of
  vanishing without a trace. (TASK-513)

## [2.4.1] - 2026-06-23

### Fixed

- Self-update now resolves release assets using goreleaser's **lowercase** OS in
  the archive name (`gg_<version>_darwin_arm64.tar.gz`), not a title-cased
  `Darwin`/`Linux`/`Windows`. The 2.4.0 self-updater looked for the title-cased
  name and could not find any published asset, so `gg update` and the
  session-start auto-update failed with "no asset for this platform". (Follow-up
  to 2.4.0; verified end-to-end against the real GitHub release.)

## [2.4.0] - 2026-06-23

### Added

- `gg update` now self-updates by downloading the platform release binary from
  the GitHub release (and verifying its SHA-256 against `checksums.txt`), then
  atomically replacing the running binary. This replaces the previous
  `go install @latest` path, which could not reach v2.x releases — the module
  path lacks a `/v2` suffix, so the Go proxy only ever resolved v1.x. A new
  `--force` flag overrides the guards; `gg update --from-source` (build from a
  local checkout) is unchanged. Source/dev builds (Go VCS pseudo-versions) are
  guarded against accidental clobbering by a release binary.
- Throttled auto-update on `gg session-start`: when a newer release exists, gg
  installs it (at most one check per 24h window) and prints a one-line notice.
  Opt out with `GG_NO_AUTO_UPDATE=1` or pin a version with `GG_PIN_VERSION`;
  source/dev builds and offline/rate-limited runs are skipped silently so
  session start never blocks or fails on the update check.

## [2.3.3] - 2026-06-23

### Fixed

- `gg audit inbox-obedience` now counts a per-recipient `read_by` read as
  acknowledgement, not only the legacy global read flag. A role recipient who
  consumes messages via a non-peek `gg inbox --role <role>` (which records them
  in `read_by`, not the global flag) previously showed as 0% compliant
  ("reviewer 0/15"); role-targeted handoffs now reflect real acknowledgement.
  The audit still excludes `to_role="all"` broadcasts. (BUG-091)
- Parent commands `gg task`, `gg bug`, and `gg telemetry` now exit non-zero with
  an error on a missing or unknown subcommand instead of silently printing help
  and exiting 0; `gg task show` / `gg bug show` alias to `get`. Help via
  `--help` / `gg help <cmd>` is unaffected. (BUG-093)
- `gg telemetry` summary no longer conflates user-invoked CLI command verbs with
  internal brain-kind access labels — they are counted and rendered separately.
  (BUG-092)
- Rewrote the obsolete Qdrant-era reconcile test
  (`TestReconcile_FromJSONL_RecoversMissingFromQdrant`) to exercise the embedded
  SQLite reconcile path with a valid UUID and `EnsureCollections`, removing
  dead-backend references. (BUG-090)

### Changed

- Backend-neutral user-facing wording: `gg system brain status` reports
  `embeddings=` instead of `ollama=` (accurate under the Voyage cloud backend;
  the JSON key `ollama` is unchanged for machine consumers). `AGENTS.md`
  corrected to state the embedded SQLite stores are the only backend — the
  former Qdrant/Memgraph server backends were removed. (TASK-496, TASK-497)

## [2.3.2] - 2026-06-22

### Fixed

- `gg index` and the `gg index --changed` git hooks installed by
  `gg doctor --install-index-hooks` now refresh the language(s) the project was
  actually indexed as — resolved from `index-state.json` — when `--lang` is not
  passed, instead of defaulting to `go`. Previously the language-agnostic
  auto-refresh hook silently failed with "no go modules found" on every non-go
  project (TypeScript/Vue/Swift/Python), so their CodeGraph never auto-updated on
  commit/push/merge. Explicit `--lang` still wins; a never-indexed project still
  defaults to `go`. (BUG-095)

## [2.3.1] - 2026-06-22

### Fixed

- `gg import` and `gg brain import` now size vector collections to the imported
  data's real embedding dimension — from the bundle's own vectors (`gg import`)
  or the project's `embedding-meta.json` (`gg brain import`) — instead of a
  hardcoded 768. Importing a non-768 bundle/brain (e.g. `qwen3-embedding:0.6b`
  = 1024) no longer builds 768-sized collections that silently break recall.
- `gg brain import` now stamps `embedding-meta.json` to the model/dimension the
  collections were sized to, so the post-import re-embed (and any later
  `gg reembed`) resolves the same dimension instead of splitting between a probe
  and the metadata — which previously hard-failed re-embed on non-768 models.

## [2.3.0] - 2026-06-21

Configurable local embedding model. gg's Ollama backend can now point at any
embedding model — not just the 768-dim default — and you can switch it across
every project with one environment variable. Changing models is a `gg reembed` away.

### Added

- `GG_EMBED_MODEL` environment variable overrides the Ollama embedding model for
  the current process (read at config load, never written to `config.yaml`). A
  single `export GG_EMBED_MODEL=qwen3-embedding:0.6b` switches the model for every
  gg project at once. Ignored under the Voyage backend.
- `gg reembed` now retries transient embedder failures and skips (with a warning)
  any single record the model persistently rejects, so one Ollama model-runner
  hiccup on a large input no longer aborts an entire migration. It still fails fast
  when many records fail consecutively (model genuinely unavailable). Skipped
  records stay in the JSONL source of truth and are retried on the next `gg reembed`.

### Fixed

- The Ollama embedding dimension is now resolved authoritatively from
  `embedding-meta.json` (probed once on first run for a fresh project) instead of
  being hardcoded to 768 on the everyday search / record / MCP / init paths.
  Switching to a non-768 model (e.g. `qwen3-embedding:0.6b` = 1024) no longer trips
  a false "embedding model mismatch — run gg reembed" loop after reembedding.
  `gg doctor` now checks the live vector size against the resolved dim, not 768.

### Notes

- The shipped default stays `nomic-embed-text` (768-dim) — no forced model pull on
  existing installs. `store.VectorSize` (768) remains only the Ollama first-run
  fallback. The persisted `qdrant_id` node-identity key is unchanged.

## [2.2.1] - 2026-06-19

Patch: backend-neutral wording follow-up. Qdrant/Memgraph are fully removed
(embedded SQLite vector + graph since 2.x); this neutralizes the last
user-facing "qdrant" string. Internal Go symbol names and the persisted
`qdrant_id` node-identity key are intentionally retained — renaming the latter
would be a graph-store migration, so it stays for forward storage compatibility.

### Changed

- Neutralize the last user-facing "qdrant" wording: `DeleteTaskNode`'s empty-id
  guard now returns `task id is required` (was `taskQdrantID is required`). No
  behavior, schema, or storage-format change.

## [2.2.0] - 2026-06-17

Distribution + code-intelligence release: gg's brain is now reachable over MCP by
any agent, gains live LSP code navigation, hybrid lexical search, and a hardened
multi-agent story. No migration from 2.x (same embedded SQLite backend).

### Added

- **`gg mcp serve`** — read-only MCP server over **stdio** (no port, no daemon) exposing
  `gg_search`/`gg_context`/`gg_impact`/`gg_canon`/`gg_task_get`/`gg_bug_get`. Any MCP client
  (Claude Desktop, Cursor, Zed, …) auto-inherits the project brain; the project is resolved
  from CWD so brains never mix, and **no write tool is ever exposed**.
- **`gg lsp refs|defn|hover <file> <line> <col>`** — live, type-aware code intelligence via a
  per-invocation language server (gopls for Go; extensible per language). Exact references,
  definitions, and hover with zero index staleness.
- **`gg decisions [query]`** — direct decisions view/search.
- **`gg canon suggest` / `gg canon apply`** — structured, no-LLM canon consolidation (the agent
  fills an add/edit/delete op contract; gg validates atomically before applying).
- **`gg canon show --compact`** + a live decision↔task drift badge so canon never outruns the ledger.
- **Hybrid BM25/FTS5 symbol search** in `gg search` — exact-identifier matches alongside semantic.
- **Mandatory agent onboarding** — AGENTS.md orientation is now mandatory; new **Gemini** (`GEMINI.md`)
  and **OpenHands** (`.openhands/microagents/gg.md`) installers so every runtime gets a gg-managed
  "read the brain first" instruction.
- **`gg graph status`** — typed code-graph readiness line; stale-graph notice in `gg search`/`gg impact`.

### Changed

- `gg context` now ranks bundle entries by task-relevance (overlap + priority/type/recency,
  critical/architecture force-injected).
- `gg telemetry summary` bars normalized to max + leaf-verb section; bare `gg telemetry` defaults
  to `summary`; `gg task <unknown>` errors with a suggestion; `gg status` shows a labeled unread breakdown.
- `gg index --watch` hardened — single-flight mutex, per-file debounce, watchdog, circuit breaker
  (still foreground, no daemon).

### Fixed

- Multi-agent SQLite write contention — SQLITE_BUSY retry/backoff on store + graph write paths.
- Inbox unread-count inconsistency + unbounded agent-broadcast pileup (BUG-091); `gg inbox archive`
  timeout at 500+ messages (BUG-094); reconcile-from-JSONL test rewritten for the embedded store (BUG-090).
- MCP: JSONL fallback so tools never false-empty on un-reembedded projects; real read errors surfaced;
  panic-recovery (request loop + tool goroutines); oversize-line handled gracefully; `gg_context` parity.
- `gg decisions` offline path returns decisions only; canon drift marker anchored to exact IDs;
  inbox role-hint respects `--include-agents`/time filters.

## [2.1.0] - 2026-06-17

Polish + bug-fix release on top of 2.0.0: completes the backend-neutral cleanup,
adds self-consistency surfaces, and fixes the inbox firehose + audit findings.

### Added

- `gg decisions [query]` — direct decisions view/search (previously reachable only
  via `gg search` / `gg context`; the verb was surfaced in telemetry but not runnable).
- `gg canon show --compact` flag (consistent with `gg context`/`gg search`).
- Live decision↔task drift badge in `gg canon show` (⚡ CONFLICT / ⚠ live: pending),
  reusing the `gg context` conflict detector so canon can no longer tell a
  fresher-but-wronger story than the ledger.
- Routine firehose drain: `gg session-start` bounded auto-archive of `audience=agents`
  broadcasts older than 30d (failure-tolerant; JSONL preserved).

### Changed

- `gg telemetry summary`: bars normalized to the max count; leaf/subcommand verbs
  separated from top-level commands; bare `gg telemetry` now defaults to `summary`.
- `gg status` MESSAGES: labeled unread breakdown ("N project-wide (+M agent-broadcasts)")
  instead of one ambiguous count.
- `gg task <unknown-subcommand>` errors with a suggestion; `gg task show` aliases to `get`.
- Backend-neutral wording completed: the `gg init` config template no longer writes dead
  `qdrant:`/`memgraph:` blocks (eliminates the per-command "unrecognized key(s)" warning);
  `--json` contract keys, hints, internal identifiers, help text, and 14 prose/CLI docs
  de-branded to the embedded vector store / code graph.

### Fixed

- Inbox unread-count inconsistency across `status`/`inbox`/`--include-agents` and the
  unbounded `audience=agents` broadcast pileup (BUG-091).
- `gg inbox archive` blew the 10s command deadline at 500+ broadcasts (BUG-094) —
  the archive mutation is now batched.
- `gg decisions` offline (store-down) path returned all record kinds instead of
  decisions only (BUG-092c).
- `gg canon show` drift marker substring-matched prefix IDs ("TASK-49" vs "TASK-499") —
  now anchored to exact TASK/BUG tokens.
- Inbox role blind-spot hint mis-fired under `--include-agents` and time filters
  (`--since`/`--older-than`) (TASK-500).
- Reconcile-from-JSONL test referenced the removed Qdrant path (BUG-090) — rewritten
  to recover from the embedded SQLite store.

## [2.0.0] - 2026-06-17

### Changed

- **Single embedded backend — server backends removed entirely.** The opt-in
  Qdrant/Memgraph server backends (kept as selectable in 1.1.0) are gone. gg now
  has exactly one storage backend: the embedded, CGO-free SQLite vector store
  (`.gg/vectorstore.db`) and code graph (`.gg/graph.db`). Each project's brain is
  fully self-contained and Docker-independent (Ollama remains the only optional
  external service, native or in its own container).
  - Removed: the `qdrant:` / `memgraph:` config sections, the `qdrant.backend` /
    `memgraph.backend` fields, the `GG_VECTOR_BACKEND` / `GG_GRAPH_BACKEND` env
    overrides, backend resolution, and the `MEMGRAPH_*` credential env vars. Old
    configs that still carry these keys load fine — the keys are ignored with a
    one-time warning (removed-key stability contract).
  - Removed: the Qdrant gRPC dial path (`vectorstore_qdrant.go`), the Memgraph
    neo4j/Bolt driver path (`graphstore_neo4j.go`), the Docker compose bring-up
    in `gg init` (`startSharedServices`/`pullOllamaModel`/`init_docker.go`), and
    all server health checks in `gg doctor`.
  - **Dependencies dropped:** `github.com/neo4j/neo4j-go-driver/v5`,
    `github.com/qdrant/go-client`, and `google.golang.org/grpc` are no longer
    module dependencies. The vector store's data types (payload values, points,
    filters, requests) are now native Go structs in `internal/store`; the Qdrant
    protobuf types they replaced are gone. `google.golang.org/protobuf` remains
    only as an indirect dependency of the SCIP indexer.
  - **Vector-store payload format changed** from protobuf (`qdrant.Struct`) to
    JSON. The on-disk vector index (`.gg/vectorstore.db`) is a derived artifact
    rebuilt from the JSONL brain — **existing projects must run `gg reembed` once**
    after upgrading (the JSONL source of truth in `.gg/brain/*.jsonl` is never
    touched, so no data is lost). Integer payload fields round-trip losslessly
    (whole-number JSON values are coerced back to int64).
- **Backend-neutral wording (completes TASK-497).** `gg index`/`wave`/`export`/
  `brain reindex-decisions`/`reembed`/`check`/`doctor`/`index --watch` help text,
  error messages, and the down-store/offline hints no longer name Qdrant/Memgraph
  or tell users to `docker compose up`; they describe the embedded vector store /
  code graph. `gg reembed`'s false "stored knowledge will be lost" warning is
  corrected (`.gg/brain/*.jsonl` is the never-dropped source of truth). JSON
  `source_backend` reports `sqlite` on the embedded read path.
- **Backend-neutral `--json` keys.** `gg index status --json` renames
  `memgraph_configured` / `memgraph_available` / `memgraph_detail` →
  `graph_configured` / `graph_available` / `graph_detail`; `gg system brain-status
  --json` renames `qdrant` → `vector` and `memgraph_available` → `graph_available`.
  Scripts parsing these fields must update.

### Fixed

- **`gg init` no longer writes a config it immediately rejects.** The generated
  `.gg/config.yaml` template still emitted top-level `qdrant:` / `memgraph:`
  sections after those keys were removed from the schema, so every command on a
  freshly-`init`'d project printed `unrecognized key(s): memgraph, qdrant`. The
  template now carries only recognized keys.

- **`gg context <topic>` / `gg context` / `gg context --for-task` now fall back to
  the JSONL brain** when the embedded collections are not materialized yet (fresh
  clone before `gg reembed`), instead of hard-erroring with a raw not-found —
  matching `gg search`'s existing graceful degradation (audit VEC-1).
- **`gg doctor` no longer reports a freshly-`init`'d project as broken.** A
  never-indexed code graph (no `index-state.json`) is an advisory warning with the
  `gg index` hint, not a hard failure; genuine stale/corrupt graphs still fail
  (audit INFRA-4).
- **Search ranking uses semantic score as a tiebreaker.** When results share a
  lexical score, the more semantically relevant item now ranks higher instead of
  being ordered purely by record-kind precedence (audit VEC-3).
- **`gg doctor --fix-index` / code-graph freshness no longer depend on a
  placeholder server URI.** Graph availability is unconditional for the embedded
  store; blanking the (now-removed) `memgraph.uri` can no longer disable graph
  reads/writes across `record`/`task`/`bug`/`impact`/`doctor` (audit INFRA-1/2/3/5).
- **`gg init` seeds collections at the embedding backend's real dimension**
  (`embedding.EffectiveDim`) instead of a hardcoded 768 (audit EMB-1).

## [1.1.0] - 2026-06-14

Docker is no longer required. The two heavy database containers (Qdrant,
Memgraph) are replaced by embedded, CGO-free SQLite stores that are the default;
the server backends remain fully selectable as opt-in. Additive and backward
compatible — existing projects keep working and can migrate with one
`gg reembed` (vectors) + `gg index` (graph).

### Added

- **Embedded SQLite vector store** (TASK-493) — pure-Go brute-force cosine index
  (`.gg/vectorstore.db`) replaces the Qdrant container as the default. Exact
  (not approximate) ranking; single-digit-ms at project scale.
- **Embedded SQLite graph store** (TASK-494) — pure-Go graph (`.gg/graph.db`)
  replaces the Memgraph container as the default for `gg impact`.
- **Pluggable embedding backend** (TASK-495) — Ollama remains the default
  (byte-identical); **Voyage** cloud (`voyage-3.5-lite`) is an opt-in backend via
  `embedding.backend: voyage` + `VOYAGE_API_KEY`. Embeddings can now run with no
  Docker (native Ollama) or no local compute (Voyage).
- **No-Docker by default** (TASK-496) — `gg init` no longer requires Docker;
  `provisionInfra` only brings up compose services when a server backend is
  explicitly selected. `gg doctor` is backend-aware (embedded health checks,
  empty-store reembed/index hints, no false `ollama=down` under Voyage).
- **Standalone SQLite vector-store tests** — `cosineSimilarity`, the float32
  codec, brute-force ranking + top-K + score_threshold, must/must_not/should
  payload filters, write path and on-disk persistence now have unit coverage
  independent of the Qdrant integration oracle (17% → 57%).

### Changed

- Backend selection precedence (both stores): `GG_VECTOR_BACKEND` /
  `GG_GRAPH_BACKEND` env > `qdrant.backend` / `memgraph.backend` config field >
  `sqlite` built-in default.
- `docker-compose.yaml` template drops the Qdrant and Memgraph services; Ollama
  remains as an explicitly **optional** service.
- Service-down hints: the Ollama hint now lists Docker, native, and Voyage
  recovery paths; `gg bug` help no longer hard-references Qdrant.

### Fixed

- Integration tests pin the server backend (`GG_GRAPH_BACKEND=memgraph` /
  `Backend: qdrant`) so the sqlite default-flip can't silently divert or
  hard-fail them (RULES.md Rule 7).

## [1.0.1] - 2026-06-11

Post-1.0 hardening. No breaking changes; all additive within the 1.0 stability
contract.

### Added

- **Live CodeGraph** (TASK-502) — `gg init` now auto-installs the index git
  hooks, and the set gains a **post-commit** hook so the CodeGraph refreshes on
  every local commit (not just push/merge). No manual `gg index --watch` needed.
  Foreground/non-blocking — never delays or fails a commit; opt out with
  `--no-index-hooks` / `GG_NO_INDEX_HOOKS` / `auto_index_hooks: false`.
- **`--json` for `gg status` and `gg doctor`** (TASK-503) — machine-readable
  output for agents, plus discoverability cues (linked projects, pending outbox,
  `GG_TRACE` tip) surfaced in the human output.
- **Actionable service-down hints** (TASK-497) — when Qdrant/Memgraph/Ollama are
  unreachable, errors now include the exact `docker compose … up`/`logs` recovery
  commands.
- **Registry hygiene** (TASK-504) — `gg system register --list`/`sync`/`doctor`
  surface stale (missing-root) and invalid entries with a prune hint; duplicate
  `project_id` at a different root now warns.
- **Auto-reconcile outbox on session-start** (TASK-505) — pending vector writes
  replay automatically (bounded 3s, non-fatal); opt out with
  `GG_NO_AUTO_RECONCILE`. No more manual `gg doctor --reconcile` after a crash.
- **Init friction reducers** (TASK-501) — early Docker-daemon check, embedding
  model readiness in `gg doctor`, and an unsupported-language notice.
- **`gg doctor --strict`** (TASK-506) — exit non-zero on artifact drift for CI;
  default stays advisory.
- Store integration tests for client/export/dedup against real Qdrant (TASK-500).

### Changed

- **`gg gsd-guard` fails safe** (TASK-498) — unreadable/malformed PreToolUse
  stdin now blocks (exit 1) instead of silently failing open.
- **`--yes`/`GG_YES` bypass** added to `gg doctor --heal` and the test-tier
  install prompt (TASK-499); `gg reembed` gains a `--yes` alias for `--confirm`.
- Clarified help for `wave` (optional) and `metrics` (dogfood) (TASK-508);
  internal cleanups — `serve.go` split under the size gate (TASK-507), runner
  `MustRegister`, shared index-state warn helper (TASK-506).

## [1.0.0] - 2026-06-11

First stable release. The stability guarantees in
[docs/stability.md](docs/stability.md) are now **in effect**: the stable command
surface is frozen within a major version, storage is forward-only readable, and
breaking changes follow SemVer with a deprecation cycle. See
[docs/1.0-readiness.md](docs/1.0-readiness.md) for the readiness audit.

### Added

- **Stability & versioning policy** (TASK-492) — new `docs/stability.md` (SemVer
  mapping, command tiers, forward-only storage, deprecation policy, config
  additive-keys rule) and `docs/1.0-readiness.md` (command-surface + storage
  audit and the closed punch-list).
- **Shared deprecation mechanism** (TASK-493) — a single-source deprecation
  helper plus cobra `Deprecated` adoption on `gg decide`/`gg reject`, so
  deprecations warn once on stderr and are surfaced consistently in `--help`.
- **Config/registry schema versioning** (TASK-494) — `.gg/config.yaml` and
  `~/.gg/projects.json` carry a `schema_version`; unknown/removed config keys
  emit a one-time stderr warning instead of being silently dropped, and older
  files still load unchanged.
- **Experimental-tier markers** (TASK-496) — `audit`, `trace`, `metrics`,
  `telemetry`, `gsd`, and `reconcile` are now marked `[experimental]` in
  `--help` so users can tell them from the frozen stable surface.

### Changed

- **Honest compact-missed telemetry** (TASK-490) — `gg telemetry compact-missed`
  and the `gg status` Missed block now count agent-origin calls only; human
  full-reads are no longer counted as missed compact opportunities.
- **Honest hydration re-fetch metric** (TASK-491) — the re-fetch rate splits
  gate-mandated `--full` reads from discretionary re-fetches; the "drop-list
  agresif" warning now fires only on the discretionary agent rate.
- **Status: pre-1.0 → stable (1.0).** README and docs updated; the stability
  contract is now binding rather than aspirational.

### Fixed

- **docs/cli drift → zero** (TASK-495) — regenerated the generated CLI reference
  (added missing `gg onboard`/`gg serve` pages, refreshed stale pages); the CI
  docs-drift check is green.

## [0.6.0] - 2026-06-10

### Added

- **Launcher portfolio** (TASK-488) — the dashboard gains a **Projects** tab
  listing every registered gg project with quick health (open tasks / open bugs /
  decisions), loaded lazily per project via `/api/project-health`. Click a card
  to open that project. Navigation metadata only — brains stay isolated.

### Changed

- **Brain graph self-heals** (TASK-489) — when a new shell opens and the
  per-project relationship graph has drifted (decision nodes but no edges), gg
  now reconciles it automatically (task + decision reindex) instead of printing a
  "run `gg doctor --fix-index`" notice. Healthy projects stay silent; the
  reconcile is bounded and non-fatal. No manual upkeep.

## [0.5.0] - 2026-06-10

### Added

- **Global, path-independent dashboard** (TASK-487) — `gg serve` now runs from
  any directory and serves every gg project registered on the host
  (`~/.gg/projects.json`), with a header dropdown to switch between them. Each
  request carries `?project=<id>` and resolves a cached per-project store +
  embedder; brains stay fully isolated (no cross-project merge). Inside a project
  that project is the default; stale (deleted-root) entries are filtered out.

### Changed

- **Work board is read-only** (TASK-486) — removed dashboard drag-to-start.
  Agents self-claim and progress tasks (`gg next` / `gg task start`); a human
  dragging to start created a fake "dashboard" owner and conflicted with the
  autonomous-agent model. The clickable task-detail panel and the human compose
  writes (record decision / create task) are retained; dropped `@dnd-kit/core`.

## [0.4.10] - 2026-06-09

### Added

- **Brain-graph drift notice** (TASK-482) — session-start warns when the
  per-project Memgraph relationship graph is stale (decision nodes but no edges),
  pointing to `gg doctor --fix-index`; mirrors the code-graph notice.
- **Drag-and-drop Kanban** (TASK-483) — with `gg serve --write`, drag a Pending
  task onto In Progress to start it (`gg task start`, in-process). Gated
  transitions stay in the CLI.
- **Messages tab filter** (TASK-485) — client-side filter (from / to / content /
  audience / task), matching Decisions and Bugs.

### Verified

- **Dashboard is project-agnostic** (TASK-484) — `gg serve` + all dashboard APIs
  verified on a second project (rising-demo); data correctly project-scoped.

## [0.4.9] - 2026-06-09

### Added

- **Dashboard: About tab** (TASK-481) — a plain-language page explaining what gg
  is and does for anyone who opens the dashboard: the per-project brain, what it
  records (JSONL source of truth), how it recalls (Qdrant semantic + Memgraph
  graph), the auto-distilled canon, the "done = verified" gates, the 3-tier
  context economy, the no-network / no-daemon / single-binary architecture, key
  commands, and live project stats.

## [0.4.8] - 2026-06-08

### Added

- **Dashboard write actions** (TASK-477) — `gg serve --write` enables compose
  forms (record decision on Overview, create task on Work) that run gg
  in-process; read-only by default, POST + same-origin guarded.
- **`gg onboard`** (TASK-478) — prints the distilled newcomer briefing (canon +
  invariants + workflow) and its token cost; the "30-second senior-dev" proof.
- **Task detail panel + list filters** (TASK-479) — click a task in Work to see
  its full detail, dependencies and linked decisions; Decisions/Bugs tabs gain a
  client-side filter.

## [0.4.7] - 2026-06-08

### Added

- **Live dashboard via SSE** (TASK-476) — `/api/stream` emits change events; the
  dashboard auto-refreshes data tabs as agents write, with a live indicator.
- **Human-only commit identity rule** (TASK-480) — `gg doctor` checks the git
  commit identity, warns at session-start, and `gg doctor --fix-git-identity`
  resets a repo-local agent identity (e.g. `Hermes Agent <…@localhost>`) so
  commits are attributed to you, not an agent. Detector flags `agent|bot` /
  `@localhost` identities.

## [0.4.6] - 2026-06-08

### Fixed

- **BUG-089: module/hook discovery descended into `.claude/worktrees`.** Nested
  git worktrees there carry a `go.mod`, so discovery treated them as project
  submodules and installed stale per-worktree hooks
  (`10-go-verify-.claude-worktrees-agent-*.sh`) that regenerated after every
  delete/sync. Added `.claude` to `DefaultHookInstallSkipDirs` (covers hook
  install + code-graph discovery) and removed the leftover worktree.

## [0.4.5] - 2026-06-08

### Added

- **Dashboard: Messages tab + `/api/messages`** (TASK-474) — the recent
  agent-to-agent message stream (from→to, audience, task link, read state, time),
  newest-first. Surfaces how agents coordinate; previously invisible despite
  thousands of stored messages.

### Changed

- **`gg doctor --fix-index` now reconciles the brain graph too** (TASK-473) —
  it reindexes Decision/Task nodes + DECIDES/DEPENDS_ON edges before refreshing
  the code graph, so the standard repair entry point (and the index git-hooks
  that call it) keep Memgraph's brain relationships in sync — moving BUG-088's
  reconcile toward automatic.

## [0.4.4] - 2026-06-08

### Fixed

- **BUG-088: brain→Memgraph relationship edges not synced per-project.**
  `gg brain reindex-decisions` replayed Decision nodes but never rebuilt the
  DECIDES edges, so after any Memgraph rebuild the per-project relationship graph
  was effectively empty (gg-cli had 1 brain edge vs 278 links in the store). It
  now reconciles the DECIDES edge from each decision's structured `TaskID`. Run
  `gg task reindex` then `gg brain reindex-decisions` to heal — verified live on
  gg-cli: 1 → DECIDES 194 + DEPENDS_ON 72.

## [0.4.3] - 2026-06-08

Dashboard Phase 2 visual depth (TASK-472).

### Added

- **Context tab: command-mix donut** (Recharts) on top of the existing bars.
- **Graph: click a node** to open a side panel with the full record and every
  relationship it participates in (incoming + outgoing, with the connected
  record's title).
- **Motion**: tab transitions now fade/slide (framer-motion).

## [0.4.2] - 2026-06-08

### Changed

- **Graph tab now uses a dagre DAG layout** (left-to-right: decision → task →
  dependency) instead of naive columns, so the relationship web is readable even
  at hundreds of nodes.
- Overview "decisions" card relabeled **active decisions** to make clear it
  counts active records (vs the full ledger incl. superseded/rejected).

## [0.4.1] - 2026-06-08

### Added

- **Graph tab** (react-flow) — visualize the brain's relationship web:
  decision→task (DECIDES), task→task (DEPENDS_ON / BLOCKS), bug→task (AFFECTS).
  Built from the store (`Task.DependsOn/Blocks`, `Decision/Bug.TaskID`), not
  Memgraph — gg-cli's per-project brain edges aren't reliably synced to Memgraph,
  and the store always has the links. Only connected records are shown; the
  37k-symbol code graph is excluded to stay legible. New `/api/graph` endpoint.

## [0.4.0] - 2026-06-08

The dashboard becomes a real React SPA — without giving up the single-binary,
offline, no-daemon architecture.

### Changed

- **`gg serve` now serves a React + Vite + TypeScript + Tailwind dashboard**,
  replacing the hand-rolled vanilla page. The compiled bundle is embedded in the
  Go binary via `go:embed` (committed `dashboard/dist`), so end users still get a
  single binary with no Node runtime, fully offline, served foreground on
  127.0.0.1 only. Node is a build/dev dependency only.
  - Tabs: Overview (+ auto-canon), Live Search (embed→Qdrant pipeline with
    timings + score bars), Decisions, Work (Kanban), Bugs, Files (raw JSONL
    browser), Context (visual telemetry).
  - Dev DX: `cd dashboard && npm run dev` with `/api` proxied to a running
    `gg serve` (hot reload). Production: `npm run build` → embedded `dist`.
  - This unblocks rich visual libraries (react-flow graph, charts, drag-drop
    Kanban) for future iterations without an architecture change.

## [0.3.37] - 2026-06-08

Dashboard v1 complete (still the embedded vanilla build; the React SPA is next).

### Added

- **Kanban board** (`Work` tab) — tasks laid out in lifecycle columns
  (pending / in progress / ready for live / blocked / done).
- **Files tab** — browse the raw JSONL source-of-truth stores (name, record
  count, size) and view the most recent records of any store. Path-traversal
  guarded; new `/api/files` and `/api/file` endpoints.
- **Context & Activity tab** — the telemetry view is now visual: net/compact
  tokens saved, a per-command activity breakdown ("what gg is actually doing"),
  compaction savings by verb, session context-pressure, and the 3-tier context
  model (session-start / for-task / search) so the context economy is legible.

## [0.3.36] - 2026-06-08

A way to *see* the project brain — and how it works.

### Added

- **`gg serve` — local dashboard.** A FOREGROUND, localhost-only web UI for the
  project brain. Not a daemon: it binds 127.0.0.1 only, runs until Ctrl-C, and is
  read-only (consistent with the no-daemon / no-network architecture; same
  precedent as foreground `--watch`). Anyone who ran `gg init` opens it with
  `gg serve` (`--port`, `--no-open` flags). The dashboard is a single embedded
  page with no external assets (works fully offline). Views:
  - **Overview** — counts + the auto-derived canon + recent decisions.
  - **Live Search** — type a question and watch how gg answers it: the query is
    embedded into a 768-dim vector (Ollama), then Qdrant returns nearest records
    by cosine similarity, with embed/search timings and per-result score bars —
    the same path an agent's `gg search` takes.
  - **Decisions / Work / Bugs** — browse the memory (noise-filtered).
  - **Telemetry** — local context-economy and activity stats.

## [0.3.35] - 2026-06-08

Usability polish for the per-project brain — make the current form excellent to
use day to day (scope stays single-project; no cross-project/user brain).

### Changed

- **Leaner session-start canon.** The canon injected at session-start now uses a
  compact view (hard total cap on decisions, shorter lines) with a
  `gg canon show` pointer for full depth. Dropped the per-session briefing from
  ~13.7 KB to ~8.3 KB (~3.4K → ~2.1K tokens) while keeping the full institutional
  briefing. `gg canon show` is unchanged (full depth).
- **Release logs no longer pollute the canon.** Operational decisions
  ("Release vX shipped and synced…") are filtered as low-signal alongside
  bypass-rationale rows, so the canon and overview carry durable knowledge only.

## [0.3.34] - 2026-06-08

Canon goes fully automatic — institutional memory with zero manual upkeep. A new
agent now inherits the distilled senior-dev knowledge at session-start without
anyone ever running `gg canon set`.

### Added

- **Auto-derived canon (`BuildAutoCanon`).** `gg canon show` and session-start
  now compute the canon live from the ledger — no manual curation. Three
  sections, distilled deterministically (gg is no-network/no-LLM, so
  "summarization" = selection + dedup + ranking):
  - **key-decisions** — active decisions, deduplicated, with pinned and
    architecture/constraint-tagged ones always included regardless of age
    (important-old is never summarized away); the routine tail is capped.
  - **rejected-approaches** — what not to re-propose, deduplicated.
  - **failure-modes** — fixed-bug root causes (the lessons).
  Manual `gg canon set` still works and now renders as a "Curated" layer above
  the auto-derived digest.

### Changed

- **Noise no longer dominates a newcomer's first screen.** Low-signal
  bypass-rationale records and near-identical duplicate decisions are filtered
  out of both the canon and the `gg context` project overview
  (`FilterDecisionNoise`). In dogfooding, the overview's top went from four
  identical bypass-rationale rows to the real architectural decisions.

## [0.3.33] - 2026-06-08

Regression-gate repair found while closing the institutional-memory tasks
through the gates (no bypass) — the pre-task-done repro gate was silently broken
for every recent bug.

### Fixed

- **Regression gate ran `_test.go` repros as shell scripts.** `gg bug run-repros`
  executed every registered repro via `sh <path>`. The 19 newer bugs
  (BUG-062..086) register the locking `*_test.go` as their repro, so
  `sh foo_test.go` failed in ~3ms — the `90-bug-repros` pre-task-done hook
  reported 19 false failures and blocked every task close. A `*_test.go` repro
  now runs via `go test -run ^(Test…)$ <dir>` scoped to that file's tests; shell
  repros still run via `sh`. All 82 repros pass.

### Changed

- **Cleared lint debt (17 → 9 golangci issues).** Removed 3 dead functions
  (`impactGraphFreshnessWarnings`, `goInstallGG`, `hasCodeGraphSourceFiles`) and
  rewrote 5 gocritic if-else chains to `switch`/`else if` (impact, index_status,
  task_list, task_create). Pure refactor, no behavior change — restores a green
  `60-lint-gate`.

## [0.3.32] - 2026-06-07

Institutional-memory layer — gg moves from an append-only ledger toward a
self-distilling project memory a new agent can inherit.

### Added

- **`gg canon`** (TASK-468): the agent-distilled "what every dev must know" layer.
  `gg canon gather` dumps the raw material (active decisions, rejections,
  fixed-bug root causes); `gg canon set <area> "…"` stores durable per-area
  knowledge; `gg canon show` prints it; session-start injects it (like RULES) so
  every new agent starts with it. Stored at `.gg/canon.jsonl` (outside the
  export-managed `brain/` dir). Distillation is agent-driven (no-network: no
  cloud LLM).
- **`gg record --pin`** (TASK-469): pinned decisions surface first in the project
  overview regardless of age, so important-but-old decisions are never buried by
  recency. Rendered with 📌.
- **`gg inbox archive`** (TASK-470): retire stale `audience=agents` status
  broadcasts from the inbox (kept in JSONL, forward-only) so it stops bloating.
- **`gg doctor --install-index-hooks`** (TASK-471): opt-in pre-push + post-merge
  git hooks that run `gg index --changed` to keep the local CodeGraph fresh.
  Foreground + non-blocking, not a daemon.

### Changed

- session-start bypass audit collapsed to a one-line per-gate summary; project
  orientation no longer surfaces test/scrubber fixture notes.

## [0.3.31] - 2026-06-07

Found during a full end-to-end command QA sweep.

### Fixed

- **Task lifecycle was unsatisfiable for agents** (BUG-050 regression): under
  `GG_AGENT` the inbox/`task get` auto-compacts, and the BUG-074 fix stopped
  compact reads from recording a hydration proof — so `ready-for-live` / `done` /
  `block` always refused with "no recent full task detail read" and there was no
  in-flow override. Added `gg task get TASK-X --full`, which forces a full
  (non-compact) render even under agent auto-compact and records the hydration
  proof; the gate error messages now point at it. Bugs were unaffected
  (`gg bug triage` records hydration unconditionally).
- **Decision evidence was stored but never shown** (BUG-086, completes BUG-071):
  `gg record --evidence` persisted evidence but no renderer displayed it. The
  full context render now shows `Evidence: …`, and marks decisions with no
  evidence as `[unverified]` (evidence was already present in `--json`).

### Notes

- v0.3.30's critical search fix (BUG-085) is included here; its release publish
  was blocked by a transient GitHub 504.

## [0.3.30] - 2026-06-07

### Fixed

- **Critical: semantic search returned zero results** (BUG-085). The
  non-degraded-vector filter used Qdrant `is_null` on `gg_vector_degraded` as a
  MUST condition across every `Search*` query, but normal records never set that
  key and `is_null` matches only keys that exist and are explicitly null — so the
  filter excluded the entire brain and `gg search` / `gg context` returned
  nothing. Switched to `is_empty` (matches missing/null/empty), which keeps
  normal records and excludes only explicitly-degraded ones. Regression covered
  by TestSearchExcludesOnlyDegraded_Integration. Affected v0.3.27–v0.3.29.

## [0.3.29] - 2026-06-07

### Changed

- Document the v0.3.27 capabilities in the agent-facing docs so agents adopt them:
  `gg record --evidence` in the durable-memory contract and `AGENTS.md`, plus
  per-session identity auto-derivation and per-recipient inbox notes in the
  orientation guidance. Propagates to registered projects via `gg system sync`.

## [0.3.28] - 2026-06-07

Ships local fixes that were committed in parallel and merged with the v0.3.27
memory-integrity cluster.

### Fixed

- Active-status filter on `SearchDecisions`/`SearchBugs` with a status badge in
  renderers (BUG-064).
- Strict mode fails closed on non-executable hooks (BUG-065).
- Exclude degraded zero-vector records from all `Search*` queries (BUG-066).
- `gg record --rejects` supersedes the rejected decision in the store (BUG-068).
- Inbox gate preflight added to `gg task done` (BUG-072).
- Restored the permanent repro for BUG-061 (BUG-081).

## [0.3.27] - 2026-06-06

Memory-integrity bug cluster (17 bugs, PR #1) — restores the "one consistent
shared brain" guarantee: every agent reads the same durable, current memory.

### Fixed

- **Durable-memory mutations are JSONL-first with version/CAS** (BUG-062, BUG-063):
  decision/bug/message status updates were Qdrant-only with no concurrency guard,
  so they silently reverted on any Qdrant rebuild and concurrent writers clobbered
  each other. Mutations now append the full record to JSONL under an optimistic
  version guard (last-write-wins fold), then mirror to Qdrant best-effort.
- **reembed sources from JSONL** (BUG-069): no longer drops JSONL-only records or
  prefers stale Qdrant payloads.
- **reconcile** folds latest state into Qdrant, surfaces malformed JSONL lines
  (BUG-070), and holds a non-blocking lock so two reconcilers can't clobber a
  store (BUG-073).
- **Per-recipient inbox read-state** (BUG-082): one agent reading no longer marks
  a message read for everyone.
- **Claude inbox-first hook** actually injects now (BUG-083): grep matches the
  real (compact) header and only filters by role when `GG_ROLE` is set.
- **Per-session agent identity** (BUG-084): a generic `GG_AGENT=claude-code`
  under a Claude session derives a unique `claude-code-<session>` so concurrent
  tabs don't collapse ownership/verifier separation.
- **Identity-based verifier separation** (BUG-067): closure is refused when the
  closing runtime is the one that set ready-for-live (role strings were spoofable).
- **Embedding dimension guard** (BUG-078): refuse to persist `Dim:0` and self-heal
  a corrupt zero-dim meta instead of silently disabling the mismatch check.
- **Hook-level gate disable is audited** (BUG-079): `GG_AC_ATTESTATION=off` /
  `GG_REVIEW_CONVERGENCE=off` now require a rationale and write a searchable
  brain event.
- **Review-convergence trailer** must enumerate >=3 matrix categories, not a bare
  token (BUG-077).
- **Hydration gate** is satisfied only by a full (non-compact) read (BUG-074).
- **`gg context` offline** does a live JSONL scan before the stale LKG cache
  (BUG-075) and reports per-collection query failures in `--json` (BUG-076).
- Hardening (BUG-080): numeric ID export sort, atomic session-cursor write,
  documented lock-bypassing `projectstate.Write`, and a JSONL bootstrap for the
  discussion sequence.

### Added

- `gg record --evidence` and a Decision `Evidence` field; `Note` now records an
  author — provenance so an unverified claim and a proven one are not stored with
  identical weight (BUG-071).
- `internal/identity` package resolving the effective per-runtime agent identity.

## [0.3.26] - 2026-05-30

### Fixed

- Clarify `GG_AGENT` examples so GSD shells use a `gsd-*` runtime identity instead of copying a host-agent example literally.

## [0.3.25] - 2026-05-30

### Added

- Document native workflow capture points for BMAD, GSD2, OMO Slim, Antigravity, Codex, Claude Code, Cursor, and Aider.
- Add native-workflow memory-sync smoke coverage for decisions, rejections, and handoff retrieval.

### Changed

- Reframe agent protocol, templates, and gate wording around durable shared memory and evidence capture instead of gg-owned workflow orchestration.

### Fixed

- Remove retired orchestration wording from active docs and tests while preserving stale-wording absence coverage.

## [0.3.24] - 2026-05-28

### Added

- Add Swift CodeGraph recognition and externally-backed SCIP ingestion: `gg index --lang swift`, SwiftPM/Xcode/.swift freshness detection, explicit `scip-swift` preflight errors, and Swift setup documentation.

## [0.3.23] - 2026-05-26

### Added

- `gg context --compact` without a topic now emits a compact project-level onboarding bundle for fresh agent sessions.
- `gg task ready-for-live` can update the stored verifier plan on already-ready tasks, including via `--plan`.

### Changed

- CodeGraph freshness now explicitly excludes dependency lockfile-only churn such as `go.sum`, npm/yarn/pnpm lockfiles, and Python lockfiles.
- Reviewer packets and reconciliation output now surface clearer task lifecycle drift details.

### Fixed

- Impact attestation now checks the active task diff first and only falls back to HEAD trailers when HEAD references the exact task being closed.

## [0.3.22] - 2026-05-24

### Fixed

- `gg system sync` now self-heals missing per-project tracker collections before refreshing contracts/hooks, while preserving non-destructive stale registry reporting and cancellation semantics.
- `gg system brain status` now describes its role separately from the tracker self-heal performed by `gg system sync`.

## [0.3.21] - 2026-05-22

### Added

- Standardize CodeGraph freshness notices across session start, next-step, impact, doctor, and index status outputs.

### Fixed

- Make foreground CodeGraph watchers use per-language freshness state and dirty-tree fingerprints to avoid stale slices and repeated full-refresh loops.

## [0.3.20] - 2026-05-22

### Fixed

- Regenerate CLI docs with deterministic trailing newlines so the docs drift CI gate stays clean.
- Add focused task-claim helper coverage to keep `internal/store` above the required coverage threshold.

## [0.3.19] - 2026-05-22

### Added

- CodeGraph freshness notices now surface changed/new/deleted/module-file counts across `gg index status`, `gg doctor`, `gg impact`, `gg session-start`, and `gg next`.
- Added a time-to-productivity onboarding smoke script covering session start, next-step recommendation, search/context retrieval, impact, and ready-task listing.

### Fixed

- `gg doctor --fix-index` now repopulates empty or unavailable Memgraph projections with a full index instead of no-oping through `--changed`, and treats Qdrant downtime as advisory for CodeGraph repair.
- TypeScript/Python `src/` fallback discovery no longer overrides nested manifest-based module roots.
- Inbox-obedience auditing now keeps assignment bypass handling aligned with focused user-directed release work.

## [0.3.18] - 2026-05-19

### Fixed

- Clear the remaining `golangci-lint` debt surfaced by gg task quality hooks.

## [0.3.17] - 2026-05-19

### Fixed

- `gg update` now installs the concrete latest version selected by `gg update check`, and uses `GOPROXY=direct` when direct lookup beats a stale Go proxy result.

## [0.3.16] - 2026-05-19

### Documentation

- Trim README into a shorter public-facing overview and point detailed command/config material to docs.
- Clarify agent-status broadcasts should use `--audience agents` in runtime/template guidance so status noise stays out of human inboxes.
- Refresh deprecated decision-capture help text to point users at canonical `gg record` forms.

## [0.3.15] - 2026-05-19

### Added

- `gg index status` and `gg system brain status` now treat projects with no supported CodeGraph source as `not_applicable`, so non-code projects no longer block cross-project brain health.

### Fixed

- `gg doctor` now warns when the installed `gg` binary cannot be proven fresh because the source checkout has uncommitted build-affecting changes.
- `gg bug fix/start/wontfix/reopen/attach-repro` now require a recent full `gg bug get` or `gg bug triage` hydration proof in tagged agent sessions.
- `gg audit inbox-obedience` no longer treats `gg tell all` broadcasts as per-role acknowledgements unless a role is explicitly mentioned.
- TypeScript CodeGraph indexing now preserves other language graph slices, records per-language freshness, and can index nested package roots when a workspace root lacks a TypeScript config.

## [0.3.14] - 2026-05-18

### Added

- `gg system brain status` reports cross-project project ID, backend, brain snapshot drift/freshness, and CodeGraph health separately from `gg system sync` contract propagation.

### Fixed

- `gg task done` no longer panics when a valid compact-hydration proof returns a typed-nil gate result.

## [0.3.13] - 2026-05-18

### Fixed

- Release builds now cross-compile Windows binaries by moving project runtime state locking behind platform-specific files.

## [0.3.12] - 2026-05-18

### Fixed

- Release workflow now publishes GoReleaser-built binary archives and checksums to GitHub Releases instead of creating metadata-only releases.

## [0.3.11] - 2026-05-18

### Added

- `gg doctor` now checks code-graph freshness, degraded placeholder vectors, embedding vector validity, and a semantic canary before reporting the project brain as healthy.
- `gg index --changed`, `gg index status`, and `gg impact` now account for dirty tracked files and untracked source files via working-tree fingerprints, preventing stale impact answers after local edits.
- `gg task ready-for-live` and `gg task block` now share the compact-hydration gate used by `gg task done`, so tagged agent sessions must hydrate the full task before changing task state.

### Fixed

- `gg update` now verifies the installed `gg` binary version after update attempts, avoiding false "latest" reports caused by Go proxy or PATH skew.
- Offline JSONL search now renders tasks and bugs as native result kinds instead of coercing them into decisions.
- `gg doctor --reconcile` marks zero-vector Qdrant payload replays as degraded and tells users to run `gg reembed` to restore semantic recall.
- Runtime/config state writes now use cross-process locking on Unix to reduce concurrent agent clobbering.

## [0.3.10] - 2026-05-16

### Added

- `gg task done` now enforces a compact-hydration gate for tagged agent sessions: agents must run a recent full `gg task get TASK-ID` before closing the task, preventing compact list/search rows from being treated as source-of-truth.
- Full task hydration proofs are stored in project runtime state with locked updates so concurrent Hermes agents cannot clobber another session's proof, bypass audit entries, or session-start version state.

## [0.3.7] - 2026-05-15

### Fixed

- `gg status` now renders compact hydration risk even when compact calls have zero full re-fetches, so zero-hydration agent sessions show the source-of-truth warning instead of hiding the Hydration line.
- File-size scans now skip dependency/framework/runtime trees such as Hermes mounts and generated caches while preserving source paths like `internal/cache`, keeping `gg status` responsive in mounted agent workspaces.

## [0.3.6] - 2026-05-13

### Added

- Compact output now marks hidden record bodies inline (`[reason]`, `[tags]`, `[detail]`, `[resolved]`) so agents can see when a full hydrate is needed without expanding the compact row.
- Agent compact output now ends with `! compact: reasons/details omitted; hydrate before action` to reinforce that compact is an index, not the source record.
- `gg status` now warns when hydration/re-fetch rates are low enough that compact may be used as source-of-truth.

## [0.3.5] - 2026-05-12

### Fixed

- `gg session-start` now waits for the bounded brain auto-backup export before exiting, so project brain snapshots are reliably refreshed for other agents and projects instead of being abandoned by a fire-and-forget goroutine.

## [0.3.4] - 2026-05-12

### Added

**Compact system overhaul — agent auto-compact + 4 new surfaces**

- `isCompactActive(cmd)` — unified resolution: explicit `--compact` flag > `GG_COMPACT` env > agent origin (`GG_ROLE`/`GG_AGENT`/`--from`) with `--compact` flag registered > off. Agents skip the flag and get compact by default; humans stay on rich output; the flag always wins for explicit opt-out.
- `--compact` now available on: `gg inbox`, `gg task list`, `gg bug list`, `gg context --for-task` (previously only on `search`/`context`/`impact (file)`/`task get`).
- Shared line builders in `cmd/compact.go` — 7 duplicated render sites collapsed; format changes land in one place. `renderer_v:1` stamp in telemetry entries so aggregates across format changes can be bucketed.
- New `compact_tokens_saved` metric in `gg status` and `gg telemetry summary` (bytes/4 heuristic). Output: `Compact  74 calls, 208.5 KB / ~53K tok saved (avg 59% reduction)`.

### Fixed

- `compactTrim(s, n<=1)` no longer panics on `runes[:-1]` (latent bug — no caller passed 0 before, but the guard prevents future regressions).
- `gg impact TASK-X --compact` and `gg impact BUG-X --compact` now actually compress output. Previously both appended a 1-line summary after the default render, *increasing* bytes and skipping telemetry — the flag was dead-code.
- `gg impact BUG-X` default (non-compact) output now renders Related Decisions / Tasks / Rejections sections. They were fetched into the result struct but dropped from text mode (JSON output was unaffected).
- `gg audit decide-gaps --compact` now records telemetry via `emitCompact` (was silently emitting without a telemetry entry).
- `gg task get --compact --with-context` baseline measurement now includes the context block on both paths, so the savings percentage reported in `gg status` is honest instead of comparing compact-without-ctx against default-with-ctx.

**`gg watch` — real-time inbox and event stream**

- `gg watch` tails the project's telemetry JSONL and polls the inbox simultaneously, emitting new entries as they arrive. Designed for terminal status bars, desktop notification scripts, and agent-side monitoring loops.
- Flags: `--role` (filter by recipient), `--event` (filter by telemetry event type), `--tag`, `--since` (ISO timestamp or relative duration), `--format ndjson|pretty`, `--no-inbox`, `--no-telemetry`.
- stdout-pipeable: any tool that reads a line-delimited stream works without extra wiring.

**`gg brain backfill` — migrate implicit Task↔Decision links to Memgraph edges**

- Scans Qdrant for implicit Task↔Decision relationships and writes them as explicit `(Decision)-[:DECIDES]->(Task)` edges in Memgraph so `gg impact` and decision-traversal queries work on older projects that predate the edge model.
- Two sources: (1) `Decision.task_id` direct field (always migrated — unambiguous); (2) tag overlap where exactly one decision and one task share a tag (ambiguous multi-matches reported and skipped).
- Dry-run by default — pass `--apply` to execute. Idempotent `MERGE` with `created_by=backfill_v1` tag for rollback. Post-migration audit prints counts.

**`gg gsd audit` — GSD ↔ gg mirror drift detection**

- Compares `.gsd/gsd.db` task state against gg tasks whose titles contain `[GSD:<milestone>-<slice>-<task>]`, reporting tasks present in GSD but missing from gg. Exits non-zero on drift so CI can gate on mirror integrity.

**Task lifecycle auto-broadcast**

- `gg task create`, `gg task done`, and `gg task block` now broadcast a short summary to `all` automatically when `GG_AGENT` or `GG_ROLE` is set and `GG_NO_AUTO_NOTIFY` is unset. Parallel sessions see task state changes without manual `gg tell` calls.
- `GG_NO_AUTO_NOTIFY=1` suppresses the broadcast (same escape valve as the verify-gate notify).

**`gg tell` `@role` mention syntax + multi-target comma fanout**

- `@role` mentions in message bodies are auto-routed to the named recipient's inbox in addition to the declared target. Inbox renders `<<@role>>` so mentions are visually distinct.
- Comma-separated targets in the first positional argument (`gg tell "developer,qa" "..."`) fan the same message out to multiple recipients atomically.

**Claude Code PreToolUse guard**

- `gg gsd-guard` (hidden, invoked by a `PreToolUse` hook) reads the tool-call JSON from stdin and blocks `gsd_plan_*` MCP calls when `tracker.canonical: gg` is set in `.gg/config.yaml`, redirecting agents to `gg task create`.
- Installed automatically by `gg doctor --install-agent-hooks`.

**`gg init` AGENTS.md tracker governance + `gg doctor --install-agents-md`**

- The `gg init` AGENTS.md template includes a `## Tracker Rules` section naming gg as canonical and listing the forbidden MCP calls.
- `gg doctor --install-agents-md` injects the managed block into an existing project's AGENTS.md (idempotent).

**UserPromptSubmit inbox drift-detection hook**

- `gg doctor --install-agent-hooks` now writes a `UserPromptSubmit` hook that surfaces unread messages as agent context on every prompt via `gg inbox --peek`.

### Removed

- **`gg discuss`** — removed after deprecation window (TODO(v0.2) marker). Discussion tracking is handled by `gg record` (decisions) and `gg task create` (action items). 0 calls in dogfood.
- **`gg note`** — removed after deprecation window (TODO(v0.2) marker). Use `gg record` for decisions or commit messages for ambient context. 1 call in dogfood.

## [0.2.0] - 2026-04-18

### Added

**Pre-done verify gate — turn `gg` from a notebook into a checkpoint**

- `gg task done` now runs `.gg/hooks/pre-task-done.d/*.sh` **before** writing the new task state. Any non-zero exit aborts the transition with exit code `7` (`ExitVerifyFailed`) and the task stays in its current state.
- Pre-hooks are always strict by design. `hooks.strict` in `.gg/config.yaml` continues to govern only the post-done `task-done.d` hooks (advisory).
- Hook env contract — shared with future gates like `pre-review-approve.d`: `GG_TASK_ID`, `GG_TASK_SUMMARY`, `GG_PROJECT_ID`, `GG_ACTOR` (`GG_ROLE` or `GG_AGENT`).

**Cross-agent smart rejection signals**

- On rejection, stderr emits a single NDJSON event line with stable keys so any agent (Claude Code, Codex, Cursor, Aider, CI) can program against it without scraping text:
  `{"event":"verify_failed","task":"TASK-042","hook":"10-build.sh","exit":1,"ts":"<rfc3339>","detail":"<tail>"}`
- Internal `gg tell` from `verify-gate` to `all` fires best-effort so parallel sessions see the collision in their next `gg inbox` / `gg status` — no per-agent plumbing needed.
- `GG_NO_AUTO_NOTIFY=1` suppresses the broadcast only. Exit code and NDJSON event still fire. Used by CI, reentrant hook scripts, and tests.
- A store-down failure during the notify step is silently swallowed so it can never mask the underlying verify failure.

**`gg doctor --install-task-hooks` — one-command gate setup**

- Walks the project tree up to depth 3 and installs a pre/post hook pair for every `go.mod` and `package.json` it finds. Monorepos (`lift-cli/go.mod`, `packages/api/package.json`) are first-class.
- Skips `.git`, `.gg`, `.gsd`, `node_modules`, `vendor`, `dist`, `build`, `_bmad`, `_bmad-output` so phantom gates from vendored deps never land.
- Installs disambiguated filenames (`10-go-verify-lift-cli.sh`, `10-go-verify-packages-api.sh`) that each `cd` into their manifest directory before running checks via a substituted `__GG_SUBDIR__` placeholder.
- Node template auto-detects the package manager from lockfiles (`bun.lockb` → `pnpm-lock.yaml` → `yarn.lock` → `npm`) and only runs `typecheck` / `build` / `test` when defined in `package.json`.
- Idempotent — existing files are preserved so user edits survive a reinstall.

**Help text and docs**

- `gg task done --help` now documents the verify gate, the exit-7 contract, the NDJSON envelope, the auto-broadcast, and the installer bootstrap path.
- `docs/cli/` reference regenerated against the current command surface: 15 missing command files added (`gg brain *`, `gg wave *`, `gg telemetry`, `gg task review`, `gg status render`, `gg session-start`) and 26 existing files updated with new flags.

### Fixed

- `TestRecord_OriginHuman` in `internal/telemetry` now isolates `GG_AGENT` as well as `GG_ROLE`, so running the suite inside a standard agent session (`GG_AGENT=claude-code`) no longer produces a false positive failure.

### Changed

- Verify-gate internals renamed for gate symmetry ahead of future gates (`pre-review-approve.d`, …). `verifyRejection` kept as a type alias and `sendVerifyFailure` kept as a one-line wrapper for test-suite continuity; unused back-compat stubs (`runPreTaskDoneHooks`, `emitVerifyFailedEvent`) were removed. Call sites prefer the new `gateRejection`, `emitGateFailedEvent`, `sendGateFailure`, `notifyGateFailure`, and the gate-name-parameterised `runGateHooks(cmd, cache, gateName, taskID, summary)`.
- NDJSON payload now marshalled via an explicit `gateFailedPayload` struct with stable JSON tags, so field order in stderr is `event → gate → task → hook → exit → ts → detail` instead of Go's alphabetical map ordering.
- `gg task done` shares a single per-command config cache (`hookConfig`) between the pre-hook and post-hook paths — one `config.GGDir` + one `config.Load` per invocation instead of two.
- Installer walk parameters moved to configuration. `.gg/config.yaml` now accepts `doctor.hook_install.skip_dirs` and `doctor.hook_install.max_depth`; defaults are still the built-in skip list + depth 3.

### Documentation

- `docs/verify-gate.md` — dedicated reference: contract, env vars, NDJSON schema with stability guarantees, escape valves (`GG_NO_AUTO_NOTIFY`, `GG_DEBUG`), exit codes table, and troubleshooting.
- `docs/getting-started.md` picks up the installer one-liner in the "Install the verify gate" section.
- `docs/ONBOARDING.md` key-commands table links `gg task done` to the gate and adds `gg doctor --install-task-hooks`.
- `docs/adapters.md` new "gg task-lifecycle hooks" subsection distinguishes pre-done blocking from post-done advisory.
- `docs/demo.sh` now demonstrates `gg doctor --install-task-hooks` at the end of the walkthrough.
- `AGENTS.md` documents the monorepo walk defaults, skip-dir override, and the symlink caveat.

## [0.1.0] - 2026-04-14

### Added

**Core CLI**
- `gg init` — bootstrap project: creates `gg.yaml`, `docker-compose.yaml`, `AGENTS.md`, and `RULES.md` in the project root
- `gg status` — session overview: pending tasks, unread messages, open discussions, and recent decisions
- `gg search <query>` — semantic vector search across decisions, rejections, tasks, notes, and bugs
- `gg record "text"` — canonical verb for recording decisions and rejected approaches (`--decision-status=rejected`); supports `--reason`, `--tags`, `--task`, `--from`
- `gg task create/list/get/done/block` — full task lifecycle with priority, tags, detail, and file-locked ID allocation
- `gg tell <role> <message>` / `gg inbox` — agent-to-agent messaging with sender attribution and read tracking
- `gg note "text"` — ambient context notes, semantically searchable, no lifecycle overhead
- `gg context <query>` — knowledge bundle retrieval: related decisions, rejections, tasks, and notes in one call
- `gg discuss open/resolve/dismiss` — open discussion lifecycle with mandatory resolution before session close
- `gg bug report/triage/start/fix/wontfix` — full bug lifecycle with severity tiers and retrospective enforcement
- `gg doctor` — runtime health check: Qdrant/Memgraph connectivity, indexer binary detection, and `--install-indexers` flag for auto-install via native package managers (go/npm/pip)
- `gg doctor --reconcile` — manual trigger for outbox convergence
- `gg impact <file>` — downstream impact report: graph-traced dependents, exported symbols, and related KB entries
- `gg check` — pre-push gate: surfaces high-severity impact before commits leave the machine
- `gg index` — SCIP-based code graph indexing (Go, TypeScript, Python runners)
- `gg index --changed` — incremental re-index using `git diff` + 1-hop graph invalidation; falls back to full re-index on non-ancestor HEAD
- `gg reembed` — re-embed all stored entries when embedding model changes

**Storage & Embeddings**
- Qdrant vector store with per-project collection namespacing (`{projectID}_decisions`, `_tasks`, etc.)
- Ollama-backed local embeddings (replaced OpenAI dependency); dimension validated at `Generate` time
- File-locked sequential ID allocators for tasks, discussions, and bugs — collision-free under 50 concurrent goroutines
- Outbox pattern for dual-store writes (Qdrant + Memgraph): `UpsertNode`, `UpsertEdge`, and convergence via `--reconcile`
- Embedding model metadata stored with collection; `gg reembed` detects and migrates on model change

**Graph (Memgraph)**
- Memgraph Go client via `neo4j-go-driver/v5` (chosen over `memgraph-go-client` for Bolt compatibility and maintenance)
- Per-project isolation: all nodes stamped with `project_id`; `graph.New()` requires projectID; multi-project sharing safe
- Single query choke-point in `internal/graph/queries.go` — all Cypher routed through `runQuery`, raw `sess.Run` forbidden outside the package
- Boundary node schema for cross-file symbol edges
- Hybrid tier resolution metadata on graph nodes (SCIP-resolved vs. heuristic)
- `indexed_at_commit` tracking + `SweepProject()` — ghost symbols from branch switches are reaped on full re-index
- Non-ancestor detection: if HEAD is not a descendant of the last indexed commit, triggers a full re-index

**Code Indexing**
- SCIP-based indexing pipeline: `scip-go`, `scip-typescript`, `scip-python` (npm-installed)
- Version skew compat-matrix for SCIP indexers (`internal/index/compat`)
- `--changed` invalidation contract spec (`internal/index/CHANGED_CONTRACT.md`)
- `ErrIndexerMissing` typed error for missing indexer binaries

**AGENTS.md & Multi-Agent Protocol**
- Auto-generated `AGENTS.md` on `gg init` with full agent collaboration rules
- Autonomous next-task pickup rule (priority queue with claimed-task broadcast)
- OPEN DISCUSSIONS rule: discussions must be resolved/dismissed before session close
- Broadcast-status rule: cross-agent visibility at pick-up, approach choice, blocker, and completion
- Subagent / multi-agent round rule: orchestrator persists subagent decisions to `gg`
- Bug handling rule: report → triage → start → fix → retrospective lifecycle in AGENTS.md
- Note rule: `gg note` for ambient context that doesn't fit a decision/task

**Observability & Infrastructure**
- HealthCheck middleware in `loadDeps`: fail-fast on Qdrant/Memgraph unavailability
- `--json` flag wired to all commands for structured output
- Double-dash stripping in `requireNonEmpty` for robust arg parsing
- GitHub Actions CI: ubuntu/macos/windows matrix, race detector, `golangci-lint`

**Documentation**
- `README.md`: hero pitch, quickstart, prerequisites, architecture overview
- `docs/architecture.md`, `docs/commands.md`, `docs/adapters.md`, `docs/getting-started.md`, `docs/roadmap.md`
- `LICENSE` (MIT)

### Changed

- Switched embedding backend from OpenAI to local Ollama — no external API dependency, no key required
- `gg init` now creates `AGENTS.md` at project root automatically (previously manual)
- Task ID allocator upgraded from in-memory counter to file-locked (`O_EXCL`) sequential allocator

### Deprecated

- `gg decide` — use `gg record` instead; will be removed in a future major release
- `gg reject` — use `gg record --decision-status=rejected` instead; will be removed in a future major release

### Fixed

- Task ID race condition under concurrent goroutines (replaced with file-locked allocator)
- `scroll_all` truncation in Qdrant scroll API — paginated correctly
- `ctx` propagation through all store and graph calls
- Signal handling and graceful shutdown on interrupt
- `NewValueMap` panic on `[]string` property values in Memgraph
- Qdrant client/server version mismatch warning silenced (cosmetic stderr noise)
- Copylocks vet warning in flock implementation
- `elementId()` not supported in Memgraph 3.0 — replaced with `toString(id(n))`

[Unreleased]: https://github.com/gurkangul/gg-cli/compare/v0.3.23...HEAD
[0.3.23]: https://github.com/gurkangul/gg-cli/compare/v0.3.22...v0.3.23
[0.3.22]: https://github.com/gurkangul/gg-cli/compare/v0.3.21...v0.3.22
[0.3.21]: https://github.com/gurkangul/gg-cli/compare/v0.3.20...v0.3.21
[0.3.20]: https://github.com/gurkangul/gg-cli/compare/v0.3.19...v0.3.20
[0.3.19]: https://github.com/gurkangul/gg-cli/compare/v0.3.18...v0.3.19
[0.3.18]: https://github.com/gurkangul/gg-cli/compare/v0.3.17...v0.3.18
[0.3.17]: https://github.com/gurkangul/gg-cli/compare/v0.3.16...v0.3.17
[0.3.16]: https://github.com/gurkangul/gg-cli/compare/v0.3.15...v0.3.16
[0.3.15]: https://github.com/gurkangul/gg-cli/compare/v0.3.14...v0.3.15
[0.3.14]: https://github.com/gurkangul/gg-cli/compare/v0.3.13...v0.3.14
[0.3.13]: https://github.com/gurkangul/gg-cli/compare/v0.3.12...v0.3.13
[0.3.12]: https://github.com/gurkangul/gg-cli/compare/v0.3.11...v0.3.12
[0.3.11]: https://github.com/gurkangul/gg-cli/compare/v0.3.10...v0.3.11
[0.3.10]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.10
[0.3.7]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.7
[0.3.6]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.6
[0.3.5]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.5
[0.3.4]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.4
[0.2.0]: https://github.com/gurkangul/gg-cli/releases/tag/v0.2.0
[0.1.0]: https://github.com/gurkangul/gg-cli/releases/tag/v0.1.0
