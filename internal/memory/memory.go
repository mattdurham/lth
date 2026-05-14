// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package memory provides the core application logic: Memory type, Store/Search/Get operations.
package memory

import "time"

// Memory is the application-level representation of a stored memory.
type Memory struct {
	ID             string
	Layer          int
	Content        string
	ContentHash    string
	Embedding      []float32
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
	Attrs          map[string]string
}
