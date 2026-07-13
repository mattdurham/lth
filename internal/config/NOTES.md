# internal/config -- Design Notes

## 1. YAML for Configuration Parsing

*Added: 2026-05-14, updated: 2026-06-03*

**Decision:** Use `gopkg.in/yaml.v3` for YAML parsing.

**Rationale:** YAML is human-friendly for nested config with comments. The standard Go YAML library
is widely used and already in the module cache.

**Consequence:** Configuration file format is YAML. No environment variable overrides are supported
in v1 -- config is file-only for simplicity and auditability.

## 2. ~/.lth/ Home Directory Convention

*Added: 2026-05-14*

**Decision:** All lth state lives under `~/.lth/`: DB at `~/.lth/memory.db`, config at
`~/.lth/config.yaml`, watcher state at `~/.lth/watcher-state.json`, PID file at `~/.lth/watch.pid`.

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

**Decision:** Add a `sync` section to the existing Config struct for `lth sync push/pull` configuration.

**Rationale:** The sync client needs server URL and identity (account, org, user, team). Reusing the
existing YAML config file avoids a second config file on the client side.

**Consequence:** Existing config files without a `sync` section load successfully (all fields zero/empty).
The `lth sync` commands validate non-empty values at runtime.

## 6. Default Watcher Path is ~/.claude/projects

*Added: 2026-05-29, updated: 2026-06-03*

**Decision:** `Default()` sets `Watcher.Paths` to `[~/.claude/projects]`.

**Rationale:** lth watches Claude CLI conversation history by default. Additional paths (e.g. other
conversation tools) can be added via the config file. The watcher detects the JSONL format of each
file at ingest time via `detectFormat(path)`.

**Consequence:** Existing config files with an explicit `watcher.paths` list are unaffected —
`applyDefaults` only fills in the default when the loaded slice is empty.

---

## 7. Hot Config Reload via mtime Polling

*Added: 2026-06-08*

**Decision:** The daemon polls `~/.lth/config.yaml` every 60 seconds and re-parses
it when the file's mtime increases. On a successful parse, `ReloadInPlace` overwrites
the running `*Config` in place; on parse failure the old config remains live and the
failure is logged at WARN. Field paths that are not in `HotFields` are written but
require a daemon restart to fully take effect (the consumer has already captured the
old value at construction time).

**Rationale considered alternatives:**

- **`atomic.Pointer[Config]` with re-fetch at each read site** — cleanest but requires
  touching every component that currently captures the `*Config` pointer at startup
  (compactor, watcher, mdwatcher, issueswatcher, autoSync, sync push/pull). Large
  refactor for marginal benefit since most fields are captured into local vars
  anyway (ticker intervals, fsnotify watch paths).
- **`SIGHUP` handler** — explicit user signal, no polling overhead, but UX-hostile:
  user has to remember to send the signal after every edit.
- **`fsnotify` on the config file itself** — slightly more responsive than 60s polling
  but adds a watcher dependency for one file. mtime polling at 60s is dirt cheap
  (one `Stat()` per minute) and "config edit visible within a minute" is plenty fast
  for tuning use cases.

**Consequences:**

1. Editing the YAML and saving propagates **hot fields** (Compaction tuning, Search
   weights, Sync credentials, Markdown/Issues lists) automatically within ≤60 seconds.
2. Editing **non-hot fields** (DB.Path, Embedding/LLM construction, Watcher.Paths,
   any `IntervalS`, any `TimeoutS`) logs an INFO message naming them but does NOT
   apply — the daemon must be restarted (`lth watch stop && lth watch start`).
3. A YAML typo is logged and the daemon keeps running with the previous config.
   The poller will retry on the next mtime change.
4. `HotFields` is the source of truth for which fields can be live-tuned. Adding a
   new tunable field to a per-tick consumer is a two-step change: (a) make the
   consumer read from the shared `*Config` (not a captured value), (b) add the
   field path to `HotFields` in `reload.go`.

## 8. Accepted: Inconsistent Field Naming Across Watcher Config Sections

*Added: 2026-07-11*

**Decision:** Do not rename config fields to normalize naming across sections, despite
real inconsistencies flagged by adversarial review: `Markdown.GitHub.CacheDir` and
`PR.CacheDir` both mean "directory this feature clones into," `GWS.OutputDir` means
"directory this feature writes generated files into," and `Watcher.StateFile` /
`PR`'s and `Issues`' state files aren't even a config field at all (hardcoded under
`~/.lth/`) -- four different names for closely related concepts, added independently
across separate days/commits with no shared convention. Similarly `PR.MaxPerScan` and
`Backup.Keep` are both "a safety-bound count" with unrelated names.

**Rationale:** Every one of these is a YAML key in a real, already-deployed
`~/.lth/config.yaml` (see e.g. the user's own config, which sets `pr.cache_dir` and
`pr.max_per_scan` today). Renaming any of them is a breaking config-format change for
existing installs, and this codebase treats backwards compatibility as a hard
constraint elsewhere (patch releases get cherry-picks only, no gratuitous breaking
changes). A cosmetic consistency pass is not worth breaking every existing config file
that references these keys.

**Consequence:** New config sections added in the future should follow a consistent
naming convention going forward (prefer `CacheDir`/`OutputDir` for "directory this
feature owns," matching the majority precedent) even though the existing sections are
left as-is. This note exists so a future reviewer doesn't re-flag the same inconsistency
as an oversight -- it's a deliberate accept, not a miss.
