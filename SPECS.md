# lth — Project-Level Invariants

## Overview

`lth` is a 5-layer hierarchical memory database for AI agents. It stores, retrieves, and
automatically promotes memories through layers using LLM-based compaction.

## Invariants

1. **No CGO**: The entire project is pure Go. All SQLite operations use `modernc.org/sqlite`
   (ccgo-transpiled, no CGO). Vector search uses `modernc.org/sqlite/vec` (same module).

2. **5-layer hierarchy**: Memories have layers L1–L5 where L1 is most abstract/permanent and
   L5 is most ephemeral/recent. Layer determines decay rate and compaction eligibility.

3. **Soft-delete only**: Rows in the `memories` table are never hard-deleted. Compaction sets
   `compacted_at` timestamp. All queries must filter `WHERE compacted_at IS NULL` for active memories.

4. **Content-hash deduplication**: Storing identical content is idempotent. The second store
   returns the existing memory without creating a new row.

5. **WAL mode required**: All SQLite connections must use `journal_mode=WAL` and `foreign_keys=1`.
   The daemon and CLI share the same DB file via WAL cross-process concurrency.

6. **Daemon auto-start**: Every CLI command that touches the database automatically starts the
   background daemon if it is not running. The daemon handles JSONL watching and compaction scheduling.

7. **Context propagation**: All I/O methods accept `context.Context` as the first argument
   and respect cancellation.

8. **One public struct per file**: Per `revive: max-public-structs: [1]` configuration.

9. **NOTE invariant on all .go files**: Every `.go` file begins with:
   `// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.`
