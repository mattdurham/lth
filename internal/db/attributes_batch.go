// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

// GetAttributesBatch fetches attributes for multiple memory IDs in one query.
// Returns map[memoryID]map[key]value. Every requested ID has an entry (empty map if no attrs).
func (d *DB) GetAttributesBatch(ctx context.Context, ids []string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(ids))
	for _, id := range ids {
		result[id] = make(map[string]string)
	}
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	//nolint:gosec // placeholder count matches ids length, no injection possible
	query := fmt.Sprintf("SELECT mem_id, key, value FROM memory_attributes WHERE mem_id IN (%s)", placeholders)
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attributes batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var memID, key, val string
		if err := rows.Scan(&memID, &key, &val); err != nil {
			return nil, fmt.Errorf("scan attribute row: %w", err)
		}
		if m, ok := result[memID]; ok {
			m[key] = val
		}
	}
	return result, rows.Err()
}
