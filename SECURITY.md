# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.3.x   | Yes       |
| 0.2.x   | Yes       |
| 0.1.x   | Security fixes only during alpha |
| < 0.1.0 | No        |

gg is still alpha software. Supported alpha lines receive best-effort security
fixes; upgrade to the latest tagged release before reporting a vulnerability
unless the report is specifically about an older supported line.

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

## Credentials

Memgraph credentials (password, username, URI) should never be written into
`.gg/config.yaml`. Use environment variables instead — they override the config
at runtime without touching the file:

```sh
export MEMGRAPH_PASSWORD="your-password"   # overrides memgraph.password
export MEMGRAPH_USERNAME="your-user"       # overrides memgraph.username
export MEMGRAPH_URI="bolt://host:7687"     # overrides memgraph.uri
```

`gg init` leaves `password: ""` in the generated config and prints a reminder
to use `MEMGRAPH_PASSWORD`. If you find a credential written in plaintext in
`.gg/config.yaml` in any project, treat it as a potential leak and rotate it.

## No secrets in history

A secret audit was performed on the git history on 2026-04-14. No credentials were found committed. The `.gitignore` guards against `.env`, `*.pem`, `*.key`, `credentials.json`, and `secrets.json`. If you find a credential in the history, report it as a vulnerability.

## Credits

Contributors who responsibly disclose verified vulnerabilities will be credited in the release notes, unless they prefer to remain anonymous.
