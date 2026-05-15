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
