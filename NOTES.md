# lth — Design Notes

## 1. Pure-Go SQLite via modernc.org/sqlite

*Added: 2026-05-14*

**Decision:** Use `modernc.org/sqlite` (ccgo-transpiled C → Go) for all SQLite access, including
the `vec0` virtual table for vector KNN search via `modernc.org/sqlite/vec`.

**Rationale:** The requirement prohibits CGO. `modernc.org/sqlite` is a pure-Go transpilation of
the SQLite C source, satisfying this constraint without the performance penalty of a from-scratch
Go SQLite implementation. The `sqlite-vec` extension (vec0 virtual table) is also transpiled in
the same module — no separate dependency needed.

**Consequence:** Build is CGO-free. Compilation is slightly slower than mattn/go-sqlite3 due to
the larger transpiled source, but this is acceptable for a developer tool.

## 2. sqlite-vec vec0 for Vector Search

*Added: 2026-05-14*

**Decision:** Use `vec0` virtual table for KNN embedding search rather than brute-force Go or HNSW.

**Rationale:** `modernc.org/sqlite/vec` is already pure-Go (same transpilation approach), already
in the local module cache, and provides ANN-class KNN performance without implementing HNSW.
At scale ≤ 100K memories, vec0 is sufficient and correct.

**Consequence:** Embedding dimension is baked into the schema (`float[768]`). Changing the
embedding model requires a migration. The embedding BLOB is kept in `memories.embedding` for
cosine similarity computation and export; `memories_vec` is the KNN index only.

## 3. WAL Mode + Single Writer

*Added: 2026-05-14*

**Decision:** WAL journal mode + `SetMaxOpenConns(1)` per process.

**Rationale:** WAL allows concurrent cross-process reads while serializing writes. The daemon and
CLI are separate OS processes; WAL handles their concurrent DB access. `SetMaxOpenConns(1)` within
each process prevents internal write contention.

**Consequence:** Write throughput is serialized per process but cross-process reads are concurrent.
This is appropriate for a personal tool with low write frequency.

## 4. Daemon Forked as Subprocess

*Added: 2026-05-14*

**Decision:** The background daemon is forked as a subprocess using `exec.Command(os.Executable(), "watch", "daemon")` with `Setsid: true` to detach from terminal.

**Rationale:** Embedding the daemon in the CLI process would require the CLI to stay running.
Forking allows the CLI to exit while the daemon continues. PID file at `~/.lth/watch.pid` provides
liveness checks and idempotent start semantics.

**Consequence:** Daemon management uses PID files. Race condition on dual-fork is mitigated by
the daemon checking for an existing live PID on startup and exiting if found.

## 5. Composite Scoring Formula

*Added: 2026-05-14*

**Decision:** `score = α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m)` where λ=0.995/hour.

**Rationale:** Balances recency (exponential decay), curated importance (LLM-scored 1-10),
and semantic relevance (cosine similarity with query embedding). Equal weighting (α=β=γ=1/3)
as default; callers can override for different retrieval behaviors.

**Consequence:** L1 memories (decay_rate=0) are permanent; their time component stays at
`exp(0) = 1.0`. L5 memories decay quickly (decay_rate=0.5).

## 6. Web UI and Search API on the Daemon HTTP Server

*Added: 2026-05-14*

**Decision:** Extend the existing daemon HTTP server (`:10010`) with a JSON search API
(`POST /api/search`, `GET /api/stats`) and a self-contained single-page web UI at `/ui`.
The `metrics.Server` struct receives a `memory.Store` reference at construction; a `withStore`
guard returns 503 when no store is attached (e.g. tests).

**Rationale:** The daemon already owns an HTTP server for Prometheus metrics. Adding the search
API to the same server avoids a second listener, a second port, and a second process lifecycle.
The web UI fires parallel per-layer fetch calls (one "agent" per layer L1–L5), mirroring the
multi-agent fan-out pattern used in orchestration workflows, making the parallel search structure
visually explicit in the UI.

**Consequence:** Callers of `metrics.NewServer` must pass a `memory.Store` (or `nil`). All API
endpoints degrade gracefully to 503 when the store is nil, so tests that only exercise metrics
endpoints are unaffected.

## 7. Auto-Context Generation via `lth prompt` and Skills

*Added: 2026-05-15*

**Decision:** Added `lth prompt <query>` command, three skills (lth-warmup, lth-brief, lth-reflect), and a hook (lth-inject.sh) to automatically inject memory context into agent workflows.

**Rationale:** Agents bootstrapping from lth were repeating the same 3-search pattern independently. `lth prompt` centralizes this into one command that outputs a ready-to-embed structured block. Skills and hooks wire this into workflows without requiring each agent to know the search pattern.

**Consequence:** Skills calling `lth prompt` depend on the daemon. The UserPromptSubmit hook is opt-in and only fires for workflow commands (/bob:work, /bob:explore, /lth:brief).

## 8. Export/Import for Full DB Reconstruction

*Added: 2026-05-15*

**Decision:** Export and import bypass pkg/lth client and open internal/db directly (same pattern as compact.go) to preserve original embeddings, timestamps, importance, and valence exactly.

**Rationale:** client.Store() re-embeds every memory (requires running embedding server) and resets importance/valence to defaults. Direct DB insertion gives exact reconstruction.

**Consequence:** Export/import are daemon-exempt. The zip format (chunked JSONL + manifest.json) allows partial imports and is human-inspectable with any zip tool.
