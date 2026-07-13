// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	serverReg *prometheus.Registry

	pushRequests     *prometheus.CounterVec
	pullRequests     *prometheus.CounterVec
	obsRequests      *prometheus.CounterVec
	memoriesAccepted prometheus.Counter
	memoriesSkipped  prometheus.Counter
	requestDuration  *prometheus.HistogramVec
)

func init() {
	serverReg = prometheus.NewRegistry()
	serverReg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	pushRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lth_push_requests_total",
		Help: "Total push requests by status.",
	}, []string{"status"})

	pullRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lth_pull_requests_total",
		Help: "Total pull requests by status.",
	}, []string{"status"})

	obsRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lth_observations_requests_total",
		Help: "Total observation requests by status.",
	}, []string{"status"})

	memoriesAccepted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "lth_memories_accepted_total",
		Help: "Total memories accepted by push handler.",
	})

	memoriesSkipped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "lth_memories_skipped_total",
		Help: "Total memories skipped (duplicate) by push handler.",
	})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lth_request_duration_seconds",
		Help:    "Request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"handler"})

	serverReg.MustRegister(
		pushRequests, pullRequests, obsRequests,
		memoriesAccepted, memoriesSkipped, requestDuration,
	)
}

func metricsHandler() http.Handler {
	return promhttp.HandlerFor(serverReg, promhttp.HandlerOpts{})
}
func instrumentHandler(name string, counter *prometheus.CounterVec, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.WithLabelValues("ok").Inc()
		timer := prometheus.NewTimer(requestDuration.WithLabelValues(name))
		defer timer.ObserveDuration()
		h.ServeHTTP(w, r)
	})
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	rc.body = append(rc.body, b...)
	return rc.ResponseWriter.Write(b)
}

func pushMetricsHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := &responseCapture{ResponseWriter: w}
		pushRequests.WithLabelValues("ok").Inc()
		timer := prometheus.NewTimer(requestDuration.WithLabelValues("push"))
		defer timer.ObserveDuration()
		h.ServeHTTP(rc, r)
		resp := pushResponse{}
		if json.Unmarshal(rc.body, &resp) == nil {
			memoriesAccepted.Add(float64(resp.Accepted))
			memoriesSkipped.Add(float64(resp.Skipped))
		}
	})
}

type responseCapture struct {
	http.ResponseWriter
	body []byte
}
