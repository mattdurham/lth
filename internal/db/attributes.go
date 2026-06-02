// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"database/sql"
	"fmt"
)

// SetAttributes replaces all attributes for the given memory ID with the provided map.
// Existing attributes are deleted and new ones are inserted atomically.
func (d *DB) SetAttributes(ctx context.Context, memID string, attrs map[string]string) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM memory_attributes WHERE mem_id = ?", memID,
		); err != nil {
			return fmt.Errorf("delete attributes: %w", err)
		}
		for k, v := range attrs {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO memory_attributes(mem_id, key, value) VALUES (?, ?, ?)", memID, k, v,
			); err != nil {
				return fmt.Errorf("insert attribute %q: %w", k, err)
			}
		}
		return nil
	})
}

// MergeAttribute upserts a single key=value attribute without touching other attrs.
func (d *DB) MergeAttribute(ctx context.Context, memID, key, value string) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO memory_attributes(mem_id, key, value) VALUES (?, ?, ?) ON CONFLICT(mem_id, key) DO UPDATE SET value=excluded.value",
		memID, key, value)
	if err != nil {
		return fmt.Errorf("merge attribute %q: %w", key, err)
	}
	return nil
}

// GetMemIDsByAttr returns all memory IDs that have the given key=value attribute.
func (d *DB) GetMemIDsByAttr(ctx context.Context, key, value string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT DISTINCT mem_id FROM memory_attributes WHERE key = ? AND value = ?", key, value)
	if err != nil {
		return nil, fmt.Errorf("get mem ids by attr: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan mem id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DistinctAttrValues returns all distinct values stored for the given attribute key,
// ordered alphabetically. Used by lth projects and filter hint generation.
func (d *DB) DistinctAttrValues(ctx context.Context, key string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT DISTINCT value FROM memory_attributes WHERE key = ? ORDER BY value", key)
	if err != nil {
		return nil, fmt.Errorf("distinct attr values: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var vals []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan value: %w", err)
		}
		vals = append(vals, v)
	}
	return vals, rows.Err()
}

// GetAttributes returns all attributes for the given memory ID as a map.
func (d *DB) GetAttributes(ctx context.Context, memID string) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT key, value FROM memory_attributes WHERE mem_id = ?", memID)
	if err != nil {
		return nil, fmt.Errorf("get attributes: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan attribute: %w", err)
		}
		result[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get attributes rows: %w", err)
	}
	return result, nil
}
