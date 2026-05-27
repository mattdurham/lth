# internal/watcher — Design Notes

## 1. Tail-Append Offset Tracking

*Added: 2026-05-14*

**Decision:** Track byte offsets per file in a JSON state file at `~/.lth/watcher-state.json`.
On each ingest, seek to the stored offset, read new lines, update and persist offset.

**Rationale:** This allows the watcher to survive daemon restarts without re-ingesting old lines.
The content hash dedup in `memory.Store` provides a second line of defense against duplicates.

**Consequence:** If the state file is lost, the watcher re-ingests all files from offset 0.
The content hash dedup prevents duplicate memories.

---

## 2. fsnotify for File Change Detection

*Added: 2026-05-14*

**Decision:** Use `github.com/fsnotify/fsnotify` for file change detection.

**Rationale:** Standard Go file-watching library, cross-platform, already in the module cache.
Watches for Write and Create events on `*.jsonl` files.

**Consequence:** On systems where fsnotify is unavailable, the watcher falls back to polling
(not implemented in v1 — just return an error from New if Add fails).

---

## 3. Separate ParseWllrLine Function for wllr Format

*Added: 2026-05-27*

**Decision:** Add a new `ParseWllrLine` function in `wllr_parser.go` rather than modifying the
existing `ParseLine` signature to handle both formats.

**Rationale:** The wllr JSONL format is structurally different from Claude's:
- Session `cwd` is on the session-type header line (not per-message)
- Messages use a flat `role` field (not `message.role`)
- Tool calls are separate records (not embedded in message content)

Modifying `ParseLine` to handle both would require changing its signature or adding a format
parameter, breaking all existing callers and tests. A separate function keeps each parser
focused, testable in isolation, and backward-compatible.

**Consequence:** `ingestFile` dispatches to the correct parser by calling `detectFormat(path)`
once at the top of the function. The `carriedCWD` state variable is only used in the wllr
branch — Claude format carries `cwd` per-line and does not need it.
