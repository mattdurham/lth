# report — Specifications

## Invariants

1. `Writer` opens files in O_APPEND|O_CREATE|O_WRONLY mode — it never truncates existing content.
2. Each call to `AppendResult` writes exactly one JSON line followed by a newline character.
3. `LoadCompleted` returns an empty map (not an error) when the file does not exist.
4. The key format used by `LoadCompleted` is `"{instance_id}:{approach}"` — callers must use the same format for deduplication.
5. `PrintSummary` writes to the provided `io.Writer`; it never writes to stdout directly.
