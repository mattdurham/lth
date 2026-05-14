// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
)

// Stats returns aggregate statistics about the memory store.
func (d *DB) Stats(ctx context.Context) (*StatsRow, error) {
	stats := &StatsRow{
		ByLayer: make(map[int]int),
	}

	// Total active memories.
	if err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM memories WHERE compacted_at IS NULL",
	).Scan(&stats.TotalMemories); err != nil {
		return nil, fmt.Errorf("count total memories: %w", err)
	}

	// Per-layer counts.
	rows, err := d.db.QueryContext(ctx,
		"SELECT layer, COUNT(*) FROM memories WHERE compacted_at IS NULL GROUP BY layer")
	if err != nil {
		return nil, fmt.Errorf("count by layer: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var layer, count int
		if err := rows.Scan(&layer, &count); err != nil {
			return nil, fmt.Errorf("scan layer count: %w", err)
		}
		stats.ByLayer[layer] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("layer count rows: %w", err)
	}

	// Total edges.
	if err := d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM memory_edges",
	).Scan(&stats.TotalEdges); err != nil {
		return nil, fmt.Errorf("count total edges: %w", err)
	}

	return stats, nil
}
