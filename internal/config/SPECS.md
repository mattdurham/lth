# internal/config -- Invariants

1. `Load` always applies defaults for any missing field -- no zero-value fields are returned for
   required configuration keys.
2. No environment variable injection -- configuration is file-only (TOML).
3. `ConfigPath` always returns a path rooted at `os.UserHomeDir()/.lth/config.toml`.
4. `Default()` is always safe to call with no filesystem side effects.
5. `Load` returns an error for invalid TOML syntax and for non-existent paths.
6. `InitDefault` returns an error without writing if the target file already exists and `force` is false.
7. All `~` prefixes in path defaults are resolved to the actual home directory by `Default()`.
8. `Compaction.SeedMinL2` (default: 10), `SeedMinL3` (default: 20), and `SeedSample` (default: 100) control the auto-seed path. Zero values in loaded TOML are replaced by defaults via `applyDefaults`.
9. The `[sync]` section is optional. If `server_url` is empty, `lth sync` commands return an error
   without contacting any server. `account`, `org`, and `user` are required for sync commands; `team` is optional.
   Existing config files without a `[sync]` section load successfully with all sync fields empty.
10. `Default()` returns `Watcher.Paths` containing both `~/.claude/projects` and `~/.wllr/sessions`
    (in that order). Both paths are included so lth watches conversation history from both Claude CLI
    and the wllr agent shell by default.
