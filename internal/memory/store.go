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

// enrichSem limits concurrent enrichAsync goroutines to avoid overwhelming
// the embedding server and LLM API during bulk ingestion.
const enrichConcurrency = 3

// MemoryStore implements the Store interface using SQLite and the internal packages.
type MemoryStore struct {
	db       *db.DB
	emb      vector.Embedder
	llm      llm.LLM
	graph    *graph.Graph
	cfg      *config.Config
	wg       sync.WaitGroup // tracks in-flight async goroutines
	enrichSem chan struct{}  // semaphore limiting concurrent enrichAsync calls
}

// Compile-time interface check.
var _ Store = (*MemoryStore)(nil)
