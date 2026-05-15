// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mattdurham/lth/internal/memory"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server serves Prometheus metrics, a search API, and the web UI.
type Server struct {
	addr  string
	reg   *prometheus.Registry
	store memory.Store
	srv   *http.Server
}

// NewServer creates a metrics HTTP server bound to addr (e.g. "localhost:10010").
// store may be nil, in which case /api/* endpoints return 503.
func NewServer(addr string, reg *prometheus.Registry, store memory.Store) *Server {
	return &Server{addr: addr, reg: reg, store: store}
}

// buildMux constructs the HTTP mux used by both Start and TestHandler.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(s.reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok") //nolint:errcheck
	})
	mux.HandleFunc("/api/search", s.withStore(s.handleSearch))
	mux.HandleFunc("/api/stats", s.withStore(s.handleStats))
	mux.HandleFunc("/ui", handleUI)
	mux.HandleFunc("/", handleDashboard)
	return mux
}

// withStore wraps a handler, returning 503 if the store is nil.
func (s *Server) withStore(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			http.Error(w, "store not available", http.StatusServiceUnavailable)
			return
		}
		h(w, r)
	}
}

// TestHandler returns the HTTP handler for use in tests (e.g. httptest.NewServer).
func (s *Server) TestHandler() http.Handler {
	return s.buildMux()
}

// Start begins serving. Blocks until ctx is canceled or a fatal listen error occurs.
func (s *Server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:         s.addr,
		Handler:      s.buildMux(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx) //nolint:contextcheck
	case err := <-errCh:
		return err
	}
}

// handleDashboard serves a minimal HTML status page.
func handleDashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>lth memory store</title>
<style>
body{font-family:monospace;max-width:800px;margin:40px auto;padding:0 20px}
table{border-collapse:collapse;width:100%}
td,th{border:1px solid #ccc;padding:8px;text-align:left}
th{background:#f5f5f5}
</style></head>
<body>
<h1>lth memory store</h1>
<p><a href="/ui">Search UI</a> | <a href="/metrics">Prometheus metrics</a> | <a href="/health">Health</a></p>
<h2>Memory Layers</h2>
<table>
<tr><th>Layer</th><th>Description</th></tr>
<tr><td>L1</td><td>Concepts / Identity</td></tr>
<tr><td>L2</td><td>Guidance / Rules</td></tr>
<tr><td>L3</td><td>Skills / Tools</td></tr>
<tr><td>L4</td><td>Situational</td></tr>
<tr><td>L5</td><td>Raw observations</td></tr>
</table>
<p><small>Live counts available at <a href="/metrics">/metrics</a> (lth_memories_total). Refresh this page to update.</small></p>
</body></html>`) //nolint:errcheck
}
