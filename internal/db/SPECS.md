# internal/db — Invariants

1. WAL mode is always enabled (`journal_mode=WAL`); `Open` fails if WAL cannot be set. After opening the connection, `Open` queries `PRAGMA journal_mode` and returns an error if the mode is not `"wal"`.
2. Foreign key enforcement is always on (`foreign_keys=1`).
3. `SetMaxOpenConns(1)` is always called on the connection pool to serialize writes.
4. `vec0` virtual table rowid maps 1:1 to the implicit `ROWID` of the `memories` table.
5. FTS5 index is kept in sync with `memories.content` via INSERT/DELETE/UPDATE triggers on `memories`.
6. Soft delete means setting `compacted_at` to a non-NULL timestamp; rows are never hard-deleted.
7. All SQL string literals live in `internal/db`; no other package contains SQL.
8. `InsertMemory` atomically inserts into both `memories` and `memories_vec` within a single call.
9. `GetMemory` and `GetByHash` return `fs.ErrNotExist` (wrapped) when no row is found; all other errors propagate as-is.
10. The `db_metadata` table records `schema_version='1'` on first open. `Open` also calls `migrateSchema`, which applies idempotent ALTER TABLE migrations (ignoring "duplicate column name" errors).
11. `valence` is stored as REAL DEFAULT 0.0 CHECK(-1.0 <= valence <= 1.0); ALTER TABLE migration in `migrateSchema` makes this backward-compatible with existing databases.
12. `valence_scored` is a BOOLEAN DEFAULT 0; it is set to 1 by `UpdateValence`. A valence of 0.0 with `valence_scored=0` means "not yet evaluated"; 0.0 with `valence_scored=1` means "genuinely neutral".
13. `ListUnscored` returns memories where `valence_scored=0 AND compacted_at IS NULL`, ordered by `created_at ASC`, up to a caller-supplied limit. Used by the backfill goroutine.
