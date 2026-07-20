// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

// attributesBatchMaxIDs caps how many IDs go into a single "IN (...)" query.
// SQLite's bound-parameter limit (SQLITE_MAX_VARIABLE_NUMBER) is 32766 on
// modern builds but was 999 before SQLite 3.32.0 (2020) -- modernc.org/sqlite
// tracks upstream SQLite, so the effective limit depends on the exact version
// vendored. 900 stays safely under either bound without needing to detect
// which one applies, at the cost of more (still cheap, indexed) queries for
// very large ID lists.
const attributesBatchMaxIDs = 900

// GetAttributesBatch fetches attributes for multiple memory IDs, chunking the
// underlying query at attributesBatchMaxIDs so a large ids slice (e.g. every
// row in a large layer) can never exceed SQLite's bound-parameter limit.
// Returns map[memoryID]map[key]value. Every requested ID has an entry (empty map if no attrs).
func (d *DB) GetAttributesBatch(ctx context.Context, ids []string) (map[string]map[string]string, error) {
	result := make(map[string]map[string]string, len(ids))
	for _, id := range ids {
		result[id] = make(map[string]string)
	}
	for start := 0; start < len(ids); start += attributesBatchMaxIDs {
		end := min(start+attributesBatchMaxIDs, len(ids))
		if err := d.getAttributesBatchChunk(ctx, ids[start:end], result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// getAttributesBatchChunk queries attributes for one chunk of ids (must be
// non-empty and within attributesBatchMaxIDs) and merges them into result.
func (d *DB) getAttributesBatchChunk(ctx context.Context, ids []string, result map[string]map[string]string) error {
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
		return fmt.Errorf("query attributes batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var memID, key, val string
		if err := rows.Scan(&memID, &key, &val); err != nil {
			return fmt.Errorf("scan attribute row: %w", err)
		}
		if m, ok := result[memID]; ok {
			m[key] = val
		}
	}
	return rows.Err()
}
