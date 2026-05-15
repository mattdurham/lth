// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package metrics_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// newTestReg returns an isolated registry and metrics for testing.
func newTestReg(t *testing.T) (*prometheus.Registry, *metrics.Metrics) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := metrics.New(reg)
	return reg, m
}

// gatherNames collects all metric family names from the registry after forcing
// at least one observation on each metric so they appear in Gather output.
func touchAll(m *metrics.Metrics) {
	m.MemoriesTotal.WithLabelValues("1").Set(0)
	m.GraphEdgesTotal.Set(0)
	m.CompactionsTotal.WithLabelValues("L5toL4").Add(0)
	m.LLMRequestsTotal.WithLabelValues("anthropic", "complete", "success").Add(0)
	m.LLMRequestDuration.WithLabelValues("anthropic", "complete").Observe(0)
	m.EmbedRequestsTotal.WithLabelValues("huggingface", "success").Add(0)
	m.EmbedRequestDuration.WithLabelValues("huggingface").Observe(0)
	m.SearchesTotal.WithLabelValues("vector").Add(0)
	m.SearchDuration.Observe(0)
	m.WatcherMessagesTotal.Add(0)
	m.WatcherFilesWatched.Set(0)
}

func TestNew_RegistersAllMetrics(t *testing.T) {
	reg, m := newTestReg(t)
	touchAll(m)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	wantNames := []string{
		"lth_memories_total",
		"lth_graph_edges_total",
		"lth_compactions_total",
		"lth_llm_requests_total",
		"lth_llm_request_duration_seconds",
		"lth_embedding_requests_total",
		"lth_embedding_request_duration_seconds",
		"lth_searches_total",
		"lth_search_duration_seconds",
		"lth_watcher_messages_ingested_total",
		"lth_watcher_files_watched_total",
	}

	got := make(map[string]bool, len(mfs))
	for _, mf := range mfs {
		got[mf.GetName()] = true
	}
	for _, name := range wantNames {
		if !got[name] {
			t.Errorf("expected metric %q to be registered", name)
		}
	}
}

// --- Server tests ---

func TestServer_MetricsEndpoint(t *testing.T) {
	reg, m := newTestReg(t)
	m.MemoriesTotal.WithLabelValues("5").Set(42)

	srv := metrics.NewServer("localhost:0", reg, nil)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "lth_memories_total") {
		t.Errorf("expected lth_memories_total in response body")
	}
}

func TestServer_HealthEndpoint(t *testing.T) {
	reg, _ := newTestReg(t)
	srv := metrics.NewServer("localhost:0", reg, nil)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
}

func TestServer_DashboardEndpoint(t *testing.T) {
	reg, _ := newTestReg(t)
	srv := metrics.NewServer("localhost:0", reg, nil)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "lth memory store") {
		t.Errorf("expected dashboard title in response body")
	}
}

// --- InstrumentedLLM tests ---

type stubLLM struct {
	result string
	err    error
}

func (s *stubLLM) Complete(_ context.Context, _ string) (string, error) {
	return s.result, s.err
}

func findMetricFamily(reg *prometheus.Registry, name string) []*dto.Metric {
	mfs, _ := reg.Gather()
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf.GetMetric()
		}
	}
	return nil
}

func hasLabelValue(reg *prometheus.Registry, family, labelName, labelValue string) bool {
	for _, m := range findMetricFamily(reg, family) {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == labelName && lp.GetValue() == labelValue {
				return true
			}
		}
	}
	return false
}

func TestInstrumentedLLM_RecordsSuccess(t *testing.T) {
	reg, m := newTestReg(t)

	inner := &stubLLM{result: "hello"}
	wrapped := metrics.NewInstrumentedLLM(inner, "testprovider", m)

	got, err := wrapped.Complete(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}

	if !hasLabelValue(reg, "lth_llm_requests_total", "status", "success") {
		t.Error("expected lth_llm_requests_total with status=success")
	}
}

func TestInstrumentedLLM_RecordsError(t *testing.T) {
	reg, m := newTestReg(t)

	inner := &stubLLM{err: errors.New("llm failed")}
	wrapped := metrics.NewInstrumentedLLM(inner, "testprovider", m)

	_, err := wrapped.Complete(context.Background(), "prompt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !hasLabelValue(reg, "lth_llm_requests_total", "status", "error") {
		t.Error("expected lth_llm_requests_total with status=error")
	}
}

// --- InstrumentedEmbedder tests ---

type stubEmbedder struct {
	result []float32
	dims   int
	err    error
}

func (s *stubEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return s.result, s.err
}

func (s *stubEmbedder) Dims() int { return s.dims }

func TestInstrumentedEmbedder_RecordsSuccess(t *testing.T) {
	reg, m := newTestReg(t)

	inner := &stubEmbedder{result: []float32{0.1, 0.2}, dims: 2}
	wrapped := metrics.NewInstrumentedEmbedder(inner, "testprovider", m)

	got, err := wrapped.Embed(context.Background(), "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d dims, want 2", len(got))
	}
	if wrapped.Dims() != 2 {
		t.Errorf("Dims() = %d, want 2", wrapped.Dims())
	}

	if !hasLabelValue(reg, "lth_embedding_requests_total", "status", "success") {
		t.Error("expected lth_embedding_requests_total with status=success")
	}
}

// --- API endpoint tests ---

func TestServer_UIEndpoint(t *testing.T) {
	reg, _ := newTestReg(t)
	srv := metrics.NewServer("localhost:0", reg, nil)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /ui: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "lth") {
		t.Errorf("expected lth in UI response body")
	}
}

func TestServer_StatsEndpoint_NoStore(t *testing.T) {
	reg, _ := newTestReg(t)
	srv := metrics.NewServer("localhost:0", reg, nil)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/stats") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503 (no store)", resp.StatusCode)
	}
}

func TestServer_StatsEndpoint_WithStore(t *testing.T) {
	reg, _ := newTestReg(t)
	store := &stubStore{
		stats: &memory.Stats{
			TotalMemories: 100,
			ByLayer:       map[int]int{1: 5, 2: 10, 3: 20, 4: 40, 5: 25},
			TotalEdges:    200,
		},
	}
	srv := metrics.NewServer("localhost:0", reg, store)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/stats") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
	var got memory.Stats
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if got.TotalMemories != 100 {
		t.Errorf("TotalMemories = %d, want 100", got.TotalMemories)
	}
	if got.TotalEdges != 200 {
		t.Errorf("TotalEdges = %d, want 200", got.TotalEdges)
	}
}

func TestServer_SearchEndpoint_WithStore(t *testing.T) {
	reg, _ := newTestReg(t)
	store := &stubStore{}
	srv := metrics.NewServer("localhost:0", reg, store)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"query": "test query",
		"topK":  5,
	})
	resp, err := http.Post(ts.URL+"/api/search", "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		t.Fatalf("POST /api/search: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", resp.StatusCode)
	}
}

func TestServer_SearchEndpoint_MethodNotAllowed(t *testing.T) {
	reg, _ := newTestReg(t)
	store := &stubStore{}
	srv := metrics.NewServer("localhost:0", reg, store)
	ts := httptest.NewServer(srv.TestHandler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/search") //nolint:noctx
	if err != nil {
		t.Fatalf("GET /api/search: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", resp.StatusCode)
	}
}

func TestInstrumentedEmbedder_RecordsError(t *testing.T) {
	reg, m := newTestReg(t)

	inner := &stubEmbedder{err: errors.New("embed failed")}
	wrapped := metrics.NewInstrumentedEmbedder(inner, "testprovider", m)

	_, err := wrapped.Embed(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !hasLabelValue(reg, "lth_embedding_requests_total", "status", "error") {
		t.Error("expected lth_embedding_requests_total with status=error")
	}
}

// --- RefreshLoop tests ---

type stubStore struct {
	stats *memory.Stats
	err   error
}

func (s *stubStore) Store(_ context.Context, _ int, _ string, _ map[string]string) (*memory.Memory, error) {
	return nil, nil //nolint:nilnil
}

func (s *stubStore) Get(_ context.Context, _ string) (*memory.Memory, error) {
	return nil, nil //nolint:nilnil
}

func (s *stubStore) Search(_ context.Context, _ *memory.SearchRequest) ([]*memory.ScoredMemory, error) {
	return nil, nil //nolint:nilnil
}

func (s *stubStore) Stats(_ context.Context) (*memory.Stats, error) {
	return s.stats, s.err
}

func (s *stubStore) ListLayer(_ context.Context, _ int) ([]*memory.Memory, error) {
	return nil, nil //nolint:nilnil
}

func (s *stubStore) SoftDelete(_ context.Context, _ []string, _ string) error {
	return nil
}

func TestRefreshLoop_UpdatesGauges(t *testing.T) {
	_, m := newTestReg(t)

	store := &stubStore{
		stats: &memory.Stats{
			ByLayer:    map[int]int{1: 10, 2: 20, 3: 30, 4: 40, 5: 50},
			TotalEdges: 99,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		metrics.RefreshLoop(ctx, store, m, 10*time.Second)
	}()

	// Give the immediate first refresh time to execute.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	// Read the gauge value via the prometheus Collect mechanism.
	ch := make(chan prometheus.Metric, 20)
	m.MemoriesTotal.Collect(ch)
	close(ch)

	var l5val float64
	for metric := range ch {
		var d dto.Metric
		if err := metric.Write(&d); err != nil {
			continue
		}
		for _, lp := range d.GetLabel() {
			if lp.GetName() == "layer" && lp.GetValue() == "5" {
				l5val = d.GetGauge().GetValue()
			}
		}
	}
	if l5val != 50 {
		t.Errorf("MemoriesTotal{layer=5} = %v, want 50", l5val)
	}
}
