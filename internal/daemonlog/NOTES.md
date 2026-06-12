# internal/daemonlog — Design Notes

## 1. Why a Custom Rotator Instead of lumberjack

*Added: 2026-06-12*

**Decision:** Hand-rolled ~200-line rotator using only stdlib, instead of
adding `gopkg.in/natefinch/lumberjack.v2` as a dependency.

**Rationale:** Daily rotation with N-day retention is a small, well-defined
problem. Lumberjack adds a transitive dep for what is ultimately three
filesystem operations: rename, open, and unlink. The lth dependency surface
is already deliberately small, and a custom rotator gives us behaviors that
lumberjack does not (notably the `dup2` over `os.Stdout` and `os.Stderr` so
direct prints follow rotation).

**Consequence:** No new dependency. Easier to audit. Behavior is exactly what
the spec needs and nothing more.

---

## 2. Lazy Rotation Inside Write

*Added: 2026-06-12*

**Decision:** Check for day rollover inside every `Write` call, under the
existing mutex. No background goroutine; no timer; no ticker.

**Rationale:** The daemon writes log lines frequently enough (multiple per
minute even at low ingest) that any rollover is detected within seconds of
the day actually changing. A background goroutine would add a goroutine, a
shutdown contract, and a goroutine leak risk for negligible benefit. The
mutex acquisition is already required for the write itself, so the rotation
check is free.

**Consequence:** A perfectly silent daemon will not rotate until something
finally writes after midnight. That is acceptable — by definition there is
nothing to lose by deferring rotation in that case.

---

## 3. dup2 over os.Stdout / os.Stderr

*Added: 2026-06-12*

**Decision:** When `RedirectStdFDs` is true, the rotator calls
`syscall.Dup2(rotator.f.Fd(), os.Stderr.Fd())` (and Stdout) after every
file open.

**Rationale:** The parent process opens `daemon.log` and passes it to the
child as `cmd.Stdout`/`cmd.Stderr`. Without `dup2`, on the first rotation
the child's stdout/stderr would continue writing to the renamed
`daemon-YYYY-MM-DD.log` file, while slog (via the rotator's `io.Writer`
contract) would write to the fresh `daemon.log`. The result: panics,
third-party direct prints, and any code outside slog would silently drift
into the wrong file. `dup2` keeps fd 1 and fd 2 always pointing at the
current file.

**Consequence:** This is Unix-specific. The package will not build on Windows
as currently written. lth's daemon is already POSIX-only via `os/signal`
SIGTERM use, so this does not narrow the supported platforms.
