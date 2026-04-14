# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report security issues by email to: **security@gurkangul.dev**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fix, if you have one

You should receive a response within 5 business days. If you do not, follow up to confirm receipt.

We will:
1. Confirm the vulnerability
2. Prepare a fix
3. Release a patch version
4. Credit you in the release notes (unless you prefer to remain anonymous)

We ask that you give us reasonable time to address the issue before public disclosure.

## Scope

gg is a local CLI tool that writes to a local Qdrant and Memgraph instance. It does not transmit data to external services (all embeddings are local via Ollama).

Areas of particular sensitivity:
- The project ID isolation mechanism (Qdrant collection namespacing)
- File-locking in the sequential ID allocators
- Any future networked features
