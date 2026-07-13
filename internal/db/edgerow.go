// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import "time"

// EdgeRow represents one row of memory_edges. The natural composite key
// (FromID, ToID, EdgeType) is the primary key — there is no synthetic id
// column. Two edges with the same triple are considered identical.
type EdgeRow struct {
	FromID    string
	ToID      string
	EdgeType  string
	Weight    float32
	CreatedAt time.Time
}
