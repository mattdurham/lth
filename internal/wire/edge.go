// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package wire

import "time"

// ExportEdge is the wire format struct for a memory edge in ZIP+JSONL export/import.
type ExportEdge struct {
	ID        string    `json:"id"`
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	EdgeType  string    `json:"edge_type"`
	Weight    float32   `json:"weight"`
	CreatedAt time.Time `json:"created_at"`
}
