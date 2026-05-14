# internal/config — Design Notes

## 1. BurntSushi/toml for Configuration Parsing

*Added: 2026-05-14*

**Decision:** Use `github.com/BurntSushi/toml` for TOML parsing.

**Rationale:** TOML is the configuration format used by other tools in this project family (wllr,
bob). BurntSushi/toml is the de-facto standard Go TOML library, already in the module cache.

**Consequence:** Configuration file format is TOML. No environment variable overrides are supported
in v1 — config is file-only for simplicity and auditability.

## 2. ~/.lth/ Home Directory Convention

*Added: 2026-05-14*

**Decision:** All lth state lives under `~/.lth/`: DB at `~/.lth/memory.db`, config at
`~/.lth/config.toml`, watcher state at `~/.lth/watcher-state.json`, PID file at `~/.lth/watch.pid`.

**Rationale:** Follows the XDG-adjacent pattern of single-directory state for single-user tools.
Simple to back up, inspect, and clean up. No XDG_DATA_HOME support in v1 (unnecessary complexity).

**Consequence:** The user's home directory must be writable. All packages that create state call
`os.MkdirAll(~/.lth/, 0755)` defensively.
