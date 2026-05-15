# internal/watcher — Invariants

1. The watcher never deletes or modifies watched files — read-only access only.
2. Deduplication via content hash: duplicate JSONL entries are silently skipped (delegated to `memory.Store`).
3. The byte offset is persisted after each file ingest so a daemon restart only processes new bytes.
4. Tool_result content blocks exceeding 10KB are always skipped regardless of other content in the same message.
5. Lines where `type` is not `"user"` or `"assistant"` are always skipped.
6. Lines where `message.role` is `"system"` are always skipped.
7. The scanner buffer size is 1MB to handle large assistant messages.
8. Offset state is written atomically (write to `.tmp`, rename).
9. For each `ingestFile` batch, file paths from `Read`, `Write`, and `Edit` tool_use blocks in assistant messages are accumulated per session and stored as a single compact L5 memory ("Files touched: ...") with attrs `source=watcher`, `session=<id>`, `repo=<go-module-path>`.
10. `RepoForPath` resolves the Go module path by walking up to the nearest `.git` directory and reading `go.mod`. Returns `""` if no git root or `go.mod` is found.
