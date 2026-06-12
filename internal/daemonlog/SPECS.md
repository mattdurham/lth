# internal/daemonlog — Invariants

1. `New(opts)` opens `daemon.log` for append in `filepath.Dir(opts.Path)` and creates the directory if missing.
2. If an existing `daemon.log` is non-empty and its mtime is not on today's UTC date, it is renamed to `daemon-YYYY-MM-DD.log` (using the mtime date) before the fresh `daemon.log` is opened.
3. `Write(p)` is safe for concurrent use. Before every write, the rotator compares `time.Now().UTC()` to the date the current file was opened; on mismatch it rotates before writing.
4. Rotation closes the current file, renames `daemon.log` → `daemon-<openDate>.log`, opens a fresh `daemon.log`, and calls `purgeOld`.
5. `purgeOld` removes any `daemon-YYYY-MM-DD.log` whose date is strictly less than `today - RetainDays` (lexical ISO date compare). Files with malformed names are ignored. `RetainDays <= 0` disables pruning entirely.
6. When `RedirectStdFDs` is true, `Dup2` points `os.Stdout` and `os.Stderr` at the rotator's current fd after every open (initial + each rotation). Dup2 errors are non-fatal.
7. `Close` flushes and closes the current file; subsequent `Write` returns `io.ErrClosedPipe`.
