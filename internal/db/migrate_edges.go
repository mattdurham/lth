// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
)

// rebuildMemoryEdgesTable drops the synthetic memory_edges.id PRIMARY KEY column
// and replaces it with a composite PK on (from_id, to_id, edge_type). SQLite
// cannot drop a PRIMARY KEY column via ALTER TABLE, so the migration follows the
// standard rebuild pattern:
//
//  1. Detect: skip if the 'id' column is no longer present (idempotent).
//  2. Create memory_edges_new with the desired schema.
//  3. INSERT INTO new SELECT (from_id, to_id, edge_type, weight, created_at) FROM old.
//     The synthetic id is dropped; any natural-key duplicates are coalesced by
//     INSERT OR IGNORE (there shouldn't be any \u2014 the old UNIQUE constraint on
//     the same key already prevented them).
//  4. DROP the old table (this also drops its dependent indexes).
//  5. RENAME memory_edges_new to memory_edges.
//  6. Recreate idx_edges_from and idx_edges_to (they were attached to the old
//     table and dropped along with it).
//
// Runs inside a single transaction so a crash mid-rebuild leaves the original
// table intact.
func (d *DB) rebuildMemoryEdgesTable(ctx context.Context) error {
	hasID, err := d.columnExists(ctx, "memory_edges", "id")
	if err != nil {
		return fmt.Errorf("check memory_edges.id column: %w", err)
	}
	if !hasID {
		return nil // already migrated
	}

	stmts := []string{
		`CREATE TABLE memory_edges_new (
			from_id    TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
			to_id      TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
			edge_type  TEXT NOT NULL,
			weight     REAL NOT NULL DEFAULT 1.0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (from_id, to_id, edge_type)
		)`,
		`INSERT OR IGNORE INTO memory_edges_new (from_id, to_id, edge_type, weight, created_at)
		 SELECT from_id, to_id, edge_type, weight, created_at FROM memory_edges`,
		`DROP TABLE memory_edges`,
		`ALTER TABLE memory_edges_new RENAME TO memory_edges`,
		`CREATE INDEX IF NOT EXISTS idx_edges_from ON memory_edges(from_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_to   ON memory_edges(to_id)`,
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rebuild memory_edges step %q: %w", firstSQLToken(s), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory_edges rebuild: %w", err)
	}
	return nil
}

// columnExists reports whether the given table has a column with the given name.
// Uses PRAGMA table_info (which returns one row per column).
func (d *DB) columnExists(ctx context.Context, table, column string) (bool, error) {
	// PRAGMA table_info doesn't accept parameter binding for the table name, so
	// we build the statement directly. The 'table' value is a fixed constant
	// from this package's migration code \u2014 not user input \u2014 so SQL injection
	// is not a concern.
	//nolint:gosec // table is an internal constant from migration code
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// firstSQLToken returns the first whitespace-delimited word of s, used for
// error messages that identify which migration step failed without quoting
// the entire statement.
func firstSQLToken(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' {
			if i == 0 {
				s = s[1:]
				i = -1
				continue
			}
			return s[:i]
		}
	}
	return s
}
