# internal/db — Design Notes

## 1. modernc.org/sqlite for Pure-Go SQLite

*Added: 2026-05-14*

**Decision:** Use `modernc.org/sqlite` (ccgo-transpiled) with a blank import of `modernc.org/sqlite/vec`
to enable the vec0 virtual table extension.

**Rationale:** The project requires no CGO. `modernc.org/sqlite` is a pure-Go transpilation of
the SQLite C source. The `sqlite-vec` extension (vec0) is transpiled in the same module subpackage.
Both are already in the local module cache.

**Consequence:** `import _ "modernc.org/sqlite/vec"` must appear in the package that calls
`sql.Open("sqlite", ...)` for vec0 to be registered.

---

## 2. vec0 Rowid Mapping Strategy

*Added: 2026-05-14*

**Decision:** Use SQLite's implicit ROWID for the `memories` table. After `INSERT INTO memories`,
capture `LastInsertId()` and use it as the rowid for `INSERT INTO memories_vec(rowid, embedding)`.

**Rationale:** The `memories` table uses a TEXT UUID as the application-level primary key for
globally unique IDs. SQLite's implicit ROWID is a compact integer that vec0 can use for KNN
joins without storing a duplicate UUID in the vec table.

**Consequence:** The join between KNN results and `memories` is `WHERE m.rowid = mv.rowid`.
The implicit rowid is stable as long as no row is deleted (only soft-deleted via `compacted_at`).

---

## 4. Migration Stub: Schema Version Recorded but Not Enforced

*Added: 2026-05-14*

**Decision:** `createSchema` inserts `schema_version='1'` into `db_metadata` via `INSERT OR IGNORE` but never reads the stored version and applies no migration logic.

**Rationale:** v1 has a single schema revision. Implementing migration infrastructure before it is needed adds complexity without benefit.

**Consequence:** Any future schema change requires implementing migration logic in `Open`/`createSchema` before shipping. Existing databases upgraded without migration will silently remain on the old schema. SPECS.md invariant 10 has been updated to reflect this accurately.

---

## 5. Valence Column Migration Strategy

*Added: 2026-05-14*

**Decision:** Add `valence` and `valence_scored` columns via idempotent `ALTER TABLE ADD COLUMN` migrations in `migrateSchema`, called from `createSchema` on every `Open`.

**Rationale:** Existing databases opened against the new binary should not fail. SQLite's `ALTER TABLE ADD COLUMN` is safe to run multiple times — it returns "duplicate column name" which we ignore. The column has a DEFAULT so all existing rows get 0.0/false without a data migration.

**Consequence:** `migrateSchema` is called on every `Open`, adding ~2 SQL statements per open. This is negligible overhead. The `valence_scored` boolean allows distinguishing "not scored" (0.0, false) from "genuinely neutral" (0.0, true).

---

## 3. FTS5 Content Table with Triggers

*Added: 2026-05-14*

**Decision:** Use FTS5 `content=memories` content table with AFTER INSERT/DELETE/UPDATE triggers
to keep the FTS index in sync.

**Rationale:** This is the established pattern from `bob/internal/navigator/store`. FTS5 content
tables provide full-text search without duplicating the content column, but require manual trigger
maintenance.

**Consequence:** The FTS index is invalidated if `memories` rows are modified outside of the
trigger path. All writes to `memories.content` must go through the standard INSERT/UPDATE path.

---

## 5. Stop Dual-Storing Embeddings (vec0 as Sole Authority)

*Added: 2026-06-08*

**Decision:** The embedding for each memory is stored ONLY in `memories_vec` (the vec0 virtual
table). The `memories.embedding` BLOB column is always written as NULL. Reads transparently fall
back to vec0 via a join keyed by `id`.

**Rationale:** A 488 MB production database audit found ~180 MB of duplicated embedding bytes
(3 KB per row × ~60k rows) split between the BLOB column and the vec0 chunk store. The vec0
table is required regardless (KNN search uses it via `mv.embedding MATCH ?`), so the BLOB column
was the redundant copy. Empirical roundtrip test (TestVec0Roundtrip) confirms vec0 returns
embeddings byte-for-byte identical to what was inserted — no quantization, no precision loss.

**Consequences:**
1. `InsertMemory` and `InsertMemoryBatch` now write `NULL` to `memories.embedding` and put the
   vector only in `memories_vec`.
2. `UpdateEmbedding` (used by `BackfillEmbeddings`) now writes to vec0 instead of the BLOB.
   Previously it only wrote the BLOB — a latent bug that made backfilled embeddings invisible
   to vector search.
3. Scan helpers transparently fall back to vec0 when the BLOB is NULL, so all callers
   (compactor, search, sync export) continue to see `m.Embedding` populated.
4. `ListUnembedded` now checks `memories_vec` membership by rowid rather than the BLOB length.
5. A migration (`migrateSchema`) NULLs out the BLOB for all rows already present in vec0 on
   first Open after upgrade. Rows missing from vec0 keep their BLOB so no embedding is lost.
6. `lth maint vacuum` is needed to reclaim the freed disk space.

**Note on vec0 upsert:** `memories_vec` does NOT support `ON CONFLICT` UPSERT clauses or
`INSERT OR REPLACE`. Both `UpdateEmbedding` and `InsertMemoryBatch` emulate upsert via
UPDATE-then-INSERT inside a single transaction. The previous code used `ON CONFLICT(rowid)
DO UPDATE`, which silently failed at runtime — another latent bug fixed by this change.

**Note on lazy vec0 creation:** When `Open(embedDim=0)` is used (typically in tests), the
vec0 table is created lazily on the first embedding write. The dim is cached on the `DB`
struct so we don't take a SQLite reserved lock on every subsequent `UpdateEmbedding` call —
critical for throughput during embedding backfill.
