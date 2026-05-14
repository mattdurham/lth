# internal/db — Test Scenarios

## TestOpen

**Scenario:** Open a fresh database.

**Setup:** Create temp dir; call `Open(filepath.Join(dir, "test.db"))`.

**Assertions:**
- No error
- Query `sqlite_master` — tables present: memories, memories_vec, memories_fts, memory_attributes, memory_edges, compaction_log, db_metadata
- WAL mode enabled: `PRAGMA journal_mode` returns "wal"
- Foreign keys enabled: `PRAGMA foreign_keys` returns 1

## TestMemoryInsertGet

**Scenario:** Insert a memory row and retrieve it by ID and by hash.

**Setup:** `testDB(t)`; call `InsertMemory`.

**Assertions:**
- `GetMemory(id)` returns same fields as inserted
- `GetByHash(hash)` returns same row
- Missing ID returns wrapped `fs.ErrNotExist`

## TestMarkAccessed

**Scenario:** Call `MarkAccessed` twice; verify access_count increments.

**Setup:** Insert memory; call `MarkAccessed` twice.

**Assertions:** `GetMemory` shows `access_count == 2`.

## TestSoftDelete

**Scenario:** SoftDelete a memory; verify it is excluded from active queries.

**Setup:** Insert memory; call `SoftDelete`; call `ListLayer(layer, activeOnly=true)`.

**Assertions:** Soft-deleted memory not in result. `GetMemory` still returns it (tombstone exists).

## TestVectorSearch

**Scenario:** Insert memories with known embeddings; KNN search returns nearest.

**Setup:** Insert 3 memories with distinct embeddings; query for embedding closest to memory[0].

**Assertions:** `VectorSearch` returns memory[0] as first result.

## TestFTSSearch

**Scenario:** Insert memory with content "golang programming"; search for "golang".

**Setup:** Insert memory; call `FTSSearch("golang", layers, limit)`.

**Assertions:** The inserted memory appears in results.

## TestEdgeCRUD

**Scenario:** Insert an edge; retrieve neighbors.

**Setup:** Insert two memories; insert an edge between them; call `GetEdges`.

**Assertions:** Edge is returned with correct fields; `GetNeighbors` returns the neighbor ID.

## TestAttributes

**Scenario:** Set and get attributes for a memory.

**Setup:** Insert memory; call `SetAttributes`; call `GetAttributes`.

**Assertions:** Returned map matches set map.