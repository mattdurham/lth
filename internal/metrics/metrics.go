// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package metrics defines and registers all Prometheus metrics for lth.
package metrics

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for lth.
type Metrics struct {
	MemoriesTotal        *prometheus.GaugeVec
	GraphEdgesTotal      prometheus.Gauge
	CompactionsTotal     *prometheus.CounterVec
	LLMRequestsTotal     *prometheus.CounterVec
	LLMRequestDuration   *prometheus.HistogramVec
	EmbedRequestsTotal   *prometheus.CounterVec
	EmbedRequestDuration *prometheus.HistogramVec
	SearchesTotal        *prometheus.CounterVec
	SearchDuration       prometheus.Histogram
	WatcherMessagesTotal prometheus.Counter
	WatcherFilesWatched  prometheus.Gauge
	SyncPushedTotal      *prometheus.CounterVec
	SyncPulledTotal      *prometheus.CounterVec
	SyncDurationSeconds  *prometheus.HistogramVec

	// Per-source ingestion counters.
	WatcherIngestedTotal  *prometheus.CounterVec // label: path
	MarkdownIngestedTotal *prometheus.CounterVec // label: dir
	IssuesIngestedTotal   *prometheus.CounterVec // label: repo
	IssuesLastSync        *prometheus.GaugeVec   // label: repo — Unix timestamp of last successful sync
}

// New creates and registers all lth metrics with the given registry.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		MemoriesTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lth_memories_total",
			Help: "Number of active memories by layer.",
		}, []string{"layer"}),

		GraphEdgesTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lth_graph_edges_total",
			Help: "Number of edges in the memory graph.",
		}),

		CompactionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_compactions_total",
			Help: "Number of compaction operations by path.",
		}, []string{"path"}),

		LLMRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_llm_requests_total",
			Help: "Number of LLM API requests.",
		}, []string{"provider", "operation", "status"}),

		LLMRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lth_llm_request_duration_seconds",
			Help:    "LLM request latency in seconds.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		}, []string{"provider", "operation"}),

		EmbedRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_embedding_requests_total",
			Help: "Number of embedding API requests.",
		}, []string{"provider", "status"}),

		EmbedRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lth_embedding_request_duration_seconds",
			Help:    "Embedding request latency in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		}, []string{"provider"}),

		SearchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_searches_total",
			Help: "Number of search operations by type.",
		}, []string{"type"}),

		SearchDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "lth_search_duration_seconds",
			Help:    "Search latency in seconds.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		}),

		WatcherMessagesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lth_watcher_messages_ingested_total",
			Help: "Number of JSONL messages ingested by the watcher.",
		}),

		WatcherFilesWatched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "lth_watcher_files_watched_total",
			Help: "Number of files currently being watched.",
		}),

		SyncPushedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_sync_pushed_total",
			Help: "Number of memories pushed to the sync server.",
		}, []string{"status"}),

		SyncPulledTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_sync_pulled_total",
			Help: "Number of memories pulled from the sync server.",
		}, []string{"status"}),

		SyncDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lth_sync_duration_seconds",
			Help:    "Duration of sync push/pull operations in seconds.",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		}, []string{"operation"}),

		WatcherIngestedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_watcher_ingested_total",
			Help: "Memories ingested by the session/JSONL watcher, by watched path.",
		}, []string{"path"}),

		MarkdownIngestedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_markdown_ingested_total",
			Help: "Memories ingested by the markdown watcher, by directory.",
		}, []string{"dir"}),

		IssuesIngestedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lth_issues_ingested_total",
			Help: "Memories ingested by the issues watcher, by repo.",
		}, []string{"repo"}),

		IssuesLastSync: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "lth_issues_last_sync_timestamp",
			Help: "Unix timestamp of the last successful issues sync, by repo.",
		}, []string{"repo"}),
	}

	reg.MustRegister(
		m.MemoriesTotal,
		m.GraphEdgesTotal,
		m.CompactionsTotal,
		m.LLMRequestsTotal,
		m.LLMRequestDuration,
		m.EmbedRequestsTotal,
		m.EmbedRequestDuration,
		m.SearchesTotal,
		m.SearchDuration,
		m.WatcherMessagesTotal,
		m.WatcherFilesWatched,
		m.SyncPushedTotal,
		m.SyncPulledTotal,
		m.SyncDurationSeconds,
		m.WatcherIngestedTotal,
		m.MarkdownIngestedTotal,
		m.IssuesIngestedTotal,
		m.IssuesLastSync,
	)
	return m
}
