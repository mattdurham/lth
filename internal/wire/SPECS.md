# internal/wire — Invariants

1. ExportMemory, ExportEdge, ExportManifest, ExportMetadata are the canonical wire format types for ZIP+JSONL archives.
2. These types mirror the JSON structure produced by `lth export` and consumed by `lth import`.
3. Both cmd/lth and cmd/lth-server must import from internal/wire — no duplication of these types in package main.
4. Field names and json tags are stable; any change is a breaking wire format change and requires a NOTES.md entry.
5. EmbeddingModel is present in ExportMemory (added for server sync); cmd/lth ignores it via omitempty when not set.
6. One public struct per file. No logic in this package — pure data types only.
