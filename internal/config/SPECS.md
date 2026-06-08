# internal/config -- Invariants

1. `Load` always applies defaults for any missing field -- no zero-value fields are returned for
   required configuration keys.
2. No environment variable injection -- configuration is file-only (YAML).
3. `ConfigPath` always returns a path rooted at `os.UserHomeDir()/.lth/config.yaml`.
4. `Default()` is always safe to call with no filesystem side effects.
5. `Load` returns an error for invalid YAML syntax and for non-existent paths.
6. `InitDefault` returns an error without writing if the target file already exists and `force` is false.
7. All `~` prefixes in path defaults are resolved to the actual home directory by `Default()`.
8. `Compaction.SeedMinL2` (default: 10), `SeedMinL3` (default: 20), and `SeedSample` (default: 100) control the auto-seed path. Zero values in loaded YAML are replaced by defaults via `applyDefaults`.
9. The `sync` section is optional. If `server_url` is empty, `lth sync` commands return an error
   without contacting any server. `account`, `org`, and `user` are required for sync commands; `team` is optional.
   Existing config files without a `sync` section load successfully with all sync fields empty.
10. `Default()` returns `Watcher.Paths` containing both `~/.claude/projects` and `~/.pi/agent/sessions` by default. Missing directories are tolerated by the watcher at runtime (a warning is logged, ingestion continues for the paths that exist).
11. `ReloadInPlace(path, dst)` re-parses the file and overwrites `*dst` with the new values under `reloadMu`. On parse failure, `dst` is left unchanged and an error is returned — a broken edit must never crash a running daemon. Returns `(changed []string, requiresRestart []string, err error)` where `changed` is the sorted list of dotted field paths that differ and `requiresRestart` is the subset of `changed` not in `HotFields`.
12. `HotFields` is the closed allow-list of dotted field paths whose new values are picked up by the running daemon without a restart, because the consumer re-reads them on every per-tick / per-request iteration. Any field not in this set is reported as requiring a restart even though `ReloadInPlace` writes the new value into `dst`.
13. The daemon polls the config file every 60 seconds via `configReloadLoop` (in `cmd/lth/watch.go`). The poll uses mtime; the file is only re-parsed when mtime increases. On parse failure, `lastMtime` is NOT advanced so the next tick will retry once the user fixes the typo.
