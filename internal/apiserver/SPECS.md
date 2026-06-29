# internal/apiserver — Invariants

## Overview

The `apiserver` package implements the lth daemon REST API under `/api/v1/`.
It is registered on the existing metrics HTTP server (same port as Prometheus metrics)
when `api.enabled: true` is set in the config.

## Invariants

1. **Markdown-first content negotiation.** All endpoints return `text/markdown` by default.
   Clients send `Accept: application/json` or `?format=json` to receive JSON instead.

2. **Stateless design.** No handler holds per-request state; all dependencies are injected
   at construction time via `New(store, graph, attrs)`.

3. **Nil-safe optional dependencies.** `GraphStore` and `AttrStore` may be nil. Endpoints
   that require them return `503 Service Unavailable` when their dependency is absent.

4. **Store guard.** All memory-touching handlers are wrapped with `withStore` which returns
   `503` if the store is nil. This mirrors the `metrics.Server.withStore` pattern.

5. **Soft-delete only.** `DELETE /api/v1/memories/{id}` calls `SoftDelete`, never a
   hard delete. This satisfies the SPECS.md invariant on the `memories` table.

6. **Layer bounds enforced.** POST /api/v1/memories validates `layer` is 1–5 and returns
   `400 Bad Request` on violations.

7. **Idempotent store.** Storing the same content twice returns the existing memory (dedup
   by content_hash is enforced by the underlying MemoryStore).

8. **Register is idempotent at construction.** `Register(mux, h)` attaches routes once;
   calling it twice on the same mux panics (standard library behaviour for duplicate paths).

9. **No authentication.** The API is intended for local use. Callers on localhost require
   no credentials.

## Routes

| Method | Path | Description |
|--------|------|-------------|
| POST | /api/v1/memories | Store a new memory |
| GET | /api/v1/memories?layer=N&top=N | List memories in a layer |
| GET | /api/v1/memories/{id} | Get a memory by ID |
| DELETE | /api/v1/memories/{id} | Soft-delete a memory |
| PATCH | /api/v1/memories/{id}/attrs | Merge attributes |
| POST | /api/v1/memories/search | Search memories |
| GET | /api/v1/stats | Aggregate statistics |
| GET | /api/v1/projects | Distinct project attribute values |
| GET | /api/v1/graph/neighbors?id=X&depth=N | Graph neighbors |
| POST | /api/v1/graph/ppr | Personalized PageRank |

## Request / Response shapes

All JSON request/response bodies use `snake_case` keys consistent with the rest of the lth codebase.
