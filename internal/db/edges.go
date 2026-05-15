// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import (
	"context"
	"fmt"
	"strings"
)

// InsertEdge inserts a new edge row into the memory_edges table.
// It silently ignores duplicate edges (same from_id, to_id, edge_type).
func (d *DB) InsertEdge(ctx context.Context, e *EdgeRow) error {
	_, err := d.db.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_edges (id, from_id, to_id, edge_type, weight, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.FromID, e.ToID, e.EdgeType, e.Weight, e.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert edge: %w", err)
	}
	return nil
}

// GetAllEdges returns all edges in the memory_edges table.
// Used by graph.LoadAll to populate the in-memory adjacency cache.
func (d *DB) GetAllEdges(ctx context.Context) ([]*EdgeRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, from_id, to_id, edge_type, weight, created_at FROM memory_edges`)
	if err != nil {
		return nil, fmt.Errorf("get all edges: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var result []*EdgeRow
	for rows.Next() {
		e := &EdgeRow{}
		if err := rows.Scan(&e.ID, &e.FromID, &e.ToID, &e.EdgeType, &e.Weight, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get all edges rows: %w", err)
	}
	return result, nil
}

// GetEdges returns all edges where from_id matches the given memory ID.
func (d *DB) GetEdges(ctx context.Context, fromID string) ([]*EdgeRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT id, from_id, to_id, edge_type, weight, created_at
FROM memory_edges WHERE from_id = ?`, fromID)
	if err != nil {
		return nil, fmt.Errorf("get edges: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var result []*EdgeRow
	for rows.Next() {
		e := &EdgeRow{}
		if err := rows.Scan(&e.ID, &e.FromID, &e.ToID, &e.EdgeType, &e.Weight, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get edges rows: %w", err)
	}
	return result, nil
}

// GetNeighbors returns the IDs of all neighbors connected by the given edge types.
// It traverses in both directions (from_id = id OR to_id = id).
// If edgeTypes is empty, all edge types are returned.
func (d *DB) GetNeighbors(ctx context.Context, id string, edgeTypes []string) ([]string, error) {
	query := `
SELECT CASE WHEN from_id = ? THEN to_id ELSE from_id END AS neighbor_id
FROM memory_edges
WHERE (from_id = ? OR to_id = ?)`

	args := []any{id, id, id}

	if len(edgeTypes) > 0 {
		placeholders := make([]string, len(edgeTypes))
		for i, et := range edgeTypes {
			placeholders[i] = "?"
			args = append(args, et)
		}
		//nolint:gosec // edge type values are internal constants, not user input
		query += fmt.Sprintf(" AND edge_type IN (%s)", strings.Join(placeholders, ","))
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get neighbors: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var result []string
	for rows.Next() {
		var neighborID string
		if err := rows.Scan(&neighborID); err != nil {
			return nil, fmt.Errorf("scan neighbor: %w", err)
		}
		result = append(result, neighborID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get neighbors rows: %w", err)
	}
	return result, nil
}

// InsertCompactionLog logs a compaction event.
func (d *DB) InsertCompactionLog(ctx context.Context, log *CompactionLog) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO compaction_log (id, run_at, path, source_layer, target_layer, source_ids, target_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.RunAt.UTC(), log.Path, log.SourceLayer, log.TargetLayer, log.SourceIDs, log.TargetID,
	)
	if err != nil {
		return fmt.Errorf("insert compaction log: %w", err)
	}
	return nil
}
