// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"path/filepath"
	"testing"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
)

// mockEmbedder returns deterministic FNV-based vectors.
type mockEmbedder struct {
	dims int
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	h := fnv.New32()
	_, _ = h.Write([]byte(text))
	seed := int64(h.Sum32())

	v := make([]float32, m.dims)
	var norm float64
	for i := range v {
		// Deterministic value using a linear congruential generator.
		seed = (seed*1664525 + 1013904223) & 0x7FFFFFFF
		v[i] = float32(seed%1000)/500.0 - 1.0 // range [-1, 1]
		norm += float64(v[i]) * float64(v[i])
	}
	// Normalize to unit vector.
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range v {
			v[i] = float32(float64(v[i]) / norm)
		}
	}
	return v, nil
}

func (m *mockEmbedder) Dims() int { return m.dims }

// mockLLM returns a fixed response.
type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}

// testMemoryStore creates a MemoryStore backed by a temp DB with mock deps.
func testMemoryStore(t *testing.T) *MemoryStore {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	g := graph.New(d)
	emb := &mockEmbedder{dims: 1024}
	l := &mockLLM{response: "7"}
	cfg := config.Default()

	store, err := NewMemoryStore(d, emb, l, g, cfg)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

func TestStoreDedup(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	// Store same content twice.
	m1, err := s.Store(ctx, 5, "hello dedup world", nil)
	if err != nil {
		t.Fatalf("Store[1]: %v", err)
	}
	m2, err := s.Store(ctx, 5, "hello dedup world", nil)
	if err != nil {
		t.Fatalf("Store[2]: %v", err)
	}

	if m1.ID != m2.ID {
		t.Errorf("IDs differ: %q != %q", m1.ID, m2.ID)
	}

	// Verify DB has exactly one row.
	rows, err := s.db.ListLayer(ctx, 5, false)
	if err != nil {
		t.Fatalf("ListLayer: %v", err)
	}
	var count int
	for _, r := range rows {
		if r.ContentHash == m1.ContentHash {
			count++
		}
	}
	if count != 1 {
		t.Errorf("DB has %d rows for this content_hash, want 1", count)
	}
}

func TestStoreBasic(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	m, err := s.Store(ctx, 1, "L1 axiom: never give up", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if m.Layer != 1 {
		t.Errorf("Layer = %d, want 1", m.Layer)
	}
	// L1 memories must have decay_rate == 0.
	if m.DecayRate != 0 {
		t.Errorf("L1 DecayRate = %f, want 0", m.DecayRate)
	}
	if m.Content != "L1 axiom: never give up" {
		t.Errorf("Content = %q, want original", m.Content)
	}
}

func TestGetUpdatesAccess(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	m, err := s.Store(ctx, 5, "access test memory", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	initialStability := m.Stability

	// Call Get twice.
	for i := 0; i < 2; i++ {
		_, err := s.Get(ctx, m.ID)
		if err != nil {
			t.Fatalf("Get[%d]: %v", i, err)
		}
	}

	got, err := s.Get(ctx, m.ID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}
	if got.AccessCount < 2 {
		t.Errorf("AccessCount = %d, want >= 2", got.AccessCount)
	}
	if got.Stability <= initialStability {
		t.Errorf("Stability = %f, want > %f", got.Stability, initialStability)
	}
}

func TestSearchLayers(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	_, err := s.Store(ctx, 1, "L1 permanent rule", nil)
	if err != nil {
		t.Fatalf("Store L1: %v", err)
	}
	_, err = s.Store(ctx, 5, "L5 ephemeral observation", nil)
	if err != nil {
		t.Fatalf("Store L5: %v", err)
	}

	results, err := s.Search(ctx, &SearchRequest{
		Query:  "rule observation",
		Layers: []int{1},
		TopK:   10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, r := range results {
		if r.Layer != 1 {
			t.Errorf("got result from layer %d, want only layer 1", r.Layer)
		}
	}
}

func TestSearchTopK(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	for i := 0; i < 20; i++ {
		content := fmt.Sprintf("memory item %d about searching data", i)
		if _, err := s.Store(ctx, 5, content, nil); err != nil {
			t.Fatalf("Store[%d]: %v", i, err)
		}
	}

	results, err := s.Search(ctx, &SearchRequest{
		Query: "searching data",
		TopK:  5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 5 {
		t.Errorf("Search returned %d results, want at most 5", len(results))
	}
}

func TestSoftDeleteExcludes(t *testing.T) {
	ctx := context.Background()
	s := testMemoryStore(t)

	m, err := s.Store(ctx, 5, "to be soft deleted test", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if err := s.SoftDelete(ctx, []string{m.ID}, "test"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	results, err := s.Search(ctx, &SearchRequest{
		Query: "soft deleted test",
		TopK:  10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.ID == m.ID {
			t.Errorf("soft-deleted memory %q found in search results", m.ID)
		}
	}
}

// makeTestEmbeddingBytes creates a 1024-dim float32 embedding as little-endian bytes.
func makeTestEmbeddingBytes(seed float32) []byte {
	const dims = 1024
	b := make([]byte, dims*4)
	for i := range dims {
		v := seed * float32(i+1) / float32(dims)
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

// Ensure makeTestEmbeddingBytes is used.
var _ = makeTestEmbeddingBytes
