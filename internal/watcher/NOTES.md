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

---

## 4. Separate ParsePiLine Function for pi Agent Format

*Added: 2026-06-08*

**Decision:** Add a third format (`FormatPi`) and parser (`ParsePiLine` / `ExtractPiFilePaths`)
for pi coding agent session logs at `~/.pi/agent/sessions/<encoded-cwd>/*.jsonl`. Auto-detect
by including this path in the default `Watcher.Paths`.

**Rationale:** Pi's JSONL schema is incompatible with both Claude and wllr:
- The session header carries BOTH `cwd` AND the sessionID (`id`); messages omit them entirely.
  This requires carrying *two* values forward, not just `cwd` as in wllr.
- Messages nest the role under `message.role` (like Claude) but include a third role
  `toolResult` (Claude embeds tool results inside a user message; wllr drops them entirely).
- Content blocks use distinct types: `text`, `thinking`, `toolCall`. Tool calls use
  lowercase names (`read`/`write`/`edit`) and an `arguments` object (vs Claude's `Read`/`Write`/`Edit`
  with `input.file_path`).
- Top-level record types include `model_change` and `thinking_level_change` which must be skipped.

**Filtering choices:**
- `thinking` blocks are dropped — they are pre-emission scratch work, very noisy as L5 memories.
- `toolCall` blocks contribute file paths to the per-session "Files touched" memory but their
  arguments are NOT stored as content (consistent with Claude tool_use handling).
- `toolResult` role text is kept (analog of Claude's user-with-tool_result), capped at 10KB per
  block to bound memory size.

**Consequence:** `ingestFile` now has three dispatch branches. The Pi branch maintains its own
`carriedSessionID` alongside `carriedCWD`, and emits "Files touched" memories keyed by the carried
sessionID. If a pi file is somehow processed before its session header line (should never happen
in practice — pi writes the header first), file-paths-touched accumulation is gated on a non-empty
`carriedSessionID` and silently dropped otherwise.
