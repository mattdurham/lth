# cmd/lth — Design Notes

## 1. Cobra Framework

*Added: 2026-05-14*

**Decision:** Use `github.com/spf13/cobra` for CLI command structure.

**Rationale:** Cobra is the established CLI framework for Go (used by kubectl, docker, etc.).
Already in the module cache. Provides subcommands, persistent flags, and help generation.

**Consequence:** Each subcommand is in its own file. The root command's `PersistentPreRunE`
handles config loading and daemon auto-start.

## 2. Daemon Auto-Start

*Added: 2026-05-14*

**Decision:** Every memory-touching command calls `ensureDaemon(cfg)` in `PersistentPreRunE`.

**Rationale:** Agents shouldn't need to manually start the daemon before using `lth store` or
`lth search`. Auto-start makes the CLI ergonomic for both humans and agents.

**Consequence:** The first invocation of any DB command may have a brief delay (< 1s) while
the daemon starts. Subsequent invocations are instant (PID file probe is fast).

## 4. REST API and Proxy Mode

*Added: 2026-06-28*

**Decision:** The daemon REST API (`/api/v1/`) reuses the existing metrics port (`:10010`)
and is opt-in via `api.enabled: true` in `~/.lth/config.yaml`. Proxy mode is enabled by
setting `api.proxy_url` to the URL of a daemon.

**Rationale:** Avoids a second port and listener. `api.enabled` defaults to false to
preserve existing behaviour for users who do not need the REST API. Proxy mode lets
multiple machines share a single lth daemon without sharing the SQLite file. A machine can
still run a local watcher daemon in proxy mode; local transcript/markdown/issues ingestion is
forwarded to the configured proxy instead of being written to a local DB.

**Consequence:** All CLI commands that call `newClientFromGlobalCfg` transparently switch
between local-DB and HTTP-proxy mode. The daemon also switches its watcher/service store to
the proxy client when `api.proxy_url` is set, while local DB-only daemon jobs are skipped.
Commands with hard-coded `lth.NewClient` calls (`lth ui`, `lth chat`, `lth compact`) always
use a local client.

## 3. Web Chat: Client-Side History

*Added: 2026-06-01*

**Decision:** Chat history is passed client-side in every POST /chat request rather than
stored server-side in a session map.

**Rationale:** Stateless server is simpler — no mutex, no session GC, no restart-loses-history
problem. For a dev tool, conversation payloads are small (10-20 turns × ~500 chars = ~10 KB).
The pattern is consistent with handleUISearch (also stateless).

**Consequence:** The server becomes a pure function: (history, message) → (reply, updated_history).
Long conversations send slightly larger payloads but this is negligible in practice.
