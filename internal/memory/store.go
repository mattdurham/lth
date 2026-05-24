// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
)

// enrichSem limits concurrent enrichAsync goroutines to avoid overwhelming
// the embedding server and LLM API during bulk ingestion.
const enrichConcurrency = 3

// MemoryStore implements the Store interface using SQLite and the internal packages.

// tracks in-flight async goroutines
// semaphore limiting concurrent enrichAsync calls

// Compile-time interface check.
var _ Store = (*MemoryStore)(nil)

type Store interface {
	Store(ctx context.Context, layer int, content string, attrs map[string]string) (*Memory, error)
	Get(ctx context.Context, id string) (*Memory, error)
	Search(ctx context.Context, req *SearchRequest) ([]*ScoredMemory, error)
	Stats(ctx context.Context) (*Stats, error)
	ListLayer(ctx context.Context, layer int) ([]*Memory, error)
	SoftDelete(ctx context.Context, ids []string, reason string) error
}
