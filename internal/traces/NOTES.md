# internal/traces — Design Notes

## 1. OTLP HTTP Trace Ingestion

*Added: 2026-05-19*

**Decision:** Accept OTLP JSON (not protobuf) at POST /v1/traces, buffer in a 10k-capacity channel, and process asynchronously.

**Rationale:** HTTP handlers must return quickly. Span storage involves embedding (100ms+) so synchronous processing would time out OTLP exporters. The channel decouples ingestion rate from processing rate.

**Consequence:** Under extreme load, spans are dropped and logged. The exporter sees a 200 response but the span may not be stored. This is acceptable for observability data — partial data is better than backpressure.

## 2. Edge fanout cap of 50 per span

*Added: 2026-05-19*

**Decision:** After storing a span, query db.GetMemIDsByAttr("trace_id", id) and create at most 50 same_trace edges regardless of how many prior spans exist in the same trace.

**Rationale:** A trace with 1,000 spans would require 999 AddEdge calls for the last span. At ~1 ms per edge write that is ~1 second of DB time for a single span. The cap keeps the worst-case processing time bounded.

**Consequence:** For traces with >50 spans, the graph is not fully connected. PPR traversal can still reach all nodes via intermediate hops since edges are bidirectional in the adjacency cache.

## 3. OTLP JSON field mapping

*Added: 2026-05-19*

**Decision:** Parse only the fields needed for storage: traceId, spanId, parentSpanId, name, startTimeUnixNano, endTimeUnixNano, attributes (span-level), and resource.attributes (for service.name). All other OTLP fields are ignored.

**Rationale:** The OTLP proto-JSON schema is large. Parsing only what we store keeps the code minimal and avoids coupling to OTLP schema evolution for unused fields.

**Consequence:** Span events, links, and instrumentation scope metadata are discarded. If future use cases require them, extend otlpSpan and Span accordingly.
