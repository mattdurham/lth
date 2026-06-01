# cmd/lth -- Invariants

1. Every command that touches the DB calls `ensureDaemon` before executing (except `watch` and `config` subcommands).
2. `--json` always produces valid JSON to stdout even on partial errors.
3. All error messages go to stderr (never stdout).
4. A non-zero exit code is returned on any error.
5. `lth watch daemon` is a hidden Cobra command; it is not shown in `--help`.
6. `lth config init` does not start the daemon.
7. The daemon exposes Prometheus metrics at `localhost:10010/metrics` (default port, overridable via `--metrics-port`) and a status dashboard at `localhost:10010/`. The port is daemon-only; all other subcommands ignore this flag.
8. The daemon exposes a search web UI at `localhost:10010/ui` and a JSON API at `localhost:10010/api/search` (POST) and `localhost:10010/api/stats` (GET). These endpoints require a live store; they return 503 if the store is unavailable.
9. `lth prompt` outputs a structured markdown block to stdout. Empty sections are omitted. `--cwd` filters L4 results to memories whose `cwd` attribute matches the current working directory. `--follow-edges` traverses graph edges from L4 results into connected L5 nodes.
10. `lth export` only exports active memories (compacted_at IS NULL). Embeddings serialized as JSON float32 arrays. Layer order in zip: L5 first -> L1, then edges.
11. `lth import` replays in manifest order. Memory and edge inserts use INSERT OR IGNORE -- re-importing is idempotent.
12. `lth sync push` never pushes memories with source="server" to prevent sync loops.
    `lth sync pull` imports received memories, setting source="server" on all received records.
    `lth sync pull` with layers=5 returns an error (L5 has no pull endpoint).

## UI Server Routes (port 8765, `lth ui`)

The `lth ui` command starts a standalone HTTP server on port 8765. It exposes:

- `GET /` — Memory search page (HTML)
- `GET /search` — Memory search JSON API (query params: q, top, layers, expand)
- `GET /chat` — Multi-turn chat page (HTML)
- `POST /chat` — Chat API (JSON request/response, see below)

### POST /chat request

```json
{
  "message": "string — required, the user's current message",
  "history": [{"user": "string", "assistant": "string"}, ...],
  "store":   true
}
```

### POST /chat response

```json
{
  "reply":   "string — assistant's answer",
  "history": [{"user": "string", "assistant": "string"}, ...]
}
```

History is owned entirely by the client and re-sent in full on every request.
The server is stateless with respect to chat sessions.
The `store` field controls whether the Q&A exchange is stored as an L5 memory (default: true).
The backend calls the same `doChat()` function used by `lth chat`, including the agentic tool-use loop.
