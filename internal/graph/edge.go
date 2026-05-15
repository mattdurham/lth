// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package graph

import "time"

// Edge represents a typed, weighted connection between two memories.
type Edge struct {
	ID       string
	FromID   string
	ToID     string
	EdgeType string // "relates_to" | "contradicts" | "supports" | "derived_from" | "compacted_from"
	Weight   float32
	Created  time.Time
}
