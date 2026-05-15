# internal/graph — Design Notes

## 1. In-Memory Adjacency Cache

*Added: 2026-05-14*

**Decision:** Maintain an in-memory bidirectional adjacency map as a cache over the `memory_edges`
SQLite table. The cache is populated by `LoadAll` and updated incrementally by `AddEdge`.

**Rationale:** PPR traversal runs entirely in-memory on this cache without DB reads during iteration.
At 10,000 nodes × 5 edges × ~80 bytes/entry = ~4MB, the cache fits easily in memory. The tradeoff
is that stale cache state is possible after compaction (mitigated by the caller rebuilding via `LoadAll`).

**Consequence:** After batch compaction, callers must call `LoadAll` to rebuild the cache.
Incremental `AddEdge` keeps the cache consistent for normal store operations.

---

## 2. PPR Parameters: d=0.85, iters=20

*Added: 2026-05-14*

**Decision:** Use damping factor d=0.85 and 20 iterations as the default PPR parameters.

**Rationale:** d=0.85 is the canonical PageRank damping factor. 20 iterations is sufficient for
convergence on sparse graphs (typical for a memory database with 5 edges per node average).
The caller can pass custom values.

**Consequence:** PPR scores are approximations, not exact solutions. For dense graphs, more
iterations may be needed. The current defaults are tuned for lth's expected scale.

---

## 3. Zettelkasten Threshold: cosine >= 0.75

*Added: 2026-05-14*

**Decision:** Auto-link creates `relates_to` edges only when cosine similarity >= 0.75.

**Rationale:** 0.75 is a relatively strict threshold that ensures only highly related memories
are linked. Lower thresholds create too many low-quality edges that degrade PPR traversal quality.
The threshold is checked after an exact cosine computation on the top-5 KNN candidates from vec0.

**Consequence:** Memories with cosine < 0.75 to all existing memories will have no auto-links.
Users can add manual edges via the graph API.
