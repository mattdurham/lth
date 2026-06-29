// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package proxyclient provides an HTTP proxy client that speaks to a running
// lth daemon's /api/v1/ REST API. It satisfies the same interface surface as
// *lth.Client so CLI commands can swap in proxy mode transparently.
//
// All requests accept and return JSON (Accept: application/json). The daemon
// returns Markdown by default; sending the Accept header switches to JSON.
package proxyclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

// Client proxies lth operations to a remote daemon over HTTP.
type Client struct {
	base string // e.g. "http://localhost:10010" — no trailing slash
	hc   *http.Client
}

// New creates a Client targeting baseURL (e.g. "http://localhost:10010").
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		base: baseURL,
		hc: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Close is a no-op; satisfies the closer pattern used by *lth.Client.
func (c *Client) Close() error { return nil }

// ---------------------------------------------------------------------------
// Store — POST /api/v1/memories
// ---------------------------------------------------------------------------

type storeRequest struct {
	Layer   int               `json:"layer"`
	Content string            `json:"content"`
	Attrs   map[string]string `json:"attrs"`
}

// Store stores a memory at the given layer with optional attributes.
func (c *Client) Store(ctx context.Context, layer int, content string, attrs map[string]string) (*memory.Memory, error) {
	if attrs == nil {
		attrs = make(map[string]string)
	}
	body, err := json.Marshal(storeRequest{Layer: layer, Content: content, Attrs: attrs})
	if err != nil {
		return nil, err
	}
	var m memory.Memory
	if err := c.do(ctx, http.MethodPost, "/api/v1/memories", body, &m); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Search — POST /api/v1/memories/search
// ---------------------------------------------------------------------------

type searchRequest struct {
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

// Search performs a search and returns ranked results.
func (c *Client) Search(ctx context.Context, req *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	body, err := json.Marshal(searchRequest{
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
		return nil, err
	}
	var results []*memory.ScoredMemory
	if err := c.do(ctx, http.MethodPost, "/api/v1/memories/search", body, &results); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Get — GET /api/v1/memories/{id}
// ---------------------------------------------------------------------------

// Get retrieves a memory by its ID.
func (c *Client) Get(ctx context.Context, id string) (*memory.Memory, error) {
	var m memory.Memory
	if err := c.do(ctx, http.MethodGet, "/api/v1/memories/"+url.PathEscape(id), nil, &m); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Stats — GET /api/v1/stats
// ---------------------------------------------------------------------------

// Stats returns aggregate statistics about the memory store.
func (c *Client) Stats(ctx context.Context) (*memory.Stats, error) {
	var s memory.Stats
	if err := c.do(ctx, http.MethodGet, "/api/v1/stats", nil, &s); err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	return &s, nil
}

// ---------------------------------------------------------------------------
// ListLayer — GET /api/v1/memories?layer=N
// ---------------------------------------------------------------------------

// ListLayer returns all active memories in the given layer.
func (c *Client) ListLayer(ctx context.Context, layer int) ([]*memory.Memory, error) {
	var rows []*memory.Memory
	path := fmt.Sprintf("/api/v1/memories?layer=%d", layer)
	if err := c.do(ctx, http.MethodGet, path, nil, &rows); err != nil {
		return nil, fmt.Errorf("list layer %d: %w", layer, err)
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// SoftDelete — DELETE /api/v1/memories/{id}
// ---------------------------------------------------------------------------

// SoftDelete soft-deletes the memories with the given IDs.
func (c *Client) SoftDelete(ctx context.Context, ids []string, _ string) error {
	for _, id := range ids {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
			c.base+"/api/v1/memories/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.hc.Do(req)
		if err != nil {
			return fmt.Errorf("delete %s: %w", id, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("delete %s: status %d", id, resp.StatusCode)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// MergeAttr — PATCH /api/v1/memories/{id}/attrs
// ---------------------------------------------------------------------------

type mergeAttrsRequest struct {
	Attrs map[string]string `json:"attrs"`
}

// MergeAttr upserts a single attribute key=value on an existing memory.
func (c *Client) MergeAttr(ctx context.Context, id, key, value string) error {
	body, err := json.Marshal(mergeAttrsRequest{Attrs: map[string]string{key: value}})
	if err != nil {
		return err
	}
	path := "/api/v1/memories/" + url.PathEscape(id) + "/attrs"
	var m memory.Memory
	if err := c.do(ctx, http.MethodPatch, path, body, &m); err != nil {
		return fmt.Errorf("merge attr %s=%s on %s: %w", key, value, id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// DistinctAttrValues — GET /api/v1/projects  (project key only; generic key via query)
// ---------------------------------------------------------------------------

// DistinctAttrValues returns all distinct values for a given attribute key.
// The daemon exposes /api/v1/projects for key="project"; for other keys the
// proxy falls back to /api/v1/projects?key=<key> which the server also honors.
func (c *Client) DistinctAttrValues(ctx context.Context, key string) ([]string, error) {
	path := "/api/v1/projects"
	if key != "project" {
		path += "?key=" + url.QueryEscape(key)
	}
	var vals []string
	if err := c.do(ctx, http.MethodGet, path, nil, &vals); err != nil {
		return nil, fmt.Errorf("distinct attr %s: %w", key, err)
	}
	return vals, nil
}

// ---------------------------------------------------------------------------
// GraphNeighbors — GET /api/v1/graph/neighbors?id=X&depth=N
// ---------------------------------------------------------------------------

// GraphNeighbors returns edges connected to the given memory ID.
func (c *Client) GraphNeighbors(ctx context.Context, id string, depth int) ([]*graph.Edge, error) {
	path := fmt.Sprintf("/api/v1/graph/neighbors?id=%s&depth=%d", url.QueryEscape(id), depth)
	var edges []*graph.Edge
	if err := c.do(ctx, http.MethodGet, path, nil, &edges); err != nil {
		return nil, fmt.Errorf("graph neighbors %s: %w", id, err)
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// GraphPPR — POST /api/v1/graph/ppr
// ---------------------------------------------------------------------------

type pprRequest struct {
	Seeds []string `json:"seeds"`
	Top   int      `json:"top"`
}

type pprNode struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// GraphPPR runs Personalized PageRank seeded from the given memory IDs.
func (c *Client) GraphPPR(ctx context.Context, seeds []string) (map[string]float64, error) {
	body, err := json.Marshal(pprRequest{Seeds: seeds, Top: 50})
	if err != nil {
		return nil, err
	}
	var nodes []pprNode
	if err := c.do(ctx, http.MethodPost, "/api/v1/graph/ppr", body, &nodes); err != nil {
		return nil, fmt.Errorf("graph ppr: %w", err)
	}
	out := make(map[string]float64, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n.Score
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// transport
// ---------------------------------------------------------------------------

// do executes an HTTP request and JSON-decodes the response into dst.
// body may be nil for GET/DELETE requests.
func (c *Client) do(ctx context.Context, method, path string, body []byte, dst any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if jsonErr := json.NewDecoder(resp.Body).Decode(&apiErr); jsonErr == nil && apiErr.Error != "" {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if dst != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
