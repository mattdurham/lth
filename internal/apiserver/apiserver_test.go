// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package apiserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/apiserver"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubStore struct {
	memories map[string]*memory.Memory
	deleted  []string
	stored   []*memory.Memory
	searchFn func(*memory.SearchRequest) []*memory.ScoredMemory
}

func newStubStore() *stubStore {
	return &stubStore{memories: make(map[string]*memory.Memory)}
}

func (s *stubStore) Store(_ context.Context, layer int, content string, attrs map[string]string) (*memory.Memory, error) {
	m := &memory.Memory{
		ID:        "test-id-" + content[:min(4, len(content))],
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

func (s *stubStore) Search(_ context.Context, req *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	if s.searchFn != nil {
		return s.searchFn(req), nil
	}
	return nil, nil
}

func (s *stubStore) Stats(_ context.Context) (*memory.Stats, error) {
	return &memory.Stats{
		TotalMemories: len(s.memories),
		ByLayer:       map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 5},
		TotalEdges:    7,
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
	merged   map[string]map[string]string // id → key → value
	projects []string
}

func newStubAttrStore() *stubAttrStore {
	return &stubAttrStore{
		merged:   make(map[string]map[string]string),
		projects: []string{"grafana/tempo", "grafana/mimir"},
	}
}

func (a *stubAttrStore) DistinctAttrValues(_ context.Context, _ string) ([]string, error) {
	return a.projects, nil
}

func (a *stubAttrStore) MergeAttr(_ context.Context, id, key, value string) error {
	if a.merged[id] == nil {
		a.merged[id] = make(map[string]string)
	}
	a.merged[id][key] = value
	return nil
}

type stubGraph struct {
	edges []*graph.Edge
	ppr   map[string]float64
}

func (g *stubGraph) NeighborEdges(_ string, _ []string) []*graph.Edge { return g.edges }
func (g *stubGraph) PPR(_ []string, _ float64, _ int) map[string]float64 {
	if g.ppr == nil {
		return map[string]float64{}
	}
	return g.ppr
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func newTestServer(t *testing.T) (*httptest.Server, *stubStore, *stubAttrStore, *stubGraph) {
	t.Helper()
	store := newStubStore()
	attrs := newStubAttrStore()
	g := &stubGraph{}
	h := apiserver.New(store, g, attrs)
	mux := http.NewServeMux()
	apiserver.Register(mux, h)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, store, attrs, g
}

func jsonBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func do(t *testing.T, method, url string, body io.Reader, acceptJSON bool) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if acceptJSON {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories
// ---------------------------------------------------------------------------

func TestStoreMemory_JSON(t *testing.T) {
	ts, store, _, _ := newTestServer(t)

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories",
		jsonBody(t, map[string]any{"layer": 4, "content": "hello world", "attrs": map[string]string{"project": "lth"}}),
		true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201; body: %s", resp.StatusCode, body)
	}

	var m memory.Memory
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Layer != 4 {
		t.Errorf("layer = %d, want 4", m.Layer)
	}
	if m.Content != "hello world" {
		t.Errorf("content = %q, want %q", m.Content, "hello world")
	}
	if len(store.stored) != 1 {
		t.Errorf("stored count = %d, want 1", len(store.stored))
	}
}

func TestStoreMemory_Markdown(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories",
		jsonBody(t, map[string]any{"layer": 3, "content": "markdown test"}),
		false) // no Accept: application/json
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d, want 201", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") {
		t.Errorf("Content-Type = %q, want text/markdown", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "# Memory") {
		t.Errorf("expected markdown header in body; got: %s", body)
	}
}

func TestStoreMemory_BadLayer(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories",
		jsonBody(t, map[string]any{"layer": 9, "content": "bad"}),
		true)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestStoreMemory_EmptyContent(t *testing.T) {
	ts, _, _, _ := newTestServer(t)

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories",
		jsonBody(t, map[string]any{"layer": 5, "content": "   "}),
		true)
	readBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories?layer=N
// ---------------------------------------------------------------------------

func TestListMemories(t *testing.T) {
	ts, store, _, _ := newTestServer(t)
	// Pre-seed
	store.memories["abc"] = &memory.Memory{ID: "abc", Layer: 4, Content: "context memory", CreatedAt: time.Now()}
	store.memories["def"] = &memory.Memory{ID: "def", Layer: 3, Content: "technique memory", CreatedAt: time.Now()}

	resp := do(t, http.MethodGet, ts.URL+"/api/v1/memories?layer=4", nil, true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var rows []*memory.Memory
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "abc" {
		t.Errorf("rows = %v, want [{ID:abc}]", rows)
	}
}

func TestListMemories_MissingLayer(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/memories", nil, true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/memories/{id}
// ---------------------------------------------------------------------------

func TestGetMemory_Found(t *testing.T) {
	ts, store, _, _ := newTestServer(t)
	store.memories["xyz"] = &memory.Memory{ID: "xyz", Layer: 2, Content: "principle", CreatedAt: time.Now()}

	resp := do(t, http.MethodGet, ts.URL+"/api/v1/memories/xyz", nil, true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var m memory.Memory
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.ID != "xyz" {
		t.Errorf("ID = %q, want xyz", m.ID)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/memories/nope", nil, true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/memories/{id}
// ---------------------------------------------------------------------------

func TestDeleteMemory(t *testing.T) {
	ts, store, _, _ := newTestServer(t)
	store.memories["del-me"] = &memory.Memory{ID: "del-me", Layer: 5, Content: "ephemeral", CreatedAt: time.Now()}

	resp := do(t, http.MethodDelete, ts.URL+"/api/v1/memories/del-me", nil, true)
	resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status %d, want 204", resp.StatusCode)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "del-me" {
		t.Errorf("deleted = %v, want [del-me]", store.deleted)
	}
}

// ---------------------------------------------------------------------------
// PATCH /api/v1/memories/{id}/attrs
// ---------------------------------------------------------------------------

func TestMergeAttrs(t *testing.T) {
	ts, store, attrs, _ := newTestServer(t)
	store.memories["m1"] = &memory.Memory{ID: "m1", Layer: 4, Content: "ctx", CreatedAt: time.Now(), Attrs: map[string]string{}}

	resp := do(t, http.MethodPatch, ts.URL+"/api/v1/memories/m1/attrs",
		jsonBody(t, map[string]any{"attrs": map[string]string{"project": "lth", "tag": "test"}}),
		true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	if attrs.merged["m1"]["project"] != "lth" {
		t.Errorf("merged project = %q, want lth", attrs.merged["m1"]["project"])
	}
	if attrs.merged["m1"]["tag"] != "test" {
		t.Errorf("merged tag = %q, want test", attrs.merged["m1"]["tag"])
	}
}

func TestMergeAttrs_NotFound(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodPatch, ts.URL+"/api/v1/memories/ghost/attrs",
		jsonBody(t, map[string]any{"attrs": map[string]string{"k": "v"}}),
		true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories/search
// ---------------------------------------------------------------------------

func TestSearch(t *testing.T) {
	ts, store, _, _ := newTestServer(t)
	store.searchFn = func(req *memory.SearchRequest) []*memory.ScoredMemory {
		return []*memory.ScoredMemory{
			{Memory: &memory.Memory{ID: "r1", Layer: 3, Content: req.Query}, Score: 0.9},
		}
	}

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories/search",
		jsonBody(t, map[string]any{"query": "error handling"}),
		true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var results []*memory.ScoredMemory
	if err := json.Unmarshal([]byte(body), &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 || results[0].ID != "r1" {
		t.Errorf("results = %v, want [{ID:r1}]", results)
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories/search",
		jsonBody(t, map[string]any{"query": ""}),
		true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestSearch_MethodNotAllowed(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/memories/search", nil, true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", resp.StatusCode)
	}
}

func TestSearch_MarkdownOutput(t *testing.T) {
	ts, store, _, _ := newTestServer(t)
	store.searchFn = func(_ *memory.SearchRequest) []*memory.ScoredMemory {
		return []*memory.ScoredMemory{
			{Memory: &memory.Memory{ID: "r1", Layer: 3, Content: "some technique"}, Score: 0.8},
		}
	}

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/memories/search",
		jsonBody(t, map[string]any{"query": "technique"}),
		false)
	body := readBody(t, resp)

	if !strings.Contains(body, "# Search Results") {
		t.Errorf("expected markdown header; got: %s", body)
	}
	if !strings.Contains(body, "r1") {
		t.Errorf("expected result ID in markdown; got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/stats
// ---------------------------------------------------------------------------

func TestStats(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/stats", nil, true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var s memory.Stats
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.TotalEdges != 7 {
		t.Errorf("TotalEdges = %d, want 7", s.TotalEdges)
	}
}

func TestStats_MarkdownTable(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/stats", nil, false)
	body := readBody(t, resp)

	if !strings.Contains(body, "| L1 |") {
		t.Errorf("expected layer table in markdown; got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/projects
// ---------------------------------------------------------------------------

func TestProjects(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/projects", nil, true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var projects []string
	if err := json.Unmarshal([]byte(body), &projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("projects = %v, want 2", projects)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/graph/neighbors
// ---------------------------------------------------------------------------

func TestGraphNeighbors(t *testing.T) {
	ts, _, _, g := newTestServer(t)
	g.edges = []*graph.Edge{
		{FromID: "aaa", ToID: "bbb", EdgeType: "relates_to", Weight: 0.9},
	}

	resp := do(t, http.MethodGet, ts.URL+"/api/v1/graph/neighbors?id=aaa", nil, true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var edges []*graph.Edge
	if err := json.Unmarshal([]byte(body), &edges); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(edges) != 1 || edges[0].EdgeType != "relates_to" {
		t.Errorf("edges = %v, want [{relates_to}]", edges)
	}
}

func TestGraphNeighbors_MissingID(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodGet, ts.URL+"/api/v1/graph/neighbors", nil, true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/graph/ppr
// ---------------------------------------------------------------------------

func TestGraphPPR(t *testing.T) {
	ts, _, _, g := newTestServer(t)
	g.ppr = map[string]float64{"aaa": 0.8, "bbb": 0.5, "ccc": 0.3}

	resp := do(t, http.MethodPost, ts.URL+"/api/v1/graph/ppr",
		jsonBody(t, map[string]any{"seeds": []string{"aaa"}, "top": 2}),
		true)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200; body: %s", resp.StatusCode, body)
	}
	var nodes []struct {
		ID    string  `json:"id"`
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal([]byte(body), &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes = %d, want 2 (top=2)", len(nodes))
	}
	if nodes[0].Score < nodes[1].Score {
		t.Errorf("nodes not sorted by score descending")
	}
}

func TestGraphPPR_NoSeeds(t *testing.T) {
	ts, _, _, _ := newTestServer(t)
	resp := do(t, http.MethodPost, ts.URL+"/api/v1/graph/ppr",
		jsonBody(t, map[string]any{"seeds": []string{}}),
		true)
	readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// no-store guard
// ---------------------------------------------------------------------------

func TestNoStore_Returns503(t *testing.T) {
	h := apiserver.New(nil, nil, nil)
	mux := http.NewServeMux()
	apiserver.Register(mux, h)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	for _, path := range []string{
		"/api/v1/stats",
		"/api/v1/memories?layer=3",
	} {
		resp, err := http.Get(ts.URL + path) //nolint:noctx
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s: status %d, want 503", path, resp.StatusCode)
		}
	}
}
