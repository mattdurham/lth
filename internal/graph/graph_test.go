// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package graph

import (
	"context"
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/db"
)

const testDims = 1024

// makeEmbedding creates a deterministic float32 embedding encoded as little-endian bytes.
func makeEmbedding(seed float32) []byte {
	b := make([]byte, testDims*4)
	for i := 0; i < testDims; i++ {
		val := seed * float32(i+1) / float32(testDims)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(val))
	}
	return b
}

// makeEmbeddingF32 creates a float32 slice for graph operations.
func makeEmbeddingF32(seed float32) []float32 {
	v := make([]float32, testDims)
	for i := range v {
		v[i] = seed * float32(i+1) / float32(testDims)
	}
	return v
}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func insertTestMemory(t *testing.T, d *db.DB, id string, emb []byte) {
	t.Helper()
	now := time.Now().UTC()
	row := &db.MemoryRow{
		ID:          id,
		Layer:       3,
		Content:     "test content " + id,
		ContentHash: uuid.New().String(),
		Embedding:   emb,
		Importance:  5.0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := d.InsertMemory(context.Background(), row); err != nil {
		t.Fatalf("InsertMemory(%s): %v", id, err)
	}
}

func TestLoadAll(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	insertTestMemory(t, d, "mem-a", makeEmbedding(0.1))
	insertTestMemory(t, d, "mem-b", makeEmbedding(0.2))

	edge := &db.EdgeRow{
		ID:        "edge-1",
		FromID:    "mem-a",
		ToID:      "mem-b",
		EdgeType:  "relates_to",
		Weight:    0.9,
		CreatedAt: time.Now(),
	}
	if err := d.InsertEdge(ctx, edge); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}

	g := New(d)
	if err := g.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	neighbors := g.Neighbors("mem-a", nil)
	found := false
	for _, n := range neighbors {
		if n.neighborID == "mem-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("Neighbors(mem-a) should contain mem-b after LoadAll")
	}
}

func TestAddEdge(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	insertTestMemory(t, d, "add-a", makeEmbedding(0.1))
	insertTestMemory(t, d, "add-b", makeEmbedding(0.2))

	g := New(d)

	e := &Edge{
		ID:       "edge-add-1",
		FromID:   "add-a",
		ToID:     "add-b",
		EdgeType: "relates_to",
		Weight:   0.85,
		Created:  time.Now(),
	}
	if err := g.AddEdge(ctx, e); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Verify in cache.
	neighbors := g.Neighbors("add-a", nil)
	found := false
	for _, n := range neighbors {
		if n.neighborID == "add-b" {
			found = true
		}
	}
	if !found {
		t.Error("add-b should appear in Neighbors(add-a) after AddEdge")
	}

	// Verify bidirectional in cache.
	neighbors2 := g.Neighbors("add-b", nil)
	foundReverse := false
	for _, n := range neighbors2 {
		if n.neighborID == "add-a" {
			foundReverse = true
		}
	}
	if !foundReverse {
		t.Error("add-a should appear in Neighbors(add-b) — bidirectional edge cache")
	}

	// Verify in DB.
	edges, err := d.GetEdges(ctx, "add-a")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("GetEdges(add-a) = %d edges, want 1", len(edges))
	}
}

func TestNeighborsFilter(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	insertTestMemory(t, d, "filter-a", makeEmbedding(0.1))
	insertTestMemory(t, d, "filter-b", makeEmbedding(0.2))
	insertTestMemory(t, d, "filter-c", makeEmbedding(0.3))

	g := New(d)

	edges := []*Edge{
		{ID: "fe-1", FromID: "filter-a", ToID: "filter-b", EdgeType: "relates_to", Weight: 0.9, Created: time.Now()},
		{ID: "fe-2", FromID: "filter-a", ToID: "filter-c", EdgeType: "supports", Weight: 0.7, Created: time.Now()},
	}
	for _, e := range edges {
		if err := g.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}

	// Filter to "relates_to" only.
	neighbors := g.Neighbors("filter-a", []string{"relates_to"})
	if len(neighbors) != 1 {
		t.Fatalf("Neighbors(filter-a, [relates_to]) = %d, want 1", len(neighbors))
	}
	if neighbors[0].neighborID != "filter-b" {
		t.Errorf("expected filter-b, got %s", neighbors[0].neighborID)
	}

	// No filter: both.
	all := g.Neighbors("filter-a", nil)
	if len(all) != 2 {
		t.Errorf("Neighbors(filter-a, nil) = %d, want 2", len(all))
	}
}

func TestPPRSingleSeed(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	for _, id := range []string{"ppr-a", "ppr-b", "ppr-c", "ppr-d"} {
		insertTestMemory(t, d, id, makeEmbedding(0.1))
	}

	g := New(d)
	chain := []*Edge{
		{ID: "pp-1", FromID: "ppr-a", ToID: "ppr-b", EdgeType: "relates_to", Weight: 1.0, Created: time.Now()},
		{ID: "pp-2", FromID: "ppr-b", ToID: "ppr-c", EdgeType: "relates_to", Weight: 1.0, Created: time.Now()},
		{ID: "pp-3", FromID: "ppr-c", ToID: "ppr-d", EdgeType: "relates_to", Weight: 1.0, Created: time.Now()},
	}
	for _, e := range chain {
		if err := g.AddEdge(ctx, e); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}

	scores := g.PPR([]string{"ppr-a"}, 0.85, 20)

	// Seeds always get the highest scores; immediate neighbors rank above distant ones.
	if scores["ppr-a"] < scores["ppr-b"] {
		t.Errorf("seed ppr-a score %f < ppr-b score %f", scores["ppr-a"], scores["ppr-b"])
	}
	if scores["ppr-b"] < scores["ppr-c"] {
		t.Errorf("ppr-b score %f < ppr-c score %f", scores["ppr-b"], scores["ppr-c"])
	}
}

func TestPPREmptyGraph(t *testing.T) {
	d := testDB(t)

	insertTestMemory(t, d, "isolated", makeEmbedding(0.1))

	g := New(d)

	scores := g.PPR([]string{"isolated"}, 0.85, 20)

	if scores["isolated"] == 0 {
		t.Error("isolated seed node should have non-zero PPR score")
	}
	// Only one non-zero score.
	if len(scores) != 1 {
		t.Errorf("PPR with isolated node: expected 1 score, got %d", len(scores))
	}
}

func TestAutoLinkThreshold(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	// Two similar memories (will have high cosine to our query).
	insertTestMemory(t, d, "similar-1", makeEmbedding(1.0))
	insertTestMemory(t, d, "similar-2", makeEmbedding(0.99))
	// One dissimilar memory.
	insertTestMemory(t, d, "dissimilar", makeEmbedding(-1.0))

	// Insert the "new-mem" memory so it can be used as from_id in edge FK.
	insertTestMemory(t, d, "new-mem", makeEmbedding(0.98))

	g := New(d)

	// AutoLink with embedding similar to similar-1 and similar-2.
	newEmb := makeEmbeddingF32(0.98)
	if err := g.AutoLink(ctx, "new-mem", newEmb); err != nil {
		t.Fatalf("AutoLink: %v", err)
	}

	// Should have created edges to similar memories.
	edges, err := d.GetEdges(ctx, "new-mem")
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) == 0 {
		t.Error("AutoLink should have created at least one edge to similar memories")
	}

	// Verify edge type.
	for _, e := range edges {
		if e.EdgeType != "relates_to" {
			t.Errorf("edge type = %q, want relates_to", e.EdgeType)
		}
		if e.Weight < 0.75 {
			t.Errorf("edge weight = %f, want >= 0.75", e.Weight)
		}
	}
}
