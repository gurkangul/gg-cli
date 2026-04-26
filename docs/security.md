# Secret Scanning — Policy and Allowlist

gg integrates [gitleaks](https://github.com/gitleaks/gitleaks) for secret detection.
The `.gitleaks.toml` at the repo root activates the built-in gitleaks ruleset and
defines allowlist entries for known false positives.

## Quick-start

```sh
# Install the pinned gitleaks binary (~/.gg/bin/gitleaks).
gg doctor --install-secret-scanner

# Scan the repo (staged files + full git history).
gg doctor --check-secrets

# Staged files only.
gg doctor --check-secrets --staged

# Full history only.
gg doctor --check-secrets --history
```

## Pre-commit hook

`gg doctor --install-task-hooks` installs `.gg/hooks/pre-commit.d/20-secret-scan.sh`.
It runs gitleaks against staged files and exits 7 on findings, blocking the commit.

**Bypass (audited):**
```sh
GG_BYPASS_RATIONALE="<why this is a false positive>" git commit ...
```
The bypass is recorded to the gg brain so future sessions can audit it.

**Modes:**

| `GG_SECRET_SCAN` | behaviour |
|---|---|
| `on` (default) | block commit on findings (exit 7) |
| `warn` | print warning, allow commit |
| `off` | skip entirely |

## Fallback behaviour

When gitleaks is not installed, `gg doctor --check-secrets` and the pre-commit hook
both fall back to the internal narrow-regex scanner (`internal/scrub`). That scanner
covers a subset of patterns (Anthropic/OpenAI keys, GitHub PATs, AWS access keys,
PEM private keys, Slack tokens). It emits a warning that coverage is reduced.

## Allowlist governance

Allowlist entries in `.gitleaks.toml` must:

1. **Have a comment** explaining why the match is not a real secret.
2. **Be as narrow as possible** — prefer a specific literal over a broad regex.
3. **Use path scoping** when the false positive only appears in specific directories
   (e.g. `testdata/`, `docs/`).
4. **Be reviewed** before merging — the PR description must explain each new entry.

### Current allowlist entries

| Entry | Reason |
|---|---|
| `AKIAIOSFODNN7EXAMPLE` | AWS example key from docs/tests; this is the canonical AWS documentation placeholder |
| `sk-test[A-Za-z0-9]{16,}` | Synthetic key prefix used in unit tests to verify the scrub package correctly detects and redacts it |
| `YOUR_API_KEY_HERE` | Doc placeholder — appears in README and getting-started guides |
| `<your-token-here>` | Doc placeholder — appears in onboarding examples |
| docs + README paths | Documentation files may contain illustrative key-like strings |
| `.*testdata/.*` | Test fixture directories contain synthetic secrets by design |
| `.*_test\.go` | Go test files may contain synthetic secrets to exercise the scanner |

## Adding a new allowlist entry

1. Add the entry to `.gitleaks.toml` with a comment.
2. Update the table in this file.
3. Run `gg doctor --check-secrets` to verify the allowlist works.
4. In your PR, explain the entry under "Allowlist change" in the description.
