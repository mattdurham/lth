// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/pkg/lth"
)

// fakeSearcher records the request it received and returns a fixed result set.
type fakeSearcher struct {
	gotReq  *lth.SearchRequest
	results []*lth.SearchResult
}

func (f *fakeSearcher) Search(_ context.Context, req *lth.SearchRequest) ([]*lth.SearchResult, error) {
	f.gotReq = req
	return f.results, nil
}

func TestHandleUISearch_ProjectParamSetsFilterAttrs(t *testing.T) {
	fs := &fakeSearcher{}
	req := httptest.NewRequest(http.MethodGet, "/search?q=tempo&project=grafana/tempo", nil)
	w := httptest.NewRecorder()

	handleUISearch(w, req, fs)

	if fs.gotReq == nil {
		t.Fatal("Search was never called")
	}
	if fs.gotReq.FilterAttrs["project"] != "grafana/tempo" {
		t.Errorf("FilterAttrs[project] = %q, want %q", fs.gotReq.FilterAttrs["project"], "grafana/tempo")
	}
}

func TestHandleUISearch_NoProjectParamLeavesFilterAttrsNil(t *testing.T) {
	fs := &fakeSearcher{}
	req := httptest.NewRequest(http.MethodGet, "/search?q=tempo", nil)
	w := httptest.NewRecorder()

	handleUISearch(w, req, fs)

	if fs.gotReq.FilterAttrs != nil {
		t.Errorf("FilterAttrs = %v, want nil when no project param given", fs.gotReq.FilterAttrs)
	}
}

func TestHandleUISearch_ResponseIncludesProjectField(t *testing.T) {
	fs := &fakeSearcher{
		results: []*lth.SearchResult{
			{
				Memory: &memory.Memory{ID: "m1", Layer: 5, Content: "hello", Attrs: map[string]string{"project": "grafana/tempo"}},
				Score:  0.9,
			},
			{
				Memory: &memory.Memory{ID: "m2", Layer: 5, Content: "no project attr"},
				Score:  0.5,
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/search?q=tempo", nil)
	w := httptest.NewRecorder()

	handleUISearch(w, req, fs)

	var items []struct {
		ID      string `json:"id"`
		Project string `json:"project"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Project != "grafana/tempo" {
		t.Errorf("items[0].Project = %q, want %q", items[0].Project, "grafana/tempo")
	}
	if items[1].Project != "" {
		t.Errorf("items[1].Project = %q, want empty (no project attr set)", items[1].Project)
	}
}
