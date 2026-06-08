# AGENTS.md — Guide for AI Agents Working in This Repo

## Build & Test

```bash
go build ./...               # build everything
go test ./...                # run all tests
go test ./internal/memory/... # run a specific package
make ci                      # build + test + lint
```

Tests are table-driven. No mocks for the database — tests use real SQLite in-memory or temp files.

## Code Conventions

- All `.go` files in spec-driven modules start with:
  `// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.`
- Each package with non-trivial logic has `SPECS.md` (invariants), `NOTES.md` (design decisions), and `TESTS.md` (test coverage map). Update them alongside code changes.
- Errors always go to stderr; stdout is for data output only.
- `--json` flag must produce valid JSON on all commands.

## Memory Layers

| Layer | Name | Decay rate |
|---|---|---|
| 1 | core | 0.0 (permanent) |
| 2 | principles | 0.01 |
| 3 | knowledge | 0.05 |
| 4 | workspace | 0.1 |
| 5 | observations | 0.5 |

The integer values (1–5) are stored in SQLite. Layer names are display-only, defined in `internal/layers/layers.go`.

## Key Packages

| Package | Role |
|---|---|
| `pkg/lth` | Public client API — `NewClient`, `Store`, `Search`, `Get` |
| `internal/memory` | Core store logic — dedup, async enrichment, compaction |
| `internal/db` | SQLite layer — schema, migrations, batch ops |
| `internal/vector` | Embedding providers (HuggingFace TEI, Ollama) |
| `internal/llm` | LLM providers (Anthropic, Ollama) |
| `internal/graph` | PPR graph for memory linking |
| `internal/compactor` | L5→L4→L3→L2 compaction pipeline |
| `internal/watcher` | fsnotify JSONL watcher for Claude / wllr / pi transcript ingestion |
| `internal/parquet` | Parquet read/write for sync blob storage |
| `internal/blobstore` | Blob store abstraction (local filesystem or S3) |
| `cmd/lth` | CLI — all user-facing commands |
| `cmd/lth-server` | Sync server — push/pull Parquet blobs |

## Sync Architecture

```
client push: exportMemory (JSONL/ZIP) → lth-server → Parquet blobs + attrs sidecar
client pull: lth-server → Parquet blobs + attrs sidecar overlay → importMemory (JSONL)
```

- Content dedup is by `content_hash` (SHA-256 of content). Same content from two machines = one record.
- Attributes (`project`, `tags`, `cwd`, etc.) are stored separately in `memory_attributes` and synced via a sidecar at `{account}/{org}/attrs/{hash[:2]}/{hash}`.
- `InsertMemoryBatch` uses `ON CONFLICT(content_hash) DO UPDATE ... WHERE excluded.updated_at > memories.updated_at` — newer record wins on pull.

## Attributes

Free-form key=value pairs on any memory. Well-known keys:

| Key | Set by | Meaning |
|---|---|---|
| `source` | watcher, sync, store | Origin (`watcher`, `server`, `chat`, etc.) |
| `tags` | LLM (async) | Auto-extracted topic tags |
| `cwd` | watcher | Working directory at time of conversation |
| `file` | watcher | Transcript file path |
| `session` | watcher | Conversation session ID |
| `repo` | watcher | Git repo name |
| `project` | watcher, backfill | Project name derived from `cwd` |
| `agent` | store | Agent that created the memory |

## Config

File: `~/.lth/config.yaml`. Format: YAML. Parsed by `internal/config`. Defaults applied by `config.Default()` for any missing field — no zero-value fields returned.

## Running the Daemon

```bash
lth stats          # starts daemon if not running
lth watch start    # explicit start
lth watch stop     # stop
```

The daemon auto-starts the embedding Docker container on first use.

## lth-server

The sync server stores pushed memories as Parquet files in a blob store (local or S3). It is separate from the client and has its own config file (`lth-server.yaml`).

```bash
go build -o ~/bin/lth-server ./cmd/lth-server
lth-server --config lth-server.yaml
```

## Generating Agent Context

```bash
# Structured context block for an AI agent
lth prompt "Go error handling patterns"

# With working directory filter (L4 workspace)
lth prompt --cwd "database schema migration"

# Boost results matching a project
lth prompt --attr project=grafana/tempo "ingester scaling"
```

## Claude Code Skills

The `skills/` directory contains Claude Code skills installed by `make install-skills`:

| Skill | Invocation | Purpose |
|---|---|---|
| lth-work | `/lth-work` | Full team workflow with lth memory bootstrapping |
| lth-grv | `/lth-grv` | Like lth-work but agents edit Go via the `grv` AST tool |
| lth-amnesia | `/lth-amnesia` | Bootstrap agent context from lth at session start |
| lth-brief | `/lth-brief` | Generate a task brief from lth memory |
| lth-warmup | `/lth-warmup` | Surface recent project context |
| lth-reflect | `/lth-reflect` | Store session learnings back to lth |
