// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

const schemaBase = `
CREATE TABLE IF NOT EXISTS memories (
	id               TEXT PRIMARY KEY,
	layer            INTEGER NOT NULL,
	content          TEXT    NOT NULL,
	content_hash     TEXT    UNIQUE NOT NULL,
	embedding        BLOB,
	importance       REAL    NOT NULL DEFAULT 5.0,
	access_count     INTEGER NOT NULL DEFAULT 0,
	created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_accessed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	decay_rate       REAL    NOT NULL DEFAULT 0.0,
	stability        REAL    NOT NULL DEFAULT 1.0,
	source           TEXT    NOT NULL DEFAULT '',
	agent            TEXT    NOT NULL DEFAULT '',
	compacted_at     DATETIME,
	valence          REAL    NOT NULL DEFAULT 0.0 CHECK(valence >= -1.0 AND valence <= 1.0),
	valence_scored   BOOLEAN NOT NULL DEFAULT 0,
	valence_attempts INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_memories_layer       ON memories(layer);
-- Note: no explicit index on content_hash — the column's UNIQUE constraint already
-- creates sqlite_autoindex_memories_2 which serves all content_hash lookups.
-- A migration drops the legacy idx_memories_content_hash if it exists from an older schema.
CREATE INDEX IF NOT EXISTS idx_memories_compacted_at ON memories(compacted_at);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
	content,
	content='memories',
	content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS memories_fts_ai AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_ad AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;

CREATE TRIGGER IF NOT EXISTS memories_fts_au AFTER UPDATE OF content ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
	INSERT INTO memories_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TABLE IF NOT EXISTS memory_attributes (
	mem_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
	key    TEXT NOT NULL,
	value  TEXT NOT NULL,
	PRIMARY KEY (mem_id, key)
);

CREATE TABLE IF NOT EXISTS memory_edges (
	from_id    TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
	to_id      TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
	edge_type  TEXT NOT NULL,
	weight     REAL NOT NULL DEFAULT 1.0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (from_id, to_id, edge_type)
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON memory_edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to   ON memory_edges(to_id);

CREATE TABLE IF NOT EXISTS compaction_log (
	id           TEXT PRIMARY KEY,
	run_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	path         TEXT NOT NULL,
	source_layer INTEGER NOT NULL,
	target_layer INTEGER NOT NULL,
	source_ids   TEXT NOT NULL,
	target_id    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS db_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

const schemaVec = `
CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
	embedding float[%d]
);
`

func (d *DB) createSchema(embedDim int) error {
	if _, err := d.db.Exec(schemaBase); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}
	if embedDim > 0 {
		if _, err := d.db.Exec(fmt.Sprintf(schemaVec, embedDim)); err != nil {
			return fmt.Errorf("exec vec schema: %w", err)
		}
	}
	// Record schema version in db_metadata.
	if _, err := d.db.Exec(
		`INSERT OR IGNORE INTO db_metadata(key, value) VALUES ('schema_version', '1')`,
	); err != nil {
		return fmt.Errorf("insert schema version: %w", err)
	}
	return d.migrateSchema()
}

// migrateSchema applies idempotent ALTER TABLE migrations for new columns added after v1.
// Each migration ignores "duplicate column name" errors, making it safe to run on every Open.
func (d *DB) migrateSchema() error {
	ctx := context.Background()
	migrations := []struct {
		sql  string
		name string
	}{
		{
			sql:  `ALTER TABLE memories ADD COLUMN valence REAL NOT NULL DEFAULT 0.0`,
			name: "valence column",
		},
		{
			sql:  `ALTER TABLE memories ADD COLUMN valence_scored BOOLEAN NOT NULL DEFAULT 0`,
			name: "valence_scored column",
		},
		{
			sql:  `CREATE INDEX IF NOT EXISTS idx_attrs_key_value ON memory_attributes(key, value)`,
			name: "idx_attrs_key_value index",
		},
		{
			sql:  `ALTER TABLE memories ADD COLUMN embedding_model TEXT NOT NULL DEFAULT ''`,
			name: "embedding_model column",
		},
		{
			sql:  `ALTER TABLE memories ADD COLUMN pushed_at DATETIME`,
			name: "pushed_at column",
		},
		{
			// The UNIQUE constraint on memories.content_hash creates an autoindex
			// that fully serves equality lookups. An explicit secondary index on the
			// same column was created by older schemas and is pure duplicate storage
			// (~6 MB on a 70k-row database). Drop it.
			sql:  `DROP INDEX IF EXISTS idx_memories_content_hash`,
			name: "drop redundant idx_memories_content_hash",
		},
		{
			// Drop the synthetic memory_edges.id PRIMARY KEY (UUID string) and use
			// the natural composite key (from_id, to_id, edge_type) as the PK
			// instead. The synthetic id was never queried by application code — it
			// only served as a PK — and cost ~4 MB of duplicate storage (the table
			// already had a UNIQUE constraint on the natural key that created an
			// autoindex). The migration rebuilds the table because SQLite cannot
			// drop a PRIMARY KEY column via ALTER TABLE.
			//
			// Detection: check whether the 'id' column still exists in PRAGMA
			// table_info before rebuilding. Re-running on an already-migrated
			// schema is a no-op.
			sql:  `-- handled in Go via rebuildMemoryEdgesTable`,
			name: "rebuild memory_edges without synthetic id",
		},
		{
			// Stop dual-storing embeddings. memories_vec is the authoritative store;
			// the memories.embedding BLOB column was holding a redundant copy that
			// accounted for ~180 MB on a 70k-row database (3 KB per row, 60k rows).
			// NULL the BLOB only for rows already present in vec0 — rows missing from
			// vec0 keep their BLOB so scan-fallback continues to work and no embedding
			// is lost. Reclaim disk space via `lth maint vacuum` after migration.
			sql:  `UPDATE memories SET embedding = NULL WHERE embedding IS NOT NULL AND rowid IN (SELECT rowid FROM memories_vec)`,
			name: "null out memories.embedding for vec0-present rows",
		},
		{
			sql:  `ALTER TABLE memories ADD COLUMN valence_attempts INTEGER NOT NULL DEFAULT 0`,
			name: "valence_attempts column",
		},
	}
	for _, m := range migrations {
		// The memory_edges rebuild is handled in Go because SQLite can't drop a
		// PRIMARY KEY column via ALTER TABLE — we need a multi-statement table swap.
		if m.name == "rebuild memory_edges without synthetic id" {
			if err := d.rebuildMemoryEdgesTable(ctx); err != nil {
				return fmt.Errorf("migrate %s: %w", m.name, err)
			}
			continue
		}
		// The dual-store-removal migration only applies when memories_vec exists.
		// Open(embedDim=0) does not create memories_vec; in that case there is no
		// embedding data to migrate and the UPDATE would fail with "no such table".
		if m.name == "null out memories.embedding for vec0-present rows" {
			exists, err := d.tableExists(ctx, "memories_vec")
			if err != nil {
				return fmt.Errorf("check memories_vec existence: %w", err)
			}
			if !exists {
				continue
			}
		}
		if _, err := d.db.ExecContext(ctx, m.sql); err != nil {
			// "duplicate column name" means the column already exists — idempotent.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("migrate %s: %w", m.name, err)
		}
	}
	return nil
}

// tableExists returns whether a table with the given name is present in the schema.
// Note: virtual tables (vec0, fts5) appear in sqlite_master with type='table'.
func (d *DB) tableExists(ctx context.Context, name string) (bool, error) {
	var n int
	err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	return n > 0, err
}
