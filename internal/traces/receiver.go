// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package traces

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/db"
	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

const (
	spanQueueCap    = 10_000
	spanMemoryLayer = 5
	maxEdgesPerSpan = 50
)

type spanJob struct {
	span       Span
	receivedAt time.Time
}

// Receiver accepts OTLP HTTP trace data, buffers spans in a queue, and
// processes them into lth memories with same_trace graph edges.
type Receiver struct {
	store memory.Store
	g     *graph.Graph
	d     *db.DB
	queue chan spanJob
	log   *slog.Logger
}

// NewReceiver creates a Receiver with a buffered span queue.
func NewReceiver(store memory.Store, g *graph.Graph, d *db.DB, log *slog.Logger) *Receiver {
	return &Receiver{store: store, g: g, d: d, queue: make(chan spanJob, spanQueueCap), log: log}
}

// ServeHTTP implements http.Handler for POST /v1/traces.
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 4<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	spans, err := parseOTLP(body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now()
	dropped := 0
	for _, s := range spans {
		select {
		case r.queue <- spanJob{span: s, receivedAt: now}:
		default:
			dropped++
		}
	}
	if dropped > 0 {
		r.log.Warn("span queue full, dropped spans", "count", dropped)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"partialSuccess": map[string]any{}}) //nolint:errcheck
}

// Start processes queued spans until ctx is cancelled.
func (r *Receiver) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.queue:
			r.processSpan(ctx, job)
		}
	}
}

func (r *Receiver) processSpan(ctx context.Context, job spanJob) {
	s := job.span
	attrs := map[string]string{
		"trace_id":      s.TraceID,
		"span_id":       s.SpanID,
		"parent_span_id": s.ParentSpanID,
		"service_name":  s.ServiceName,
		"source":        "otlp",
	}
	m, err := r.store.Store(ctx, spanMemoryLayer, spanContent(s), attrs)
	if err != nil {
		r.log.Warn("failed to store span memory", "err", err)
		return
	}
	ids, _ := r.d.GetMemIDsByAttr(ctx, "trace_id", s.TraceID)
	count := 0
	for _, sibID := range ids {
		if sibID == m.ID || count >= maxEdgesPerSpan {
			continue
		}
		r.g.AddEdge(ctx, &graph.Edge{ //nolint:errcheck
			ID:       uuid.New().String(),
			FromID:   m.ID,
			ToID:     sibID,
			EdgeType: "same_trace",
			Weight:   1.0,
			Created:  time.Now(),
		})
		count++
	}
}
