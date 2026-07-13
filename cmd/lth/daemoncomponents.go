// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"github.com/mattdurham/lth/internal/compactor"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/vector"
)

type daemonComponents struct {
	store     memory.Store
	ms        *memory.MemoryStore
	compactor *compactor.Compactor
	d         *db.DB
	g         *graph.Graph
	llm       llm.LLM
	emb       vector.Embedder
}
