# internal/compactor — Design Notes

## 1. Three-Path Compaction Design

*Added: 2026-05-14*

**Decision:** Three independent compaction paths: L5→L4 (window summarization), L4→L3 (cosine clustering), L3→L2 (pattern recognition). Each path has independent trigger conditions and runs sequentially in `RunOnce`.

**Rationale:** The paths have different triggers and semantics. Running them independently allows each to proceed even when another fails. Sequential execution avoids race conditions between paths.

**Consequence:** A single `RunOnce` call may compact multiple layers in one invocation. The order is always L5→L4, L4→L3, L3→L2.

---

## 3. Centroid Normalization Bug Fix

*Added: 2026-05-14*

**Decision:** Fixed `computeCentroid` to use `1.0 / math.Sqrt(norm)` (L2 norm) instead of `1.0 / norm` (which was dividing by the sum-of-squares, not the norm).

**Rationale:** The function computes `norm = sum(v[i]^2)` which equals `‖v‖²`. The original code used `1/‖v‖²` producing a vector of magnitude `1/‖v‖`, not a unit vector. The correct formula is `1/‖v‖ = 1/sqrt(‖v‖²)`.

**Consequence:** All L4→L3 cluster centroids are now correctly unit-normalized. Cosine comparisons using the centroid were already scale-invariant, so this primarily matters if the centroid is ever stored or compared via direct distance metrics.

---

## 4. Pairwise Cosine Clustering Fix

*Added: 2026-05-14*

**Decision:** `findClusters` now validates cosine similarity against ALL current cluster members (pairwise) rather than only the pivot (single-linkage).

**Rationale:** The original single-linkage implementation allowed two memories `j` and `k` to be co-clustered if both were close to pivot `i`, even if `cosine(j,k) < 0.85`. This violated SPECS.md invariant 7 and could produce incoherent L3 summaries.

**Consequence:** Clusters are now strictly pairwise-similar. Cluster sizes may be smaller, requiring more L4 memories to accumulate before a cluster of `L4ClusterSize` forms. The algorithm is O(n²) per cluster but acceptable for typical L4 pool sizes.

---

## 2. LLM via Interface Injection

*Added: 2026-05-14*

**Decision:** The compactor accepts an `llm.LLM` interface, not a concrete type. The `memory.Store` is also injected as an interface.

**Rationale:** Allows testing with mock implementations. The compactor has no direct DB access — all reads/writes go through `memory.Store`.

**Consequence:** The compactor is a pure orchestration layer. This simplifies testing significantly.
