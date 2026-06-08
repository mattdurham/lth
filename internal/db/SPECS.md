# internal/db — Invariants

1. WAL mode is always enabled (`journal_mode=WAL`); `Open` fails if WAL cannot be set. After opening the connection, `Open` queries `PRAGMA journal_mode` and returns an error if the mode is not `"wal"`.
2. Foreign key enforcement is always on (`foreign_keys=1`).
3. `SetMaxOpenConns(1)` is always called on the connection pool to serialize writes.
4. `vec0` virtual table rowid maps 1:1 to the implicit `ROWID` of the `memories` table.
5. FTS5 index is kept in sync with `memories.content` via INSERT/DELETE/UPDATE triggers on `memories`.
6. Soft delete means setting `compacted_at` to a non-NULL timestamp; rows are never hard-deleted.
7. All SQL string literals live in `internal/db`; no other package contains SQL.
8. `InsertMemory` writes the embedding ONLY to `memories_vec`; `memories.embedding` is always written as NULL. `memories_vec` is the authoritative store for embedding vectors. The `memories.embedding` BLOB column is retained for backward-compatibility with older databases (where the BLOB was the source of truth) — scan helpers populate `m.Embedding` from the BLOB if it is non-NULL, falling back to `memories_vec` otherwise. Prior to this change, embeddings were dual-stored, wasting ~3 KB per row (≈180 MB on a 70k-row DB).
8a. `vec0` does NOT support `ON CONFLICT` UPSERT or `INSERT OR REPLACE`. Upserts into `memories_vec` are emulated as UPDATE-then-INSERT inside a transaction (see `UpdateEmbedding` and `InsertMemoryBatch`).
8b. `ensureVecTable(ctx, dim)` is the single entry point for lazy creation of `memories_vec` when `Open(embedDim=0)` was used. It caches the dim on the `DB` struct so subsequent calls become a mutex-protected equality check, avoiding the SQLite reserved lock that `CREATE TABLE IF NOT EXISTS` would otherwise take on every call.
8c. `UpdateEmbedding(id, embedding, model)` upserts into `memories_vec` (via UPDATE-then-INSERT) and sets `memories.embedding = NULL, memories.embedding_model = ?`. It returns an error if `id` does not exist. Used by `BackfillEmbeddings` to populate embeddings for rows that were inserted without one or whose embedding model differs from the current one.
8d. `scanMemoryRow` / `scanMemoryRows` populate `MemoryRow.Embedding` transparently: BLOB if non-NULL, otherwise via vec0 JOIN by id. The batch variant issues exactly one extra query for all NULL-BLOB rows at the end of the scan.
9. `GetMemory` and `GetByHash` return `fs.ErrNotExist` (wrapped) when no row is found; all other errors propagate as-is.
10. The `db_metadata` table records `schema_version='1'` on first open. `Open` also calls `migrateSchema`, which applies idempotent ALTER TABLE migrations (ignoring "duplicate column name" errors).
11. `valence` is stored as REAL DEFAULT 0.0 CHECK(-1.0 <= valence <= 1.0); ALTER TABLE migration in `migrateSchema` makes this backward-compatible with existing databases.
12. `valence_scored` is a BOOLEAN DEFAULT 0; it is set to 1 by `UpdateValence`. A valence of 0.0 with `valence_scored=0` means "not yet evaluated"; 0.0 with `valence_scored=1` means "genuinely neutral".
13. `ListUnscored` returns memories where `valence_scored=0 AND compacted_at IS NULL`, ordered by `created_at ASC`, up to a caller-supplied limit. Used by the backfill goroutine.
13a. `ListUnembedded` returns active memories that need embedding. It checks `memories_vec` membership by rowid (NOT the legacy `memories.embedding` BLOB column), because the column is always NULL for vec0-present rows after the dual-store-removal migration.
14. No explicit secondary index on `memories.content_hash` — the column's `UNIQUE` constraint already creates an autoindex that fully serves equality lookups. A migration drops the legacy `idx_memories_content_hash` (created by earlier schema versions) if it exists; the duplicate index wasted ~6 MB on a 70k-row database.
15. `WALCheckpointTruncate(ctx)` runs `PRAGMA wal_checkpoint(TRUNCATE)` and returns `(walPages, checkpointed, err)`. On success the `.db-wal` sidecar is truncated to zero bytes on disk. If the operation was downgraded due to active readers/writers (`busy != 0`), it returns a non-nil error AND the partial result so callers can log progress.
16. `Vacuum(ctx)` runs `VACUUM` and returns `(beforeBytes, afterBytes, err)`. Must run outside of any open transaction. Acquires an exclusive lock for the duration and uses roughly 2x the current DB size as transient disk space.
