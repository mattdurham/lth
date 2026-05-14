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
