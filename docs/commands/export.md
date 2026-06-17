# gg export / gg import

Move your entire knowledge base between machines, repos, or team members.

## Export

```sh
gg export                          # writes gg-export-<date>.json.gz
gg export my-project.json.gz      # custom output path
```

Exports all vector-store collections for the current project:

- Decisions and rejections
- Tasks (all statuses)
- Messages
- Discussions (with transcripts)
- Notes
- Bugs

The bundle includes **embedding vectors** so that `gg import` can restore
data without re-embedding — the source Ollama instance is not required on
the target machine.

## Import

```sh
gg import bundle.json.gz          # restore into current project
gg import bundle.json.gz --as <new-uuid>   # import as a different project ID
```

`--as` is useful when:
- Migrating to a new machine and the project ID conflicts with an existing project
- Creating a staging copy alongside production data
- Onboarding a new team member with their own isolated namespace

## Use cases

**Cross-machine transfer:**

```sh
# Machine A
gg export && scp gg-export-*.json.gz user@machine-b:~/

# Machine B
cd /path/to/project
gg import ~/gg-export-*.json.gz
gg doctor
```

**Backup before a risky operation:**

```sh
gg export backup-before-rebase.json.gz
git rebase main
# something went wrong — restore
gg import backup-before-rebase.json.gz
```

**Team onboarding:**

```sh
# Lead exports the shared brain
gg export shared-context-2026-04.json.gz

# New team member
cd /path/to/project
gg init
gg import shared-context-2026-04.json.gz
```

## Round-trip guarantee

`gg export` captures the exact point-in-time state of the vector-store collections.
`gg import` restores them via MERGE semantics — existing points with the same
UUID are overwritten, new points are added. After a round-trip:

```
gg export /tmp/check.json.gz && gg import /tmp/check.json.gz
```

The collection state is identical to the pre-export state. No data is lost or
duplicated.

## Privacy warning

The export bundle contains:

- All decision text, task descriptions, and notes in plain text
- Embedding vectors (float32 arrays — not reversible to text, but do represent your content)

Treat export bundles like sensitive config files. Do not commit them to git.
The `.gitignore` added by `gg init` covers common secret file patterns but
**does not exclude `gg-export-*.json.gz`** — add it manually if needed:

```sh
echo 'gg-export-*.json.gz' >> .gitignore
```

## Format

Bundles are **gzip-compressed JSON** (`.json.gz`). The top-level structure:

```json
{
  "version": 1,
  "project_id": "<uuid>",
  "exported_at": "<RFC3339>",
  "collections": {
    "decisions": [...],
    "tasks": [...],
    ...
  }
}
```

Each collection entry includes `id`, `vector`, and `payload`.

## See also

- [`gg init`](../commands.md) — initialize a new project (required before import)
- [`gg doctor`](../commands.md) — verify collections after import
- [`gg index`](../commands.md) — rebuild the code graph after import (not included in bundle)
