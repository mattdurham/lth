# internal/backupwatcher — Design Notes

## 1. VACUUM INTO Over `.backup` or a Raw `cp`

*Added: 2026-07-10*

**Decision:** Snapshots are taken via SQLite's native `VACUUM INTO 'path'` (see
`db.VacuumInto`), not `sqlite3 .backup`, the SQLite Online Backup API, or a
raw filesystem `cp` of the `.db` file.

**Rationale:** A raw `cp` of a live WAL-mode database file is unsafe --
it can copy the main file mid-write, or miss data still sitting in the
`-wal` sidecar, producing an inconsistent copy. The Online Backup API is the
traditional safe answer but needs its own page-by-page copy loop and step
API wired up in Go. `VACUUM INTO` is a single SQL statement, built into
SQLite itself, that produces a fully consistent, already-compacted copy in
one call -- and, critically, does not require an exclusive lock the way a
plain `VACUUM` does, so it is safe to run against a live database with
concurrent readers and writers without any coordination with the rest of
the daemon (compaction, ingestion, etc.).

**Consequence:** `db.VacuumInto` only accepts a path that does not already
exist (SQLite's own restriction), which is why `snapshotOnce` always
generates a fresh timestamped temp name rather than reusing one.

## 2. Count-Based Retention, Not Age-Based

*Added: 2026-07-10*

**Decision:** `pruneOldSnapshots` keeps the `Keep` most recent files by
filename sort, not "delete anything older than N days."

**Rationale:** Age-based retention has edge cases that count-based
retention simply doesn't: if the daemon is down for a few days, an
age-based rule could leave fewer than N backups; if a manual snapshot or a
future denser schedule produces more than one per day, it could leave more
than N. Counting files sidesteps both without any clock-math.

**Consequence:** "Keep the last 7 days" (the original ask) is implemented
as "keep the 7 most recent snapshots," which is the same thing under normal
daily-cadence operation and strictly safer under gaps.

## 3. Temp and Partial Files Live Inside `Dir`, Not a System Temp Dir

*Added: 2026-07-10*

**Decision:** Both the uncompressed `VACUUM INTO` temp file and the
in-progress gzip output are written directly inside the configured backup
`Dir`, using `.tmp-memory-*.db` and `memory-*.db.gz.part` naming, rather
than a system temp directory.

**Rationale:** `Dir` is likely a different disk or network mount than the
database's own directory (the whole point of the feature). Since gzip has
to read the VACUUM INTO output regardless of where it lives, keeping it in
`Dir` avoids a cross-filesystem copy that a system-temp-dir-then-move
approach would require. The tradeoff -- stray temp/partial files being
visible inside `Dir` if a snapshot crashes mid-way -- is handled by
`cleanStale` at the start of every subsequent attempt (see SPECS.md
invariant 2), so it's a cosmetic concern for the duration of one crash, not
a lasting one.

**Consequence:** Anyone browsing `Dir` mid-snapshot will see a `.tmp-*` or
`.part` file that isn't a real backup yet. `pruneOldSnapshots` and
`ListSnapshots` both glob specifically for `memory-*.db.gz` (not `*.part`),
so these never get treated as real snapshots.

## 4. `Restore` Is Pure File Manipulation; Daemon Lifecycle Is the CLI's Job

*Added: 2026-07-10*

**Decision:** `Restore(dbPath, snapshotPath)` never checks whether the
daemon is running, never stops or starts it, and never opens the database
itself. `cmd/lth/backup.go`'s `runBackupRestore` handles stopping the
daemon (reusing the existing `watch stop` logic) before calling `Restore`,
and deliberately does not restart it afterward.

**Rationale:** Keeping `Restore` daemon-agnostic makes it independently
testable (see `restore_test.go`) without spinning up a daemon process, and
keeps the one genuinely dangerous, emergency-use operation
(overwriting the live database) as a small, auditable function with no
process-management side effects of its own. Not auto-restarting the daemon
after restore is deliberate: the user should look at what they just
restored before ingestion resumes, matching the design brainstorm's
explicit choice on this point.

**Consequence:** Any future caller of `Restore` (e.g. a hypothetical `lth
restore --auto` flag, or a test) gets the same safety guarantees
(pre-restore copy, atomic decompress-then-rename, stale sidecar cleanup)
without needing to reason about daemon state.
