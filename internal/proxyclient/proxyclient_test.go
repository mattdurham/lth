// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package proxyclient_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"net/http"

	"github.com/mattdurham/lth/internal/apiserver"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/proxyclient"
)

// ---------------------------------------------------------------------------
// stubs (mirrored from apiserver_test — kept local to avoid cross-package dep)
// ---------------------------------------------------------------------------

type stubStore struct {
	memories map[string]*memory.Memory
	deleted  []string
	stored   []*memory.Memory
}

func newStubStore() *stubStore {
	return &stubStore{memories: make(map[string]*memory.Memory)}
}

func (s *stubStore) Store(_ context.Context, layer int, content string, attrs map[string]string) (*memory.Memory, error) {
	m := &memory.Memory{
		ID:        "id-" + content[:min(6, len(content))],
		Layer:     layer,
		Content:   content,
		Attrs:     attrs,
		CreatedAt: time.Now(),
	}
	s.memories[m.ID] = m
	s.stored = append(s.stored, m)
	return m, nil
}

func (s *stubStore) Get(_ context.Context, id string) (*memory.Memory, error) {
	if m, ok := s.memories[id]; ok {
		return m, nil
	}
	return nil, &notFoundErr{id}
}

func (s *stubStore) Search(_ context.Context, _ *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	var out []*memory.ScoredMemory
	for _, m := range s.memories {
		out = append(out, &memory.ScoredMemory{Memory: m, Score: 0.5})
	}
	return out, nil
}

func (s *stubStore) Stats(_ context.Context) (*memory.Stats, error) {
	return &memory.Stats{
		TotalMemories: len(s.memories),
		ByLayer:       map[int]int{1: 0, 2: 0, 3: 0, 4: len(s.memories), 5: 0},
		TotalEdges:    3,
	}, nil
}

func (s *stubStore) ListLayer(_ context.Context, layer int) ([]*memory.Memory, error) {
	var out []*memory.Memory
	for _, m := range s.memories {
		if m.Layer == layer {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *stubStore) SoftDelete(_ context.Context, ids []string, _ string) error {
	s.deleted = append(s.deleted, ids...)
	for _, id := range ids {
		delete(s.memories, id)
	}
	return nil
}

type notFoundErr struct{ id string }

func (e *notFoundErr) Error() string { return "not found: " + e.id }

type stubAttrStore struct {
	merged map[string]map[string]string
}

func newStubAttrStore() *stubAttrStore {
	return &stubAttrStore{merged: make(map[string]map[string]string)}
}

func (a *stubAttrStore) DistinctAttrValues(_ context.Context, _ string) ([]string, error) {
	return []string{"org/repo-a", "org/repo-b"}, nil
}

func (a *stubAttrStore) MergeAttr(_ context.Context, id, key, value string) error {
	if a.merged[id] == nil {
		a.merged[id] = make(map[string]string)
	}
	a.merged[id][key] = value
	return nil
}

type stubGraph struct{}

func (g *stubGraph) NeighborEdges(_ string, _ []string) []*graph.Edge {
	return []*graph.Edge{{FromID: "a", ToID: "b", EdgeType: "relates_to", Weight: 1.0}}
}
func (g *stubGraph) PPR(_ []string, _ float64, _ int) map[string]float64 {
	return map[string]float64{"node1": 0.9, "node2": 0.4}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// test harness: real HTTP round-trip via httptest
// ---------------------------------------------------------------------------

func newTestRoundTrip(t *testing.T) (*proxyclient.Client, *stubStore, *stubAttrStore) {
	t.Helper()
	store := newStubStore()
	attrs := newStubAttrStore()
	g := &stubGraph{}
	h := apiserver.New(store, g, attrs)
	mux := http.NewServeMux()
	apiserver.Register(mux, h)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	client := proxyclient.New(ts.URL)
	return client, store, attrs
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestProxyStore(t *testing.T) {
	client, store, _ := newTestRoundTrip(t)

	m, err := client.Store(t.Context(), 4, "proxy store test", map[string]string{"source": "test"})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if m.Layer != 4 {
		t.Errorf("layer = %d, want 4", m.Layer)
	}
	if m.Content != "proxy store test" {
		t.Errorf("content = %q", m.Content)
	}
	if len(store.stored) != 1 {
		t.Errorf("server stored %d memories, want 1", len(store.stored))
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestProxyGet_Found(t *testing.T) {
	client, store, _ := newTestRoundTrip(t)
	store.memories["known"] = &memory.Memory{ID: "known", Layer: 3, Content: "technique", CreatedAt: time.Now(), Attrs: map[string]string{}}

	m, err := client.Get(t.Context(), "known")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.ID != "known" {
		t.Errorf("ID = %q, want known", m.ID)
	}
}

func TestProxyGet_NotFound(t *testing.T) {
	client, _, _ := newTestRoundTrip(t)
	_, err := client.Get(t.Context(), "ghost")
	if err == nil {
		t.Fatal("expected error for missing memory, got nil")
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func TestProxySearch(t *testing.T) {
	client, store, _ := newTestRoundTrip(t)
	store.memories["s1"] = &memory.Memory{ID: "s1", Layer: 4, Content: "some context", CreatedAt: time.Now(), Attrs: map[string]string{}}

	results, err := client.Search(t.Context(), &memory.SearchRequest{Query: "context"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want 1", len(results))
	}
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func TestProxyStats(t *testing.T) {
	client, _, _ := newTestRoundTrip(t)
	stats, err := client.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalEdges != 3 {
		t.Errorf("TotalEdges = %d, want 3", stats.TotalEdges)
	}
}

// ---------------------------------------------------------------------------
// ListLayer
// ---------------------------------------------------------------------------

func TestProxyListLayer(t *testing.T) {
	client, store, _ := newTestRoundTrip(t)
	store.memories["l4a"] = &memory.Memory{ID: "l4a", Layer: 4, Content: "workspace ctx", CreatedAt: time.Now(), Attrs: map[string]string{}}
	store.memories["l3a"] = &memory.Memory{ID: "l3a", Layer: 3, Content: "technique", CreatedAt: time.Now(), Attrs: map[string]string{}}

	rows, err := client.ListLayer(t.Context(), 4)
	if err != nil {
		t.Fatalf("ListLayer: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "l4a" {
		t.Errorf("rows = %v, want [{l4a}]", rows)
	}
}

// ---------------------------------------------------------------------------
// SoftDelete
// ---------------------------------------------------------------------------

func TestProxySoftDelete(t *testing.T) {
	client, store, _ := newTestRoundTrip(t)
	store.memories["bye"] = &memory.Memory{ID: "bye", Layer: 5, Content: "ephemeral", CreatedAt: time.Now(), Attrs: map[string]string{}}

	if err := client.SoftDelete(t.Context(), []string{"bye"}, "test"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "bye" {
		t.Errorf("deleted = %v, want [bye]", store.deleted)
	}
}

// ---------------------------------------------------------------------------
// MergeAttr
// ---------------------------------------------------------------------------

func TestProxyMergeAttr(t *testing.T) {
	client, store, attrs := newTestRoundTrip(t)
	store.memories["m1"] = &memory.Memory{ID: "m1", Layer: 4, Content: "ctx", CreatedAt: time.Now(), Attrs: map[string]string{}}

	if err := client.MergeAttr(t.Context(), "m1", "project", "lth"); err != nil {
		t.Fatalf("MergeAttr: %v", err)
	}
	if attrs.merged["m1"]["project"] != "lth" {
		t.Errorf("merged project = %q, want lth", attrs.merged["m1"]["project"])
	}
}

// ---------------------------------------------------------------------------
// DistinctAttrValues
// ---------------------------------------------------------------------------

func TestProxyDistinctAttrValues(t *testing.T) {
	client, _, _ := newTestRoundTrip(t)
	vals, err := client.DistinctAttrValues(t.Context(), "project")
	if err != nil {
		t.Fatalf("DistinctAttrValues: %v", err)
	}
	if len(vals) != 2 {
		t.Errorf("vals = %v, want 2 entries", vals)
	}
}

// ---------------------------------------------------------------------------
// GraphNeighbors
// ---------------------------------------------------------------------------

func TestProxyGraphNeighbors(t *testing.T) {
	client, _, _ := newTestRoundTrip(t)
	edges, err := client.GraphNeighbors(t.Context(), "a", 3)
	if err != nil {
		t.Fatalf("GraphNeighbors: %v", err)
	}
	if len(edges) != 1 || edges[0].EdgeType != "relates_to" {
		t.Errorf("edges = %v, want [{relates_to}]", edges)
	}
}

// ---------------------------------------------------------------------------
// GraphPPR
// ---------------------------------------------------------------------------

func TestProxyGraphPPR(t *testing.T) {
	client, _, _ := newTestRoundTrip(t)
	scores, err := client.GraphPPR(t.Context(), []string{"node1"})
	if err != nil {
		t.Fatalf("GraphPPR: %v", err)
	}
	if len(scores) == 0 {
		t.Error("expected non-empty scores")
	}
	if scores["node1"] != 0.9 {
		t.Errorf("node1 score = %f, want 0.9", scores["node1"])
	}
}
