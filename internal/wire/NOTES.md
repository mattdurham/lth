# internal/wire — Design Notes

## 1. Extracted from cmd/lth package main

*Added: 2026-05-23*

**Decision:** Extract exportMemory, exportEdge, exportManifest, exportMetadata from cmd/lth package main
into internal/wire as exported types ExportMemory, ExportEdge, ExportManifest, ExportMetadata.

**Rationale:** cmd/lth-server needs to read and write ZIP+JSONL archives in the same format as cmd/lth.
Go does not allow importing from a package main binary. Extracting these types to internal/wire allows
both binaries to share a single canonical definition without duplication.

**Consequence:** cmd/lth must be updated to use wire.ExportMemory etc. in its export/import logic.
The JSON field names are unchanged — this is a refactor, not a wire format change.
