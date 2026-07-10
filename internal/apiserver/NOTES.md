# internal/apiserver — Design Notes

## 1. Reuse the metrics port

*Added: 2026-06-28*

**Decision:** Register all `/api/v1/` routes on the existing metrics `http.ServeMux` (port
`10010` by default) via `metrics.Server.SetAPIHandler`. No second port or listener.

**Rationale:** The daemon already owns an HTTP server for Prometheus metrics and the legacy
`/api/search` + `/api/stats` endpoints. Adding `/api/v1/` to the same mux avoids a second
listen socket, a second goroutine lifecycle, and no additional port configuration surface.

**Consequence:** The API is only reachable when the metrics server is up. The metrics server
always starts; therefore the API is available whenever the daemon is running (and `api.enabled`
is set).

## 2. Markdown-first responses

*Added: 2026-06-28*

**Decision:** Default response format is `text/markdown`. Clients opt-in to JSON via
`Accept: application/json` or `?format=json`.

**Rationale:** The primary consumer of the API is AI agents (and humans via curl). Markdown
renders meaningfully in both contexts without additional tooling. JSON is available for
programmatic callers (lth CLI in proxy mode, scripts).

**Consequence:** Every handler carries two render paths (JSON via `encoding/json`, Markdown
via `renderXxxMD`). The `writeResponse` helper encapsulates the content-negotiation branch.

## 3. Proxy mode: CLI as HTTP client

*Added: 2026-06-28*

**Decision:** When `api.proxy_url` is set in `~/.lth/config.yaml`, `newClientFromGlobalCfg`
returns a `*proxyclient.Client` instead of a `*lth.Client`. All CLI commands use the
`MemClient` interface and are unaware of the switch.

**Rationale:** Enables a proxy workflow: a central lth daemon running on one machine
(or in a container) serves multiple CLI clients over HTTP without any DB file sharing, while
local watcher daemons can still ingest local files and forward those memories to the proxy.

**Consequence:**

- A local daemon may still be started when `proxy_url` is set. In that mode it uses
  `proxyclient.Client` for watcher/service writes and disables local DB-only jobs.
- `*lth.Client`-specific methods not in `MemClient` (e.g. `MemoryStore()`, `Graph()`) are
  not available in proxy mode — only commands using `MemClient` are proxied.
- Commands that bypass `newClientFromGlobalCfg` and call `lth.NewClient` directly (e.g.
  `lth ui`, `lth chat`, `lth compact`) still open a local DB in proxy mode. These are
  UI/local-only commands where a remote daemon is not meaningful.

## 4. AttrStore as a combined interface

*Added: 2026-06-28*

**Decision:** `AttrStore` in `apiserver` requires both `DistinctAttrValues` and `MergeAttr`.
`*lth.Client` satisfies both. The `apiserver.Handler` uses `AttrStore` for the `/projects`
endpoint and the `PATCH /.../attrs` endpoint.

**Rationale:** Avoids a second interface and a second nil-check for the common pattern of
"I need to both read and write attributes". Both methods are always available together.

**Consequence:** If a future store implementation supports reading attributes but not writing
them, the interface will need to be split. Acceptable for v1.
