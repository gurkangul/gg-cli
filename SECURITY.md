# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |
| < 0.1.0 | No        |

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Use one of these channels (in order of preference):

1. **GitHub Security Advisory** — [Report a vulnerability](https://github.com/gurkangul/gg-cli/security/advisories/new) (private disclosure, preferred)
2. **Email** — security@gurkangul.dev (for disclosures that don't fit the advisory form)

Include in your report:

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Potential impact
- Suggested fix, if any

## Response SLA

| Stage | Target |
|-------|--------|
| Acknowledgement | 48 hours |
| Triage (severity assessed, reproduce confirmed) | 7 days |
| Fix released | 30 days for critical/high; 90 days for medium/low |

If you don't receive acknowledgement within 48 hours, follow up via email.

We ask that you allow us to complete triage and release a fix before public disclosure.

## Scope

**In scope:**

- `gg` CLI binary and all packages under `cmd/` and `internal/`
- Project isolation mechanism (Qdrant collection namespacing via `projectID`)
- File-locking in sequential ID allocators
- Cross-project data leakage in store or graph layers
- Credential exposure in config, logs, or git history
- Path traversal in any file I/O

**Out of scope:**

- Vulnerabilities in upstream dependencies (Qdrant, Memgraph, Ollama) — report those to their respective projects
- Issues requiring physical access to the machine running `gg`
- Social engineering or phishing

## No secrets in history

A secret audit was performed on the git history on 2026-04-14. No credentials were found committed. The `.gitignore` guards against `.env`, `*.pem`, `*.key`, `credentials.json`, and `secrets.json`. If you find a credential in the history, report it as a vulnerability.

## Credits

Contributors who responsibly disclose verified vulnerabilities will be credited in the release notes, unless they prefer to remain anonymous.
