// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"encoding/json"
	"net/http"

	"github.com/mattdurham/lth/internal/memory"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stats, err := s.store.Stats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// searchAPIRequest is the JSON body accepted by POST /api/search.

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req searchAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	results, err := s.store.Search(r.Context(), &memory.SearchRequest{
		Query:      req.Query,
		Layers:     req.Layers,
		TopK:       req.TopK,
		Alpha:      req.Alpha,
		Beta:       req.Beta,
		Gamma:      req.Gamma,
		MinValence: req.MinValence,
		MaxValence: req.MaxValence,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}
