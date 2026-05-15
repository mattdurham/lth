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

**Decision:** Every DB-touching command calls `ensureDaemon(cfg)` in `PersistentPreRunE`.

**Rationale:** Agents shouldn't need to manually start the daemon before using `lth store` or
`lth search`. Auto-start makes the CLI ergonomic for both humans and agents.

**Consequence:** The first invocation of any DB command may have a brief delay (< 1s) while
the daemon starts. Subsequent invocations are instant (PID file probe is fast).
