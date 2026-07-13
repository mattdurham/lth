# cmd/lth -- Invariants

1. Every command that touches the DB calls `ensureDaemon` before executing (except `watch` and `config` subcommands, and when `api.proxy_url` is set — in proxy mode the remote daemon is already running).
2. `--json` always produces valid JSON to stdout even on partial errors.
3. All error messages go to stderr (never stdout).
4. A non-zero exit code is returned on any error.
5. `lth watch daemon` is a hidden Cobra command; it is not shown in `--help`.
6. `lth config init` does not start the daemon.
7. The daemon exposes Prometheus metrics at `localhost:10010/metrics` (default port, overridable via `--metrics-port`) and a status dashboard at `localhost:10010/`. The port is daemon-only; all other subcommands ignore this flag.
8. The daemon exposes a search web UI at `localhost:10010/ui` and a JSON API at `localhost:10010/api/search` (POST) and `localhost:10010/api/stats` (GET). These endpoints require a live store; they return 503 if the store is unavailable.
9. When `api.enabled: true` in config, the daemon registers a full REST API under `/api/v1/` on the metrics port (same port, no second listener). Default: false.
10. When `api.proxy_url` is set in config, CLI commands forward all memory operations to the remote daemon at that URL via `proxyclient.Client`. No local DB connection is opened. `lth ui`, `lth chat`, and `lth compact` are exempt — they always use a local client.
11. `newClientFromGlobalCfg` returns a `*proxyclient.Client` when `api.proxy_url` is set, or a `*lth.Client` otherwise. Both satisfy the `MemClient` interface.
12. `lth prompt` outputs a structured markdown block to stdout. Empty sections are omitted. `--cwd` filters L4 results to memories whose `cwd` attribute matches the current working directory. `--follow-edges` traverses graph edges from L4 results into connected L5 nodes.
13. `lth export` only exports active memories (compacted_at IS NULL). Embeddings serialized as JSON float32 arrays. Layer order in zip: L5 first -> L1, then edges.
14. `lth import` replays in manifest order. Memory and edge inserts use INSERT OR IGNORE -- re-importing is idempotent.
15. `lth sync push` never pushes memories with source="server" to prevent sync loops.
    `lth sync pull` imports received memories, setting source="server" on all received records.
    `lth sync pull` with layers=5 returns an error (L5 has no pull endpoint).
16. `lth backup` (and its `list`/`restore` subcommands) never auto-starts the daemon (added to the invariant-1 exemption list) — `list` only reads a directory and `restore` requires the daemon NOT running. `lth backup restore` stops the daemon first if it is running, and leaves it stopped afterward; the user must run `lth watch start` explicitly to resume ingestion after inspecting a restore.

## UI Server Routes (port 8765, `lth ui`)

The `lth ui` command starts a standalone HTTP server on port 8765. It exposes:

- `GET /` — Memory search page (HTML)
- `GET /search` — Memory search JSON API (query params: q, top, layers, expand, project)
- `GET /projects` — JSON array of distinct `project` attribute values, for the search page's project dropdown
- `GET /chat` — Multi-turn chat page (HTML)
- `POST /chat` — Chat API (JSON request/response, see below)

`project` on `/search` sets `SearchRequest.FilterAttrs = {"project": <value>}`, which BOOSTS
(1.5x score) memories whose `project` attribute matches -- it does not hard-filter out other
projects, matching `lth prompt --attr project=X`'s existing semantics. Each `/search` result
includes a `project` field (from `Memory.Attrs["project"]`, empty string if unset) so the page
can display it.

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
