// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package lth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
)

// testClient creates a Client with a temp DB for testing.
// It requires no network (Ollama) - the embedder will fail gracefully.
func testClient(t *testing.T) *Client {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(dir, "test.db")
	// Use a non-existent embedder URL so embedding fails gracefully (store without embedding).
	cfg.Embedding.BaseURL = "http://localhost:0"
	cfg.LLM.BaseURL = "http://localhost:0"

	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestNewClientClose(t *testing.T) {
	c := testClient(t)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	// Close is called by t.Cleanup in testClient.
}

func TestStoreAndGet(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	m, err := c.Store(ctx, 5, "test content for get", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if m == nil {
		t.Fatal("Store returned nil memory")
	}

	got, err := c.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != "test content for get" {
		t.Errorf("Content = %q, want %q", got.Content, "test content for get")
	}
	if got.Layer != 5 {
		t.Errorf("Layer = %d, want 5", got.Layer)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	if _, err := c.Store(ctx, 3, "L3 memory content", nil); err != nil {
		t.Fatalf("Store L3: %v", err)
	}
	if _, err := c.Store(ctx, 5, "L5 memory content", nil); err != nil {
		t.Fatalf("Store L5: %v", err)
	}

	stats, err := c.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalMemories != 2 {
		t.Errorf("TotalMemories = %d, want 2", stats.TotalMemories)
	}
	if stats.ByLayer[3] != 1 {
		t.Errorf("ByLayer[3] = %d, want 1", stats.ByLayer[3])
	}
	if stats.ByLayer[5] != 1 {
		t.Errorf("ByLayer[5] = %d, want 1", stats.ByLayer[5])
	}
}

func TestSearch(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	// Store a memory so FTS has something to find.
	if _, err := c.Store(ctx, 3, "Go error handling patterns", nil); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Search with FTS fallback (embedding will fail against localhost:0, FTS should still work).
	results, err := c.Search(ctx, &SearchRequest{
		Query:  "error handling",
		Layers: []int{3},
		TopK:   5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// FTS fallback should return at least 1 result.
	if len(results) == 0 {
		t.Error("Search: expected at least 1 result, got 0")
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	// Empty query should not error; may return empty results.
	results, err := c.Search(ctx, &SearchRequest{
		Query: "",
		TopK:  5,
	})
	if err != nil {
		t.Fatalf("Search(empty query): unexpected error: %v", err)
	}
	// With no query, results may be nil or empty — just ensure no panic/crash.
	_ = results
}

func TestSearchNoResults(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	// Search on empty store should return empty, not error.
	results, err := c.Search(ctx, &SearchRequest{
		Query:  "something that does not exist",
		Layers: []int{3},
		TopK:   5,
	})
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search on empty store: got %d results, want 0", len(results))
	}
}

func TestGraphNeighbors(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	m1, err := c.Store(ctx, 3, "memory one for graph test", nil)
	if err != nil {
		t.Fatalf("Store m1: %v", err)
	}

	// No edges exist yet — should return empty list, not error.
	edges, err := c.GraphNeighbors(ctx, m1.ID, 1)
	if err != nil {
		t.Fatalf("GraphNeighbors: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("GraphNeighbors (no edges): got %d edges, want 0", len(edges))
	}
}

func TestGraphNeighborsZeroDepth(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	m1, err := c.Store(ctx, 3, "depth zero test memory", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// depth=0 should always return nil/empty.
	edges, err := c.GraphNeighbors(ctx, m1.ID, 0)
	if err != nil {
		t.Fatalf("GraphNeighbors(depth=0): %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("GraphNeighbors(depth=0): got %d edges, want 0", len(edges))
	}
}

func TestGraphPPR(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	m1, err := c.Store(ctx, 3, "ppr seed memory", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	scores, err := c.GraphPPR(ctx, []string{m1.ID})
	if err != nil {
		t.Fatalf("GraphPPR: %v", err)
	}
	if scores == nil {
		t.Fatal("GraphPPR returned nil scores map")
	}
}

func TestGraphPPREmptySeeds(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)

	scores, err := c.GraphPPR(ctx, []string{})
	if err != nil {
		t.Fatalf("GraphPPR(empty seeds): %v", err)
	}
	// Empty seeds: PPR returns nil (no personalization vector to initialize).
	_ = scores
}

func TestMemoryStoreAccessor(t *testing.T) {
	c := testClient(t)
	s := c.MemoryStore()
	if s == nil {
		t.Error("MemoryStore() returned nil")
	}
}

func TestGraphAccessor(t *testing.T) {
	c := testClient(t)
	g := c.Graph()
	if g == nil {
		t.Error("Graph() returned nil")
	}
}
