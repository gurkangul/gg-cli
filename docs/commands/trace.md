# gg trace

Inspect span data recorded by `GG_TRACE=1`.

## Enable recording

```bash
GG_TRACE=1 gg search "authentication"
GG_TRACE=1 gg decide "use JWT"
```

Spans are appended to `.gg/traces/YYYY-MM-DD.jsonl` as JSON lines:

```json
{"op":"search","duration_ms":142.3,"ts":1713123456}
{"op":"record.decision","duration_ms":88.1,"is_err":false,"ts":1713123512}
```

## Subcommands

### `gg trace show`

Print recent spans, newest first.

```
gg trace show [--op=<name>] [--since=<duration>] [--limit=N]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--op` | (all) | Filter by operation name |
| `--since` | (all time) | Only show spans newer than this (e.g. `1h`, `2d`, `30m`) |
| `--limit` | 50 | Max spans to display (0 = all) |
| `--json` | false | Output as JSON |

**Example:**

```bash
gg trace show --op=search --since=1h
```

```
TIMESTAMP             OP                              DURATION  ERR
──────────────────────────────────────────────────────────────────
2026-04-14 22:15:03   search                            142.3ms
2026-04-14 22:10:11   search                            138.7ms

2 span(s)
```

### `gg trace summary`

Per-operation latency breakdown (p50/p95/p99).

```
gg trace summary [--since=<duration>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--since` | (all time) | Only include spans newer than this |
| `--json` | false | Output as JSON |

**Example:**

```bash
gg trace summary
```

```
OP                               P50        P95        P99       N
──────────────────────────────────────────────────────────────────────
record.decision               88.1ms    145.2ms    162.0ms       12
search                       138.7ms    201.3ms    245.1ms       31
```

### `gg trace clear`

Delete old trace files to reclaim disk space.

```
gg trace clear [--older-than=<duration>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--older-than` | `7d` | Delete files older than this duration |

**Example:**

```bash
gg trace clear --older-than=30d
# → Deleted 3 trace file(s) older than 30d.
```
