# lth

A persistent memory system for AI agents. Stores, searches, and compacts knowledge across five semantic layers — from raw observations up to core identity — with automatic embedding, graph linking, and LLM-powered enrichment.

## Overview

lth runs as a background daemon that watches your Claude conversation history and ingests new memories automatically. You can also store memories manually, search them with vector + BM25 hybrid search, and generate structured context blocks for AI agents.

Memories are organized in five layers:

| Layer | Name | Purpose |
|---|---|---|
| L1 | core | Identity and values — never decays |
| L2 | principles | Operating rules and guidelines |
| L3 | knowledge | Reusable techniques and domain patterns |
| L4 | workspace | Project context and session decisions |
| L5 | observations | Raw transcripts and ephemeral notes |

Higher layers compact into lower ones automatically. L5 observations cluster into L4 workspace context; L4 distills into L3 knowledge.

## Requirements

- Go 1.25+
- [Anthropic API key](https://console.anthropic.com) (for LLM enrichment and chat)
- Docker (for the local embedding server, auto-started on first use)

## Installation

```bash
git clone https://github.com/mattdurham/lth
cd lth
make install
```

This installs `lth` to `~/bin/lth` and copies Claude Code skills to `~/.claude/skills/`.

## Quick Start

```bash
# Initialize config
lth config init

# Start the daemon (also starts the embedding server)
lth stats

# Store a memory
lth store --layer 2 "Always write tests before implementation"

# Search memories
lth search "testing patterns"

# Generate an agent context block from memory
lth prompt "Go error handling"

# Open the web UI
lth ui   # → http://localhost:8765
```

## Commands

| Command | Description |
|---|---|
| `lth store` | Store a memory (default layer: 5) |
| `lth search` | Hybrid vector + BM25 search |
| `lth prompt` | Generate a structured context block for an AI agent |
| `lth chat` | Interactive RAG chat over your knowledge base |
| `lth ui` | Start web UI with search and chat at :8765 |
| `lth get` | Retrieve a memory by ID |
| `lth list` | List all memories in a layer |
| `lth graph` | Explore the memory graph |
| `lth stats` | Show memory statistics (also starts daemon) |
| `lth compact` | Run memory compaction manually |
| `lth export` | Export all memories to a ZIP archive |
| `lth import` | Import memories from a ZIP archive |
| `lth sync` | Push/pull memories with a remote lth-server |
| `lth watch` | Manage the background daemon |
| `lth projects` | List all tracked projects |
| `lth read` | Read a file with lth memory context prepended |

## Configuration

Config file: `~/.lth/config.yaml`

```yaml
embedding:
  provider: huggingface          # or ollama
  base_url: http://localhost:8080
  model: nomic-ai/nomic-embed-text-v1.5
  auto_docker: true              # auto-start embedding server via Docker

llm:
  provider: anthropic
  model: claude-haiku-4-5-20251001
  # api_key: ""  # or set ANTHROPIC_API_KEY env var

watcher:
  paths:
    - ~/.claude/projects         # Claude Code conversation history

sync:
  server_url: http://your-server:8090
  account: myaccount
  org: personal
  user: myuser
```

## Daemon

The daemon (`lth watch start`) runs in the background and:

- Watches configured paths for new conversation transcripts (L5 ingestion)
- Runs compaction on a schedule (L5→L4→L3→L2)
- Serves the web UI on port 8765
- Serves Prometheus metrics on port 10010
- Auto-syncs with the server every 10 minutes (if configured)

Start/stop:
```bash
lth watch start
lth watch stop
lth stats   # starts daemon if not running, shows layer counts
```

## Server

`lth-server` is the sync backend. It stores memories as Parquet blobs and serves push/pull endpoints for multiple clients.

```bash
# Build the server binary
go build -o ~/bin/lth-server ./cmd/lth-server

# Run with config
lth-server --config ~/.config/lth-server/config.yaml
```

## Claude Code Skills

After `make install`, the following skills are available in Claude Code:

- `/lth-work` — Memory-driven team workflow with lth bootstrapping
- `/lth-amnesia` — Bootstrap agent context from lth before working
- `/lth-brief` — Generate a task brief from lth memory
- `/lth-warmup` — Surface recent project context
- `/lth-reflect` — Store session learnings back to lth
