# internal/backupwatcher — Invariants

## Snapshotting

1. `Run` is hot-reload friendly: it loops forever, re-checking `cfg.Backup.Dir` on each iteration. While empty it sleeps 60s between checks; otherwise it snapshots then sleeps for `cfg.Backup.IntervalH` (default 24h) — a simple ticker, not a wall-clock schedule. There is no default `Dir`; the watcher is disabled until the user sets one.
2. `snapshotOnce` always calls `cleanStale(dir)` first, removing any `.tmp-memory-*.db` or `memory-*.db.gz.part` files left behind by a previous attempt that crashed mid-VACUUM or mid-gzip.
3. The database copy is taken via `db.VacuumInto(ctx, tmpDBPath)` — a native SQLite `VACUUM INTO` — into a freshly-named temp path inside `Dir`. This is safe against a live WAL-mode database with concurrent readers/writers; it does not require pausing compaction or any other in-daemon writer.
4. The temp `.db` file is gzipped to `<finalPath>.part`, then renamed to `<finalPath>` only after compression fully succeeds. A crash mid-gzip leaves only a `.part` file, cleaned up on the next attempt (invariant 2) — a partially-written file is never visible at the final `memory-<ts>.db.gz` name.
5. `gzipFile`'s destination is opened with `O_EXCL`: it must not already exist. Combined with invariant 2, a naming collision here indicates something unexpected and fails loudly rather than silently overwriting.
6. The uncompressed temp `.db` file is removed after a successful (or failed) gzip attempt.
7. Filenames are `memory-<ts>.db.gz` where `<ts>` is `time.Now().UTC().Format("20060102-150405")` — fixed-width and zero-padded, so lexicographic filename sort equals chronological order. This is relied on by both `pruneOldSnapshots` and `ListSnapshots`.

## Retention

8. `pruneOldSnapshots(dir, keep)` runs only after a *successful* snapshot. A failed attempt never touches retention, so one bad tick can never cascade into losing older, still-good backups.
9. Retention is count-based, not age-based: it always keeps exactly the `keep` most recent files (by filename sort) regardless of gaps (e.g. the daemon being down for several days) or ticks producing more than one snapshot in a day.
10. `cfg.Backup.Keep` defaults to 7 if unset or non-positive.

## Metrics

11. `BackupSnapshotsTotal{status}` is incremented on every attempt (`"success"` or `"failure"`), including a failure to even create `Dir`. `BackupLastSuccessTimestamp` and `BackupSnapshotBytes` are only updated on success.
12. All metrics access is nil-safe (`w.metrics != nil` checked before use), matching every other watcher — `metrics` may be nil in tests or lightweight callers.

## Restore

13. `Restore(dbPath, snapshotPath)` copies `dbPath` (and its `-wal`/`-shm` sidecars, if present) to `<path>.pre-restore` *before* touching anything, so a restore-to-the-wrong-snapshot mistake is itself recoverable. If `dbPath` did not exist yet, no pre-restore copy is made and the returned `preRestorePath` is `""`.
14. The snapshot is decompressed to `<dbPath>.restoring` and only renamed into place at `dbPath` after decompression fully succeeds — a crash mid-decompress never leaves a truncated file at `dbPath`.
15. After a successful restore, any `<dbPath>-wal`/`<dbPath>-shm` sidecars are removed. A `VACUUM INTO` snapshot is a single, self-contained file with no WAL of its own; a sidecar left over from the database just replaced belongs to a different set of pages and must never be replayed against the restored file.
16. `Restore` does not stop or start the daemon, check whether it is running, or open the database itself — it is pure file manipulation. Daemon lifecycle is the caller's responsibility (`cmd/lth/backup.go`'s `runBackupRestore`).
17. `ListSnapshots(dir)` returns snapshots oldest first, parsing each filename's timestamp via `parseSnapshotTime`. A file matching the glob but unparseable or unstat-able is silently skipped rather than failing the whole listing.
