# internal/watcher — Invariants

1. The watcher never deletes or modifies watched files — read-only access only.
2. Deduplication via content hash: duplicate JSONL entries are silently skipped (delegated to `memory.Store`).
3. The byte offset is persisted after each file ingest so a daemon restart only processes new bytes.
4. Tool_result content blocks exceeding 10KB are always skipped regardless of other content in the same message.
5. **Claude format:** Lines where `type` is not `"user"` or `"assistant"` are always skipped. **wllr format:** Lines where `type` is not `"session"` or `"message"` are always skipped (e.g. `tool_call` lines are skipped). **pi format:** Lines where `type` is not `"session"` or `"message"` are always skipped (e.g. `model_change`, `thinking_level_change`).
6. Lines where `role` is `"system"` are always skipped in all three formats. **pi format only:** roles `"user"`, `"assistant"`, and `"toolResult"` are ingested; all other roles are skipped. Content blocks of type `"thinking"` and `"toolCall"` are filtered out of pi messages (only `"text"` blocks become memory content).
7. The scanner buffer size is 1MB to handle large assistant messages.
8. Offset state is written atomically (write to `.tmp`, rename).
9. For each `ingestFile` batch, file paths from tool calls in assistant messages are accumulated per session and stored as a single compact L5 memory ("Files touched: ...") with attrs `source=watcher`, `session=<id>`, `repo=<go-module-path>`. **Claude format:** paths come from `Read`/`Write`/`Edit` tool_use blocks (`input.file_path`). **pi format:** paths come from `read`/`write`/`edit` toolCall blocks (`arguments.path`). **wllr format:** no files-touched memory is produced (different tool call schema).
10. `RepoForPath` resolves the Go module path by walking up to the nearest `.git` directory and reading `go.mod`. Returns `""` if no git root or `go.mod` is found.
11. The file format (Claude vs wllr vs pi) is determined once per file by `detectFormat(path)` — paths containing `"/.pi/agent/sessions/"` use the pi format; paths containing `"/.wllr/"` use the wllr format; all other paths use the Claude format.
12. For wllr format: the `cwd` is sourced from the `session`-type header line and carried forward to all subsequent message lines in the same file via the `carriedCWD` variable. If no session header is found before a message line, `cwd` is the empty string. For pi format: BOTH `cwd` AND the session id are sourced from the `session`-type header line and carried forward via `carriedCWD` and `carriedSessionID`. Pi message records do not repeat these fields.
13. `ParseFilePaths` is only called for Claude-format files. `ExtractPiFilePaths` is only called for pi-format files (lowercase tool names: `read`/`write`/`edit`; argument key `path`). wllr-format files use a structurally different tool call schema and do not produce "files touched" memories.
