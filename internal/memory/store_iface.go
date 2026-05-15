// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import "context"

// Store is the public interface for the memory system.
// Compactor and watcher depend on this interface, not on MemoryStore directly.
type Store interface {
	Store(ctx context.Context, layer int, content string, attrs map[string]string) (*Memory, error)
	Get(ctx context.Context, id string) (*Memory, error)
	Search(ctx context.Context, req *SearchRequest) ([]*ScoredMemory, error)
	Stats(ctx context.Context) (*Stats, error)
	ListLayer(ctx context.Context, layer int) ([]*Memory, error)
	SoftDelete(ctx context.Context, ids []string, reason string) error
}
