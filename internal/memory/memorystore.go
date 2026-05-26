package memory

import (
	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/vector"
	"sync"
)

type MemoryStore struct {
	db        *db.DB
	emb       vector.Embedder
	llm       llm.LLM
	graph     *graph.Graph
	cfg       *config.Config
	wg        sync.WaitGroup
	enrichSem chan struct {
	}
}
