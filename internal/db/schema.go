// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

const schema = `
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
	valence_scored   BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_memories_layer       ON memories(layer);
CREATE INDEX IF NOT EXISTS idx_memories_content_hash ON memories(content_hash);
CREATE INDEX IF NOT EXISTS idx_memories_compacted_at ON memories(compacted_at);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_vec USING vec0(
	embedding float[768]
);

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
	id         TEXT PRIMARY KEY,
	from_id    TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
	to_id      TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
	edge_type  TEXT NOT NULL,
	weight     REAL NOT NULL DEFAULT 1.0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(from_id, to_id, edge_type)
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

func (d *DB) createSchema() error {
	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("exec schema: %w", err)
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
	}
	for _, m := range migrations {
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
