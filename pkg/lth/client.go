// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package lth provides the public client API for the lth agentic memory system.
package lth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/vector"
)

// Client provides the public API for the lth memory system.
type Client struct {
	store *memory.MemoryStore
	graph *graph.Graph
	cfg   *config.Config
	db    *db.DB
}

// NewClient creates a Client from the given config. Call Close when done.
func NewClient(cfg *config.Config) (*Client, error) {
	// Ensure the data directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.DB.Path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	d, err := db.Open(cfg.DB.Path, config.EmbeddingDim)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := vector.EnsureEmbeddingServer(cfg); err != nil {
		// Log warning but don't fail — search degrades to FTS-only without embeddings.
		slog.Warn("embedding server unavailable", "err", err)
	}
	emb := vector.NewEmbedder(cfg)
	l := llm.New(cfg)

	g := graph.New(d)
	store, err := memory.NewMemoryStore(d, emb, l, g, cfg)
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("create memory store: %w", err)
	}

	return &Client{
		store: store,
		graph: g,
		cfg:   cfg,
		db:    d,
	}, nil
}

// Close closes the client and releases all resources.
func (c *Client) Close() error {
	c.store.Close()
	return c.db.Close()
}

// Store stores a memory at the given layer with optional attributes.
func (c *Client) Store(ctx context.Context, layer int, content string, attrs map[string]string) (*Memory, error) {
	return c.store.Store(ctx, layer, content, attrs)
}

// Search performs a multi-modal search and returns ranked results.
func (c *Client) Search(ctx context.Context, req *SearchRequest) ([]*SearchResult, error) {
	return c.store.Search(ctx, req)
}

// Get retrieves a memory by its ID.
func (c *Client) Get(ctx context.Context, id string) (*Memory, error) {
	return c.store.Get(ctx, id)
}

// DistinctAttrValues returns all distinct values for a given attribute key.
func (c *Client) DistinctAttrValues(ctx context.Context, key string) ([]string, error) {
	return c.store.DistinctAttrValues(ctx, key)
}

// ListByAttribute returns up to limit active memories that carry the given
// key=value attribute pair, newest first. limit <=0 means no cap.
//
// Intended for callers that have already identified a source (e.g. a
// specific source_file path returned by Search) and want the full set of
// memories derived from it, not just the ones that ranked highest in
// semantic search. Soft-deleted memories are excluded.
func (c *Client) ListByAttribute(ctx context.Context, key, value string, limit int) ([]*Memory, error) {
	ids, err := c.db.GetMemIDsByAttr(ctx, key, value)
	if err != nil {
		return nil, err
	}
	out := make([]*Memory, 0, len(ids))
	for _, id := range ids {
		m, err := c.store.Get(ctx, id)
		if err != nil || m == nil {
			continue
		}
		if m.CompactedAt != nil {
			continue // exclude soft-deleted
		}
		out = append(out, m)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// SoftDelete soft-deletes the memories with the given IDs.
// It does not hard-delete any rows; compacted_at is set.
func (c *Client) SoftDelete(ctx context.Context, ids []string, reason string) error {
	return c.store.SoftDelete(ctx, ids, reason)
}

// MergeAttr upserts a single attribute key=value on an existing memory.
// If the key already exists it is overwritten; other attrs are unchanged.
func (c *Client) MergeAttr(ctx context.Context, id, key, value string) error {
	return c.db.MergeAttribute(ctx, id, key, value)
}

// ListLayer returns all active memories in the given layer.
func (c *Client) ListLayer(ctx context.Context, layer int) ([]*Memory, error) {
	return c.store.ListLayer(ctx, layer)
}

// Stats returns aggregate statistics about the memory store.
func (c *Client) Stats(ctx context.Context) (*Stats, error) {
	return c.store.Stats(ctx)
}

// GraphNeighbors returns the edges connected to the given memory ID up to depth hops.
func (c *Client) GraphNeighbors(ctx context.Context, id string, depth int) ([]*Edge, error) {
	return bfsEdgesDB(ctx, c.db, id, depth)
}

// GraphPPR runs Personalized PageRank seeded from the given memory IDs.
func (c *Client) GraphPPR(_ context.Context, seeds []string) (map[string]float64, error) {
	return c.graph.PPR(seeds, 0.85, 20), nil
}

// MemoryStore returns the internal memory.Store, for use by internal components
// such as the background daemon (compactor and watcher).
func (c *Client) MemoryStore() memory.Store {
	return c.store
}

// Graph returns the internal graph.Graph, for use by the background daemon (compactor).
func (c *Client) Graph() *graph.Graph {
	return c.graph
}

// bfsEdgesDB performs BFS via DB edge queries and returns traversed edges up to depth.
func bfsEdgesDB(ctx context.Context, d *db.DB, rootID string, depth int) ([]*Edge, error) {
	if depth <= 0 {
		return nil, nil
	}

	visited := map[string]bool{rootID: true}
	queue := []string{rootID}
	var result []*Edge

	for level := 0; level < depth && len(queue) > 0; level++ {
		var next []string
		for _, nodeID := range queue {
			edges, err := d.GetEdges(ctx, nodeID)
			if err != nil {
				return result, err
			}
			for _, e := range edges {
				nid := e.ToID
				if e.FromID != nodeID {
					nid = e.FromID
				}
				if !visited[nid] {
					visited[nid] = true
					next = append(next, nid)
					result = append(result, &Edge{
						FromID:   e.FromID,
						ToID:     e.ToID,
						EdgeType: e.EdgeType,
						Weight:   e.Weight,
						Created:  e.CreatedAt,
					})
				}
			}
		}
		queue = next
	}

	return result, nil
}
