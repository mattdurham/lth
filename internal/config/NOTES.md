# internal/config — Design Notes

## 1. BurntSushi/toml for Configuration Parsing

*Added: 2026-05-14*

**Decision:** Use `github.com/BurntSushi/toml` for TOML parsing.

**Rationale:** TOML is the configuration format used by other tools in this project family (wllr,
bob). BurntSushi/toml is the de-facto standard Go TOML library, already in the module cache.

**Consequence:** Configuration file format is TOML. No environment variable overrides are supported
in v1 — config is file-only for simplicity and auditability.

## 3. L5 Cluster Configuration Fields

*Added: 2026-05-14*

**Decision:** Added `L5ClusterThreshold float32` (default: 0.75) and `L5MinClusterSize int` (default: 2) to the `Compaction` struct.

**Rationale:** The L5→L4 compaction path was redesigned to use cosine-similarity clustering instead of pure time-based windowing. These two fields control the clustering behaviour: `L5ClusterThreshold` sets the minimum pairwise cosine similarity required for two L5 memories to be placed in the same cluster, and `L5MinClusterSize` sets the minimum cluster size before summarization occurs.

**Consequence:** Existing config files without these fields receive the defaults (0.75 threshold, 2 min size) via `applyDefaults`. No breaking change to the config file format.

---

## 2. ~/.lth/ Home Directory Convention

*Added: 2026-05-14*

**Decision:** All lth state lives under `~/.lth/`: DB at `~/.lth/memory.db`, config at
`~/.lth/config.toml`, watcher state at `~/.lth/watcher-state.json`, PID file at `~/.lth/watch.pid`.

**Rationale:** Follows the XDG-adjacent pattern of single-directory state for single-user tools.
Simple to back up, inspect, and clean up. No XDG_DATA_HOME support in v1 (unnecessary complexity).

**Consequence:** The user's home directory must be writable. All packages that create state call
`os.MkdirAll(~/.lth/, 0755)` defensively.
