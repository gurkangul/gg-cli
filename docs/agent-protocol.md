# Agent Protocol — Sandbox EPERM Handling

When `gg doctor` or any write command (`gg record`, `gg task create`, `gg bug report`)
detects **operation not permitted (EPERM/EACCES)** on a localhost TCP connection:

## What this means

The agent is running in a sandboxed environment (Codex harness, restricted CI container,
or similar) where outbound TCP to localhost services (Qdrant, Memgraph, Ollama) is
blocked at the OS/seccomp level. This is **not** a service outage — the services may be
running normally outside the sandbox.

## Agent discipline (mandatory)

When EPERM is detected in any doctor/write path, the agent **MUST**:

1. **Stop** the current operation — do not continue as if the service is down.
2. **Surface the sandbox warning** to the user:
   ```
   ✗ qdrant unreachable at localhost:6334 — operation not permitted (sandbox?)
     → if running under an agent harness, rerun this command outside the sandbox or with escalated permission
   ```
3. **Request escalated permission** from the user rather than silently failing over
   to offline/degraded mode.

**Do NOT silently fall over to offline mode on EPERM.** Offline mode (JSONL fallback)
is correct for service outages (connection refused, timeout). It is incorrect for sandbox
permission errors, because the agent may be operating with incomplete state.

## Diagnosing sandbox restrictions

Use `gg doctor --diagnose-sandbox` as an explicit pre-flight check in agent prompts:

```
gg doctor --diagnose-sandbox
```

This probes a known localhost TCP target and reports:
- `sandbox: TCP localhost permitted` — normal operation, services may be down but TCP works
- `sandbox: TCP localhost BLOCKED — escalate permissions` — sandbox is blocking TCP

## Integration in agent prompts

Add this to agent initialization prompts for sandboxed environments:

```
gg doctor --diagnose-sandbox
```

If the output contains "BLOCKED", the agent should immediately notify the user and halt
gg write operations until permissions are escalated.
