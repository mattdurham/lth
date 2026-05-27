# internal/watcher — Invariants

1. The watcher never deletes or modifies watched files — read-only access only.
2. Deduplication via content hash: duplicate JSONL entries are silently skipped (delegated to `memory.Store`).
3. The byte offset is persisted after each file ingest so a daemon restart only processes new bytes.
4. Tool_result content blocks exceeding 10KB are always skipped regardless of other content in the same message.
5. **Claude format:** Lines where `type` is not `"user"` or `"assistant"` are always skipped. **wllr format:** Lines where `type` is not `"session"` or `"message"` are always skipped (e.g. `tool_call` lines are skipped).
6. Lines where `role` is `"system"` are always skipped in both formats.
7. The scanner buffer size is 1MB to handle large assistant messages.
8. Offset state is written atomically (write to `.tmp`, rename).
9. For each `ingestFile` batch, file paths from `Read`, `Write`, and `Edit` tool_use blocks in assistant messages are accumulated per session and stored as a single compact L5 memory ("Files touched: ...") with attrs `source=watcher`, `session=<id>`, `repo=<go-module-path>`. This applies only to Claude-format files (wllr uses a different tool call schema).
10. `RepoForPath` resolves the Go module path by walking up to the nearest `.git` directory and reading `go.mod`. Returns `""` if no git root or `go.mod` is found.
11. The file format (Claude vs wllr) is determined once per file by `detectFormat(path)` — paths containing `"/.wllr/"` use the wllr format; all other paths use the Claude format.
12. For wllr format: the `cwd` is sourced from the `session`-type header line and carried forward to all subsequent message lines in the same file via the `carriedCWD` variable. If no session header is found before a message line, `cwd` is the empty string.
13. `ParseFilePaths` is only called for Claude-format files. wllr-format files use a structurally different tool call schema and do not produce "files touched" memories.
