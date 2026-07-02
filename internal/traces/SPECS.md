# internal/traces — Invariants

1. The queue is bounded at 10,000 spans; excess spans are dropped with a warning log — the HTTP handler never blocks.
2. All spans are stored at L5 (raw observations) with attrs: trace_id, span_id, parent_span_id, service_name, source=otlp.
3. same_trace edges connect spans with the same trace_id when a local DB and graph are available; at most 50 edges are created per span to prevent fan-out. Proxy-backed receivers store spans without creating local graph edges.
4. ServeHTTP only accepts POST; other methods return 405.
5. The /v1/traces endpoint is disabled (returns 404) unless SetReceiver is called on the metrics.Server.
6. OTLP trace IDs and span IDs are decoded from base64 to lowercase hex strings before storage; decoding failures fall back to the original string.
7. No protobuf dependency is introduced; OTLP JSON encoding is decoded with encoding/json via hand-written structs.
8. ServeHTTP limits request body to 4 MB to prevent memory exhaustion.
