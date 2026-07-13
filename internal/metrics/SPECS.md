# internal/metrics — Invariants

## Overview

The `metrics` package owns all Prometheus metric definitions and instrumentation
wrappers for lth. It exposes an HTTP server at `localhost:10010` (configurable via
`--metrics-port`) serving `/metrics`, `/health`, and a minimal HTML dashboard at `/`.

## Invariants

1. **Single registry**: All metrics are registered with a caller-supplied `*prometheus.Registry`
   (never the default global registry). This allows isolated testing and clean daemon shutdown.

2. **Wrapper pattern for instrumentation**: LLM and Embedder calls are instrumented via
   `InstrumentedLLM` and `InstrumentedEmbedder` wrappers. The wrappers implement the same
   interfaces (`llm.LLM`, `vector.Embedder`) and must not alter the return values from the
   wrapped inner implementation.

3. **Gauge refresh is best-effort**: `RefreshLoop` logs a warning on `Stats` errors and
   continues running. It never panics or returns an error.

4. **Server shutdown is graceful**: `Server.Start` shuts down cleanly within 5 seconds when
   the context is cancelled.

5. **TestHandler exposes the HTTP mux for testing**: No real TCP socket is needed in tests;
   `Server.TestHandler()` returns the `http.Handler` for use with `httptest.NewServer`.

6. **No global state**: The package contains no `init()` side-effects and no package-level
   variables that mutate after program start.

## Metrics

| Name | Type | Labels | Description |
|------|------|--------|-------------|
| lth_memories_total | Gauge | layer | Active memory count by layer (1-5) |
| lth_graph_edges_total | Gauge | — | Memory graph edge count |
| lth_compactions_total | Counter | path | Compaction operations |
| lth_llm_requests_total | Counter | provider, operation, status | LLM API calls |
| lth_llm_request_duration_seconds | Histogram | provider, operation | LLM latency |
| lth_embedding_requests_total | Counter | provider, status | Embedding API calls |
| lth_embedding_request_duration_seconds | Histogram | provider | Embedding latency |
| lth_searches_total | Counter | type | Search operations by type |
| lth_search_duration_seconds | Histogram | — | Search latency |
| lth_watcher_messages_ingested_total | Counter | — | JSONL messages ingested |
| lth_watcher_files_watched_total | Gauge | — | Files currently watched |
| lth_issues_ingested_total | Counter | repo | Issues/comments stored by the issues watcher |
| lth_issues_last_sync_timestamp | Gauge | repo | Unix timestamp of the last completed issues sync attempt (not gated on per-issue success) |
| lth_pr_ingested_total | Counter | repo | PR summaries stored by the PR watcher |
| lth_pr_last_sync_timestamp | Gauge | repo | Unix timestamp of the last completed PR scan attempt (not gated on per-PR success) |
| lth_backup_snapshots_total | Counter | status | Backup snapshot attempts by status |
| lth_backup_last_success_timestamp | Gauge | — | Unix timestamp of the last successful backup snapshot |
| lth_backup_snapshot_bytes | Gauge | — | Size in bytes of the most recent successful snapshot |
| lth_embedding_backfill_giveup_total | Counter | — | Memories soft-deleted because they're too large to embed even after truncation |

## HTTP Endpoints

| Path | Description |
|------|-------------|
| /metrics | Prometheus text format exposition |
| /health | Returns "ok" with HTTP 200 |
| / | Minimal HTML status dashboard |
| /v1/traces | OTLP JSON trace ingest (POST only); registered only when SetReceiver is called |

7. **Conditional /v1/traces route**: The `/v1/traces` route is registered only when a receiver
   is set via `SetReceiver`; it is absent from the mux when no receiver is configured.
