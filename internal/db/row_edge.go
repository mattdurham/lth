// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import "time"

// EdgeRow is a flat struct matching the columns of the memory_edges table.
type EdgeRow struct {
	ID        string
	FromID    string
	ToID      string
	EdgeType  string
	Weight    float32
	CreatedAt time.Time
}
