# gg 90-second demo storyboard

Total runtime: ~90 seconds across 4 acts.

## Act 1 — Hook (0–10s)

**Goal:** "AI agents forget things. gg doesn't."

```
$ gg status
```

Output shows a live project with 167 tasks, 43 decisions, 175 messages.
No narration — let the numbers speak.

---

## Act 2 — Install (10–25s)

**Goal:** show the 5-command setup (from docs/ONBOARDING.md).

```
$ go install github.com/gurkangul/gg-cli/cmd/gg@latest
$ docker run -d -p 6334:6334 qdrant/qdrant
$ ollama pull nomic-embed-text
$ cd my-project && gg init
$ gg doctor
```

Focus: `gg doctor` green checkmarks. Cut here — don't show full init output.

---

## Act 3 — Three primitive flow (25–75s)

**Goal:** show the three most common primitives in a realistic scenario.

### 3a. Capture a decision (25–40s)
```
$ gg record "use PostgreSQL, not MySQL" \
    --reason "team knows it, ACID compliance" \
    --tags "database,architecture"
```

### 3b. Create + complete a task (40–60s)
```
$ gg task create "set up database migrations" --priority high
$ # ... (implied: work happens) ...
$ gg task done TASK-168 "golang-migrate integrated, 3 migrations committed"
```

### 3c. Search for context (60–75s)
```
$ gg search "database" --compact
```

Shows the decision we just recorded + related task. 3-5 results.

---

## Act 4 — Close (75–90s)

**Goal:** multi-agent angle.

```
$ gg status
```

Shows updated task list (TASK-168 done) + decision surfaced.

Closing text: "One brain. Multiple agents. No context loss."

---

## Recording notes

- `asciinema rec` at 80×24 terminal
- Typing speed: 60ms/char (adjust in demo script)
- Convert to SVG: `svg-term --in demo.cast --out demo.svg --width 80 --height 24`
- asciinema v2 format required for svg-term-cli
- Crop silence: `asciinema cut --start 0.5 --end 89.5 demo.cast`
- Use `DEMO_LIVE_DATA=1` env to show real gg-cli project stats (276 telemetry calls, 43 decisions, 167 tasks)
