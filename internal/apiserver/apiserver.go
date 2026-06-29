// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package apiserver implements the lth daemon REST API on top of the existing
// metrics HTTP server. All endpoints live under /api/v1/ and return Markdown
// by default (Accept: application/json switches to JSON). The package exposes
// a single Register function that attaches handlers to an *http.ServeMux.
package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

// GraphStore is the optional graph query surface.
type GraphStore interface {
	// NeighborEdges returns edges adjacent to id (optionally filtered by edgeTypes).
	NeighborEdges(id string, edgeTypes []string) []*graph.Edge
	// PPR runs Personalized PageRank from seeds.
	PPR(seeds []string, d float64, iters int) map[string]float64
}

// AttrStore is the optional attribute surface. *lth.Client satisfies this.
type AttrStore interface {
	DistinctAttrValues(ctx context.Context, key string) ([]string, error)
	MergeAttr(ctx context.Context, id, key, value string) error
}

// Handler holds the dependencies for all /api/v1/ endpoints.
type Handler struct {
	store memory.Store
	graph GraphStore
	attrs AttrStore
}

// New creates a Handler. graph and attrs may be nil — those endpoint groups
// return 503 when the dependency is absent.
func New(store memory.Store, g GraphStore, attrs AttrStore) *Handler {
	return &Handler{store: store, graph: g, attrs: attrs}
}

// Register mounts all /api/v1/ routes on mux.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("/api/v1/memories", h.withStore(h.handleMemories))
	mux.HandleFunc("/api/v1/memories/search", h.withStore(h.handleSearch))
	mux.HandleFunc("/api/v1/memories/", h.withStore(h.handleMemoryByID))
	mux.HandleFunc("/api/v1/stats", h.withStore(h.handleStats))
	mux.HandleFunc("/api/v1/projects", h.handleProjects)
	mux.HandleFunc("/api/v1/graph/neighbors", h.handleGraphNeighbors)
	mux.HandleFunc("/api/v1/graph/ppr", h.handleGraphPPR)
}

// ---------------------------------------------------------------------------
// guards
// ---------------------------------------------------------------------------

func (h *Handler) withStore(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.store == nil {
			httpErr(w, r, http.StatusServiceUnavailable, "memory store not available")
			return
		}
		fn(w, r)
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories         — store a new memory
// GET  /api/v1/memories?layer=N — list memories in a layer
// ---------------------------------------------------------------------------

func (h *Handler) handleMemories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.storeMemory(w, r)
	case http.MethodGet:
		h.listMemories(w, r)
	default:
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type storeRequest struct {
	Layer   int               `json:"layer"`
	Content string            `json:"content"`
	Attrs   map[string]string `json:"attrs"`
}

func (h *Handler) storeMemory(w http.ResponseWriter, r *http.Request) {
	var req storeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Layer < 1 || req.Layer > 5 {
		httpErr(w, r, http.StatusBadRequest, "layer must be 1-5")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		httpErr(w, r, http.StatusBadRequest, "content is required")
		return
	}
	if req.Attrs == nil {
		req.Attrs = make(map[string]string)
	}

	m, err := h.store.Store(r.Context(), req.Layer, req.Content, req.Attrs)
	if err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeResponse(w, r, http.StatusCreated, m, func() string {
		return renderMemoryMD(m)
	})
}

func (h *Handler) listMemories(w http.ResponseWriter, r *http.Request) {
	layerStr := r.URL.Query().Get("layer")
	if layerStr == "" {
		httpErr(w, r, http.StatusBadRequest, "layer query param required (1-5)")
		return
	}
	layer, err := strconv.Atoi(layerStr)
	if err != nil || layer < 1 || layer > 5 {
		httpErr(w, r, http.StatusBadRequest, "layer must be 1-5")
		return
	}

	topStr := r.URL.Query().Get("top")
	top := 0
	if topStr != "" {
		if top, err = strconv.Atoi(topStr); err != nil || top < 0 {
			httpErr(w, r, http.StatusBadRequest, "top must be a non-negative integer")
			return
		}
	}

	rows, err := h.store.ListLayer(r.Context(), layer)
	if err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if top > 0 && len(rows) > top {
		rows = rows[len(rows)-top:] // most recent N — matches CLI behavior
	}

	writeResponse(w, r, http.StatusOK, rows, func() string {
		return renderMemoriesListMD(rows, layer)
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/memories/search
// ---------------------------------------------------------------------------

type searchAPIRequest struct {
	Query       string            `json:"query"`
	Layers      []int             `json:"layers"`
	TopK        int               `json:"top_k"`
	Alpha       float32           `json:"alpha"`
	Beta        float32           `json:"beta"`
	Gamma       float32           `json:"gamma"`
	MinValence  *float32          `json:"min_valence,omitempty"`
	MaxValence  *float32          `json:"max_valence,omitempty"`
	Expand      bool              `json:"expand"`
	FilterAttrs map[string]string `json:"filter_attrs,omitempty"`
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req searchAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		httpErr(w, r, http.StatusBadRequest, "query is required")
		return
	}

	results, err := h.store.Search(r.Context(), &memory.SearchRequest{
		Query:       req.Query,
		Layers:      req.Layers,
		TopK:        req.TopK,
		Alpha:       req.Alpha,
		Beta:        req.Beta,
		Gamma:       req.Gamma,
		MinValence:  req.MinValence,
		MaxValence:  req.MaxValence,
		Expand:      req.Expand,
		FilterAttrs: req.FilterAttrs,
	})
	if err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	writeResponse(w, r, http.StatusOK, results, func() string {
		return renderSearchResultsMD(results, req.Query)
	})
}

// ---------------------------------------------------------------------------
// GET    /api/v1/memories/{id}        — get a memory
// DELETE /api/v1/memories/{id}        — soft-delete a memory
// PATCH  /api/v1/memories/{id}/attrs  — merge attributes
// ---------------------------------------------------------------------------

func (h *Handler) handleMemoryByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/memories/")
	rest = strings.TrimSuffix(rest, "/")

	// PATCH /api/v1/memories/{id}/attrs
	if strings.HasSuffix(rest, "/attrs") {
		id := strings.TrimSuffix(rest, "/attrs")
		if r.Method != http.MethodPatch {
			httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.mergeAttrs(w, r, id)
		return
	}

	id := rest
	if id == "" {
		httpErr(w, r, http.StatusBadRequest, "memory ID required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getMemory(w, r, id)
	case http.MethodDelete:
		h.deleteMemory(w, r, id)
	default:
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) getMemory(w http.ResponseWriter, r *http.Request, id string) {
	m, err := h.store.Get(r.Context(), id)
	if err != nil {
		httpErr(w, r, http.StatusNotFound, fmt.Sprintf("memory %s: %s", id, err.Error()))
		return
	}
	writeResponse(w, r, http.StatusOK, m, func() string {
		return renderMemoryMD(m)
	})
}

func (h *Handler) deleteMemory(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.SoftDelete(r.Context(), []string{id}, "api"); err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "# Deleted\n\nMemory `%s` has been soft-deleted.\n", id)
}

type mergeAttrsRequest struct {
	Attrs map[string]string `json:"attrs"`
}

func (h *Handler) mergeAttrs(w http.ResponseWriter, r *http.Request, id string) {
	var req mergeAttrsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Attrs) == 0 {
		httpErr(w, r, http.StatusBadRequest, "attrs map is required and must be non-empty")
		return
	}
	if h.attrs == nil {
		httpErr(w, r, http.StatusServiceUnavailable, "attribute store not available")
		return
	}

	// Verify the memory exists first.
	m, err := h.store.Get(r.Context(), id)
	if err != nil {
		httpErr(w, r, http.StatusNotFound, fmt.Sprintf("memory %s: %s", id, err.Error()))
		return
	}

	for k, v := range req.Attrs {
		if err := h.attrs.MergeAttr(r.Context(), id, k, v); err != nil {
			httpErr(w, r, http.StatusInternalServerError, fmt.Sprintf("set %s: %s", k, err.Error()))
			return
		}
	}

	// Overlay the updated attrs onto the in-memory copy for the response.
	if m.Attrs == nil {
		m.Attrs = make(map[string]string)
	}
	for k, v := range req.Attrs {
		m.Attrs[k] = v
	}

	writeResponse(w, r, http.StatusOK, m, func() string {
		return renderMemoryMD(m)
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/stats
// ---------------------------------------------------------------------------

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stats, err := h.store.Stats(r.Context())
	if err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeResponse(w, r, http.StatusOK, stats, func() string {
		return renderStatsMD(stats)
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/projects
// ---------------------------------------------------------------------------

func (h *Handler) handleProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.attrs == nil {
		httpErr(w, r, http.StatusServiceUnavailable, "attribute store not available")
		return
	}
	projects, err := h.attrs.DistinctAttrValues(r.Context(), "project")
	if err != nil {
		httpErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeResponse(w, r, http.StatusOK, projects, func() string {
		return renderProjectsMD(projects)
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/graph/neighbors?id=X&depth=N
// ---------------------------------------------------------------------------

func (h *Handler) handleGraphNeighbors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.graph == nil {
		httpErr(w, r, http.StatusServiceUnavailable, "graph not available")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		httpErr(w, r, http.StatusBadRequest, "id query param required")
		return
	}
	depthStr := r.URL.Query().Get("depth")
	depth := 3
	if depthStr != "" {
		var err error
		depth, err = strconv.Atoi(depthStr)
		if err != nil || depth < 1 {
			httpErr(w, r, http.StatusBadRequest, "depth must be a positive integer")
			return
		}
	}
	_ = depth // NeighborEdges uses in-memory adj cache; depth filtering is caller concern
	edges := h.graph.NeighborEdges(id, nil)

	writeResponse(w, r, http.StatusOK, edges, func() string {
		return renderEdgesMD(id, edges)
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/graph/ppr  body: {"seeds":["id1","id2"],"top":10}
// ---------------------------------------------------------------------------

type pprRequest struct {
	Seeds []string `json:"seeds"`
	Top   int      `json:"top"`
}

func (h *Handler) handleGraphPPR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpErr(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.graph == nil {
		httpErr(w, r, http.StatusServiceUnavailable, "graph not available")
		return
	}

	var req pprRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, r, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Seeds) == 0 {
		httpErr(w, r, http.StatusBadRequest, "seeds is required")
		return
	}
	top := req.Top
	if top <= 0 {
		top = 10
	}

	scores := h.graph.PPR(req.Seeds, 0.85, 20)

	ranked := make([]scoredNode, 0, len(scores))
	for id, s := range scores {
		ranked = append(ranked, scoredNode{ID: id, Score: s})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	if len(ranked) > top {
		ranked = ranked[:top]
	}

	writeResponse(w, r, http.StatusOK, ranked, func() string {
		return renderPPRMD(ranked)
	})
}

// ---------------------------------------------------------------------------
// content negotiation helpers
// ---------------------------------------------------------------------------

func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json") ||
		r.URL.Query().Get("format") == "json"
}

func writeResponse(w http.ResponseWriter, r *http.Request, code int, jsonVal any, mdFn func() string) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(jsonVal)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(code)
	_, _ = fmt.Fprint(w, mdFn())
}

func httpErr(w http.ResponseWriter, r *http.Request, code int, msg string) {
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, "# Error %d\n\n%s\n", code, msg)
}

// ---------------------------------------------------------------------------
// Markdown renderers
// ---------------------------------------------------------------------------

func renderMemoryMD(m *memory.Memory) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Memory `%s`\n\n", m.ID)
	fmt.Fprintf(&sb, "**Layer:** L%d  \n", m.Layer)
	fmt.Fprintf(&sb, "**Importance:** %.1f  \n", m.Importance)
	fmt.Fprintf(&sb, "**Valence:** %.2f  \n", m.Valence)
	fmt.Fprintf(&sb, "**Access count:** %d  \n", m.AccessCount)
	fmt.Fprintf(&sb, "**Created:** %s  \n", m.CreatedAt.Format(time.RFC3339))
	if m.Source != "" {
		fmt.Fprintf(&sb, "**Source:** %s  \n", m.Source)
	}
	if len(m.Attrs) > 0 {
		fmt.Fprintf(&sb, "\n## Attributes\n\n")
		keys := make([]string, 0, len(m.Attrs))
		for k := range m.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "- **%s:** %s\n", k, m.Attrs[k])
		}
	}
	fmt.Fprintf(&sb, "\n## Content\n\n%s\n", m.Content)
	return sb.String()
}

func renderMemoriesListMD(rows []*memory.Memory, layer int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Memories — L%d\n\n", layer)
	if len(rows) == 0 {
		fmt.Fprintf(&sb, "_No memories in L%d._\n", layer)
		return sb.String()
	}
	for _, m := range rows {
		content := m.Content
		if len(content) > 120 {
			content = content[:117] + "..."
		}
		fmt.Fprintf(&sb, "## `%s`\n\n", m.ID)
		fmt.Fprintf(&sb, "> %s\n\n", content)
		fmt.Fprintf(&sb, "_Created: %s · Importance: %.1f_\n\n---\n\n",
			m.CreatedAt.Format("2006-01-02 15:04"), m.Importance)
	}
	return sb.String()
}

func renderSearchResultsMD(results []*memory.ScoredMemory, query string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Search Results\n\n**Query:** `%s`  \n**Found:** %d\n\n", query, len(results))
	if len(results) == 0 {
		fmt.Fprintf(&sb, "_No results found._\n")
		return sb.String()
	}
	for _, r := range results {
		content := r.Content
		if len(content) > 200 {
			content = content[:197] + "..."
		}
		fmt.Fprintf(&sb, "## `%s` — L%d · score %.3f\n\n", r.ID, r.Layer, r.Score)
		fmt.Fprintf(&sb, "%s\n\n", content)
		if len(r.Attrs) > 0 {
			parts := make([]string, 0, len(r.Attrs))
			for k, v := range r.Attrs {
				parts = append(parts, fmt.Sprintf("`%s=%s`", k, v))
			}
			sort.Strings(parts)
			fmt.Fprintf(&sb, "_Attrs: %s_\n\n", strings.Join(parts, " "))
		}
		fmt.Fprintf(&sb, "---\n\n")
	}
	return sb.String()
}

func renderStatsMD(s *memory.Stats) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Memory Store Stats\n\n")
	fmt.Fprintf(&sb, "| Metric | Value |\n|--------|-------|\n")
	fmt.Fprintf(&sb, "| Total memories | %d |\n", s.TotalMemories)
	fmt.Fprintf(&sb, "| Total edges | %d |\n", s.TotalEdges)
	fmt.Fprintf(&sb, "\n## By Layer\n\n")
	fmt.Fprintf(&sb, "| Layer | Count |\n|-------|-------|\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&sb, "| L%d | %d |\n", i, s.ByLayer[i])
	}
	return sb.String()
}

func renderProjectsMD(projects []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Projects\n\n")
	if len(projects) == 0 {
		fmt.Fprintf(&sb, "_No projects found._\n")
		return sb.String()
	}
	for _, p := range projects {
		fmt.Fprintf(&sb, "- `%s`\n", p)
	}
	return sb.String()
}

func renderEdgesMD(id string, edges []*graph.Edge) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Graph Neighbors — `%s`\n\n", id)
	if len(edges) == 0 {
		fmt.Fprintf(&sb, "_No connections found._\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "| From | Type | Weight | To |\n|------|------|--------|----|\n")
	for _, e := range edges {
		from, to := e.FromID, e.ToID
		if len(from) > 12 {
			from = filepath.Base(from)
		}
		if len(to) > 12 {
			to = filepath.Base(to)
		}
		fmt.Fprintf(&sb, "| `%s` | %s | %.2f | `%s` |\n", from, e.EdgeType, e.Weight, to)
	}
	return sb.String()
}

type scoredNode struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

func renderPPRMD(ranked []scoredNode) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Personalized PageRank Results\n\n")
	if len(ranked) == 0 {
		fmt.Fprintf(&sb, "_No results._\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "| Rank | ID | Score |\n|------|----|-------|\n")
	for i, n := range ranked {
		fmt.Fprintf(&sb, "| %d | `%s` | %.6f |\n", i+1, n.ID, n.Score)
	}
	return sb.String()
}
