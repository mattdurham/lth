// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"sync"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/vector"
)

// MemoryStore implements the Store interface using SQLite and the internal packages.
type MemoryStore struct {
	db     *db.DB
	emb    vector.Embedder
	llm    llm.LLM
	graph  *graph.Graph
	cfg    *config.Config
	wg     sync.WaitGroup // tracks in-flight async goroutines
}

// Compile-time interface check.
var _ Store = (*MemoryStore)(nil)
