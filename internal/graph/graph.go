// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package graph provides typed edge management, Zettelkasten auto-linking, and PPR traversal.
package graph

import (
	"context"
	"fmt"
	"sync"

	"github.com/mattdurham/lth/internal/db"
)

// adjacency represents a single directed edge in the adjacency cache.

// true = from→neighbor, false = neighbor→from

// Graph maintains an in-memory adjacency cache over the memory_edges SQLite table.
type Graph struct {
	db     *db.DB
	mu     sync.RWMutex
	adj    map[string][]adjacency
	loaded bool
}

// New creates a new Graph wrapping the given DB. Call LoadAll before using Neighbors or PPR.
func New(d *db.DB) *Graph {
	return &Graph{
		db:  d,
		adj: make(map[string][]adjacency),
	}
}

// LoadAll loads all edges from the DB into the in-memory adjacency cache.
// It replaces any existing cache contents.
func (g *Graph) LoadAll(ctx context.Context) error {
	allEdges, err := g.db.GetAllEdges(ctx)
	if err != nil {
		return fmt.Errorf("load all edges: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.adj = make(map[string][]adjacency, len(allEdges)*2)
	for _, e := range allEdges {
		g.adj[e.FromID] = append(g.adj[e.FromID], adjacency{
			neighborID: e.ToID,
			edgeType:   e.EdgeType,
			weight:     e.Weight,
			outgoing:   true,
		})
		g.adj[e.ToID] = append(g.adj[e.ToID], adjacency{
			neighborID: e.FromID,
			edgeType:   e.EdgeType,
			weight:     e.Weight,
			outgoing:   false,
		})
	}
	g.loaded = true
	return nil
}

// AddEdge persists an edge to the DB and updates the in-memory cache.
func (g *Graph) AddEdge(ctx context.Context, e *Edge) error {
	dbEdge := &db.EdgeRow{
		ID:        e.ID,
		FromID:    e.FromID,
		ToID:      e.ToID,
		EdgeType:  e.EdgeType,
		Weight:    e.Weight,
		CreatedAt: e.Created,
	}
	if err := g.db.InsertEdge(ctx, dbEdge); err != nil {
		return fmt.Errorf("insert edge: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.adj[e.FromID] = append(g.adj[e.FromID], adjacency{
		neighborID: e.ToID,
		edgeType:   e.EdgeType,
		weight:     e.Weight,
		outgoing:   true,
	})
	g.adj[e.ToID] = append(g.adj[e.ToID], adjacency{
		neighborID: e.FromID,
		edgeType:   e.EdgeType,
		weight:     e.Weight,
		outgoing:   false,
	})

	return nil
}

// NeighborEdges returns all edges adjacent to the given memory ID as exported Edge values.
// If edgeTypes is non-empty, only edges of those types are returned.
// This is safe to call concurrently.
func (g *Graph) NeighborEdges(id string, edgeTypes []string) []*Edge {
	adjs := g.Neighbors(id, edgeTypes)
	result := make([]*Edge, len(adjs))
	for i, a := range adjs {
		from, to := id, a.neighborID
		if !a.outgoing {
			from, to = a.neighborID, id
		}
		result[i] = &Edge{
			FromID:   from,
			ToID:     to,
			EdgeType: a.edgeType,
			Weight:   a.weight,
		}
	}
	return result
}

// Neighbors returns all adjacencies for the given memory ID.
// If edgeTypes is non-empty, only adjacencies of those edge types are returned.
// This is safe to call concurrently.
func (g *Graph) Neighbors(id string, edgeTypes []string) []adjacency {
	g.mu.RLock()
	defer g.mu.RUnlock()

	all := g.adj[id]
	if len(edgeTypes) == 0 {
		result := make([]adjacency, len(all))
		copy(result, all)
		return result
	}

	typeSet := make(map[string]bool, len(edgeTypes))
	for _, et := range edgeTypes {
		typeSet[et] = true
	}

	var result []adjacency
	for _, a := range all {
		if typeSet[a.edgeType] {
			result = append(result, a)
		}
	}
	return result
}
