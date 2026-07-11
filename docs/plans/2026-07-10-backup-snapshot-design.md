# Backup/Snapshot Design

*Date: 2026-07-10*

## Purpose

Disaster recovery for `~/.lth/memory.db`: disk failure, corrupted DB, accidental deletion.
A user-configured directory (ideally on a different disk/mount than the DB itself) receives a
daily, complete, drop-in-restorable copy of the memory database. The last 7 (configurable) copies
are retained; older ones are pruned automatically.

Explicitly *not* the goal: portability/migration (already served by `lth export`/`lth import`) or
undo-a-bad-operation workflows requiring point-in-time granularity finer than a day.

## Config

New `backup:` block, following the same self-gating pattern as every other watcher
(`pr:`, `gws:`, `issues:`, `markdown:`):

```go
type BackupConfig struct {
	Dir       string `yaml:"dir"`        // required; no default -- disabled until set
	IntervalH int    `yaml:"interval_h"` // default 24
	Keep      int    `yaml:"keep"`       // default 7
}
```

- `Dir` has no default. Defaulting to an lth-owned directory (e.g. under `~/.lth/`) would likely
  put backups on the same disk as the DB they're protecting against, defeating the purpose. The
  watcher stays disabled (self-gates, like every other watcher when its config is empty) until the
  user points it somewhere -- ideally a different disk, mount, or network share.
- `IntervalH` is a simple ticker, not a wall-clock schedule -- consistent with `gws.interval_h`,
  `issues.interval_s`, etc. No "run at 3am" semantics. First snapshot happens shortly after the
  daemon starts (or after `Dir` is set via config hot-reload); subsequent snapshots follow every
  `IntervalH` hours.
- `Keep` defaults to 7, matching the "last 7 days" ask, but is genuinely a *count* of retained
  files, not an age cutoff (see Retention below).

File naming: `memory-<YYYYMMDD-HHMMSS>.db.gz`, written directly into `Dir`. Lexicographic filename
sort equals chronological order, which both retention pruning and `lth backup list`/`restore` rely
on for "most recent N" and "list available" respectively.

## Taking a Snapshot

New `internal/backupwatcher` package, matching the shape of the existing five watchers
(`New(cfg, ...) *Watcher`, `Run(ctx)`, self-gated, ticker-based, hot-reload friendly per
`cfg.Backup.Dir`/`IntervalH`/`Keep`).

Per-tick sequence:

1. **Clean up any stray temp file** from a previous crashed attempt (`.tmp-memory-*.db` in `Dir`).
2. **`VACUUM INTO`**: `db.ExecContext(ctx, "VACUUM INTO ?", tmpPath)` against the live DB
   connection, targeting `<Dir>/.tmp-memory-<ts>.db`. This is a native SQLite operation that
   produces a fully consistent, compacted, single-file copy as of a transactional snapshot point --
   safe to run against a live WAL-mode DB with concurrent readers/writers, no coordination needed
   with compaction or any other in-daemon writer. `VACUUM INTO` refuses to overwrite an existing
   file, which is why the temp path is always freshly named.
3. **Compress**: gzip the temp file to `<Dir>/memory-<ts>.db.gz`, then remove the uncompressed temp
   file. The final `.gz` name only exists once compression has fully succeeded -- a crash mid-gzip
   leaves only a stray temp file (cleaned up next tick), never a truncated/corrupt file at the final
   name.
4. **Prune**: list `<Dir>/memory-*.db.gz` sorted by filename, delete all but the `Keep` most recent.
   Pruning only runs after a *successful* snapshot -- a failed attempt never touches retention, so
   one bad tick can't cascade into losing older, still-good backups.

Failures at any step (disk full, `Dir` unreachable, VACUUM error, gzip error) are logged at
`slog.Warn` and retried next tick -- never fatal to the daemon, matching every other watcher's
failure-handling convention.

## Metrics

Same pattern as `PRIngestedTotal`/`PRLastSync`:

- `lth_backup_snapshots_total{status}` -- counter, `status` = `success`/`failure`.
- `lth_backup_last_success_timestamp` -- gauge, Unix time of the last successful snapshot.
- `lth_backup_snapshot_bytes` -- gauge, size of the most recent snapshot file. Cheap early warning
  if the DB unexpectedly balloons, or a snapshot comes back suspiciously small/empty.

## Restore

`lth backup restore <file>` -- deliberately a manual, CLI-driven action, never something the daemon
does to itself:

1. Resolve `<file>`: accepts a full path or a bare filename looked up inside `cfg.Backup.Dir`.
2. Stop the daemon if running (reuses the existing `watch stop` logic) -- restoring while the
   daemon holds the DB open is unsafe.
3. Copy the *current* `cfg.DB.Path` to `<cfg.DB.Path>.pre-restore` first, so a restore-to-the-wrong-
   snapshot mistake is itself recoverable. Belt-and-suspenders for a command whose whole purpose is
   emergency use.
4. Gunzip `<file>` into place at `cfg.DB.Path`.
5. Print confirmation and remind the user to run `lth watch start` -- the command intentionally does
   not auto-restart the daemon, so the user can inspect what they just restored before ingestion
   resumes.

`lth backup list` -- companion command, prints the snapshot files in `cfg.Backup.Dir` with their
timestamps and sizes, so `restore` isn't the first time the user is looking at what's available.

## Docs

Per the repo's spec-driven convention (NOTE header on every `.go` file):

- `internal/backupwatcher/SPECS.md` -- invariants: `VACUUM INTO` targets a temp path, renamed
  (via gzip-then-remove-temp) only after successful compression; retention is count-based (`Keep`
  most recent by filename sort), never age-based; a failed snapshot never triggers pruning; `Dir`
  empty means permanently disabled with no default.
- `internal/backupwatcher/NOTES.md` -- rationale: `VACUUM INTO` chosen over `sqlite3 .backup` or a
  raw `cp` (transactionally consistent on a live WAL-mode DB in one API call, no coordination with
  other in-daemon writers needed); count-based retention chosen over age-based (robust to gaps,
  e.g. the daemon being down for a few days).
- Root `NOTES.md` -- one entry noting the 6th watcher, consistent with "the daemon owns all
  background work" architecture already established by `watcher`/`mdwatcher`/`gwswatcher`/
  `issueswatcher`/`prwatcher`.

## Testing

Matching the bar set by `internal/prwatcher`'s tests (real local fixtures, no mocking of SQLite):

- `backupwatcher_test.go`: build a temp `db.DB`, run the snapshot step directly, assert the `.gz`
  file exists, decompresses to a valid SQLite file with the expected row count, and the temp file
  is gone.
- Retention pruning: create N fake `memory-*.db.gz` files with controlled names, assert exactly
  `Keep` remain and they're the most-recently-named ones.
- Crash-safety: leave a stray `.tmp-memory-*.db` in `Dir` before a run, assert it's cleaned up and
  the run still succeeds.
- `restore_test.go`: exercise the full restore flow against a temp `cfg.DB.Path` and a fake
  snapshot dir -- asserts the pre-restore copy exists and the restored file matches the snapshot's
  content.

## Open Items for Implementation

- Exact `BackupConfig` YAML defaults/`applyDefaults` wiring, following the `PRConfig` precedent.
- Whether `VACUUM INTO`'s temp-file path needs to live in `Dir` itself (simplest, avoids a
  cross-filesystem rename after gzip) vs a system temp dir (avoids partial `.tmp` files being
  visible in the backup directory, at the cost of a cross-filesystem copy during gzip).
- CLI wiring for `lth backup list`/`lth backup restore` (new `cmd/lth/backup.go`, mirroring
  `cmd/lth/watch.go`'s stop/start reuse).
