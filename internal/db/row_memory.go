// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

import "time"

// MemoryRow is a flat struct matching the columns of the memories table.
type MemoryRow struct {
	ID             string
	Layer          int
	Content        string
	ContentHash    string
	Embedding      []byte // raw IEEE 754 little-endian float32 bytes; may be nil
	Importance     float32
	AccessCount    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastAccessedAt time.Time
	DecayRate      float32
	Stability      float32
	Source         string
	Agent          string
	CompactedAt    *time.Time
}
