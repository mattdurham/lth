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
