# internal/config — Invariants

1. `Load` always applies defaults for any missing field — no zero-value fields are returned for
   required configuration keys.
2. No environment variable injection — configuration is file-only (TOML).
3. `ConfigPath` always returns a path rooted at `os.UserHomeDir()/.lth/config.toml`.
4. `Default()` is always safe to call with no filesystem side effects.
5. `Load` returns an error for invalid TOML syntax and for non-existent paths.
6. `InitDefault` returns an error without writing if the target file already exists and `force` is false.
7. All `~` prefixes in path defaults are resolved to the actual home directory by `Default()`.
