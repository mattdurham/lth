# internal/metrics — Design Notes

## 1. Isolated Prometheus registry

*Added: 2026-05-14*

**Decision:** Use a caller-supplied `*prometheus.Registry` rather than `prometheus.DefaultRegisterer`.

**Rationale:** The default registry is a global singleton that causes registration panics in
tests if the same binary runs multiple test cases registering the same metric names. An
isolated registry per test (or per daemon process) is safe and composable.

**Consequence:** Callers (e.g. the daemon in cmd/lth/watch.go) are responsible for creating
the registry and passing it to both `metrics.New` and `metrics.NewServer`.

---

## 2. Wrapper instrumentation via interface injection

*Added: 2026-05-14*

**Decision:** Instrument LLM and Embedder calls by wrapping them at construction time in
`newDaemonComponents`, rather than embedding metrics calls inside `internal/llm` or
`internal/vector`.

**Rationale:** Avoids a dependency cycle (metrics → llm → metrics). The wrapper implements
the same interface so the rest of the call chain is unaware of instrumentation. This is the
standard "transparent decorator" pattern.

**Consequence:** Any LLM or Embedder implementation added in future must be wrapped the same
way at the construction site in `cmd/lth/watch.go`.

---

## 4. Conditional /v1/traces route registration

*Added: 2026-05-19*

**Decision:** Register the `/v1/traces` route in `buildMux()` only when `s.receiver != nil`, using a direct `if` check rather than a `withReceiver` guard wrapper.

**Rationale:** The traces receiver is optional — the metrics server should be usable without an OTLP ingest path. A nil guard in buildMux keeps the route absent entirely (404) when no receiver is configured, which is cleaner than registering a handler that always returns 503.

**Consequence:** `SetReceiver` must be called before `Start` or `TestHandler` to activate the endpoint. Late registration after Start is not supported.

---

## 3. TestHandler for HTTP testing

*Added: 2026-05-14*

**Decision:** Expose `Server.TestHandler() http.Handler` to allow tests to use
`httptest.NewServer` without binding a real TCP port.

**Rationale:** Tests that bind real ports are fragile (port conflicts in CI) and slow. The
`httptest` approach uses in-process pipes and is idiomatic in Go.

**Consequence:** The internal mux construction is extracted to `buildMux()` so it is shared
between `Start` and `TestHandler`. Both paths are identical.
