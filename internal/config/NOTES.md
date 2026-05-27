# internal/config -- Design Notes

## 1. BurntSushi/toml for Configuration Parsing

*Added: 2026-05-14*

**Decision:** Use `github.com/BurntSushi/toml` for TOML parsing.

**Rationale:** TOML is the configuration format used by other tools in this project family (wllr,
bob). BurntSushi/toml is the de-facto standard Go TOML library, already in the module cache.

**Consequence:** Configuration file format is TOML. No environment variable overrides are supported
in v1 -- config is file-only for simplicity and auditability.

## 2. ~/.lth/ Home Directory Convention

*Added: 2026-05-14*

**Decision:** All lth state lives under `~/.lth/`: DB at `~/.lth/memory.db`, config at
`~/.lth/config.toml`, watcher state at `~/.lth/watcher-state.json`, PID file at `~/.lth/watch.pid`.

**Rationale:** Follows the XDG-adjacent pattern of single-directory state for single-user tools.
Simple to back up, inspect, and clean up. No XDG_DATA_HOME support in v1 (unnecessary complexity).

**Consequence:** The user's home directory must be writable. All packages that create state call
`os.MkdirAll(~/.lth/, 0755)` defensively.

## 3. L5 Cluster Configuration Fields

*Added: 2026-05-14*

**Decision:** Added `L5ClusterThreshold float32` (default: 0.75) and `L5MinClusterSize int` (default: 2) to the `Compaction` struct.

**Rationale:** The L5->L4 compaction path was redesigned to use cosine-similarity clustering instead of pure time-based windowing. These two fields control the clustering behaviour: `L5ClusterThreshold` sets the minimum pairwise cosine similarity required for two L5 memories to be placed in the same cluster, and `L5MinClusterSize` sets the minimum cluster size before summarization occurs.

**Consequence:** Existing config files without these fields receive the defaults (0.75 threshold, 2 min size) via `applyDefaults`. No breaking change to the config file format.

## 4. Seed Configuration Fields and applyDefaults Refactor

*Added: 2026-05-14*

**Decision:** Added `SeedMinL2 int` (default: 10), `SeedMinL3 int` (default: 20), and `SeedSample int` (default: 100) to the `Compaction` struct. Refactored `applyDefaults` by extracting `applyEmbeddingDefaults`, `applyLLMDefaults`, `applyCompactionDefaults`, and `applySearchDefaults` helpers to keep the function's cyclomatic complexity within the 30-function limit.

**Rationale:** The three new fields control the auto-seed compaction path in the compactor. The refactor was necessary because adding 3 new `if` blocks to `applyDefaults` pushed cyclomatic complexity above the lint threshold (gocyclo limit: 30).

**Consequence:** `applyDefaults` is now a thin dispatcher; all section-specific logic lives in the four helper functions. No behavior change -- all defaults are identical.

## 5. Sync Section Added for lth sync Commands

*Added: 2026-05-23*

**Decision:** Add a `[sync]` section to the existing Config struct for `lth sync push/pull` configuration.

**Rationale:** The sync client needs server URL and identity (account, org, user, team). Reusing the
existing TOML config file avoids a second config file on the client side.

**Consequence:** Existing config files without a `[sync]` section load successfully (all fields zero/empty).
The `lth sync` commands validate non-empty values at runtime.

## 6. Default Watcher Paths Include Both ~/.claude/projects and ~/.wllr/sessions

*Added: 2026-05-29*

**Decision:** `Default()` now sets `Watcher.Paths` to `[~/.claude/projects, ~/.wllr/sessions]` instead
of just `[~/.claude/projects]`.

**Rationale:** lth now supports ingesting conversation history from both Claude CLI (`.claude/projects/`)
and the wllr agent shell (`.wllr/sessions/`). The watcher detects the format of each file at ingest time
via `detectFormat(path)` — paths containing `/.wllr/` use the wllr JSONL format; all others use the
Claude format. Including both directories in the default config means new users automatically get full
coverage without manual configuration.

**Consequence:** Users who previously relied on `Default()` returning a single-element slice for
`Watcher.Paths` must update any test assertions that check the exact length or contents of that slice.
Existing config files with an explicit `watcher.paths` list are unaffected — `applyDefaults` only fills
in the default when the loaded slice is empty.
