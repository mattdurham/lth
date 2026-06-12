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

## 5. L5→L4 Semantic Clustering

*Added: 2026-05-14*

**Decision:** Replace time-window chunking with cosine-similarity clustering for L5→L4. Memories without embeddings (or unclustered memories when count >= windowSize) fall back to chronological windowing.

**Rationale:** Time-based windows don't group semantically related memories — "bug A in file1" and "bug A in file2" would only merge if they happened in the same time window. Cosine clustering ensures semantically similar observations compact together regardless of creation time, producing more coherent L4 episodic memories. The fallback windowing path preserves the original behaviour for the case where the embedder is unavailable and also for any memories with embeddings that don't form clusters yet count/age forces a compaction run.

**Consequence:** L5 memories without embeddings fall back to time-windowing. Semantically distinct L5 memories that don't form clusters (min size = 2, threshold = 0.75 by default) also fall back to windowing when the count/age trigger fires. A shared `allPairwiseSimilarThreshold` helper in `cluster.go` is used by both L4→L3 and L5→L4 paths.

---

## 6. Auto-Seed L2/L3 from L5 History

*Added: 2026-05-14*

**Decision:** Added a `compactSeed` path that runs before normal compaction in `RunOnce`. When L2 or L3 layers are sparse (counts below `SeedMinL2`/`SeedMinL3`) and L5 has crossed the compaction threshold, the compactor clusters L5 memories by cosine similarity and asks the LLM to directly infer behavioral rules (L2) and skills (L3) from each coherent cluster.

**Rationale:** Without seeding, L2/L3 layers only fill through the normal multi-hop promotion chain (L5→L4→L3→L2), which requires significant memory accumulation over time. New installations start with empty upper layers, making semantic search over behavioral patterns useless until weeks of data accumulate. Auto-seeding bootstraps the upper layers immediately from raw history, avoiding the need for manual `lth store --layer 2` commands for initial setup. Using semantic clusters (same algorithm as L5→L4) rather than random sampling ensures each LLM call receives topically coherent memories, producing specific and useful output rather than vague generalizations.

**Consequence:** L5 memories are never deleted by seeding (read-only path). The `SeedSample` config field caps the number of clusters processed per run to bound LLM cost. `CompactionReport.SeedL2` and `SeedL3` expose seed counts for CLI display and logging.

---

## 2. LLM via Interface Injection

*Added: 2026-05-14*

**Decision:** The compactor accepts an `llm.LLM` interface, not a concrete type. The `memory.Store` is also injected as an interface.

**Rationale:** Allows testing with mock implementations. The compactor has no direct DB access — all reads/writes go through `memory.Store`.

**Consequence:** The compactor is a pure orchestration layer. This simplifies testing significantly.

---

## 7. L5 Cluster Size Cap (Token Budget)

*Added: 2026-06-11*

**Decision:** `summarizeCluster` enforces a character-budget cap (`L5MaxClusterChars`, default 80,000 ≈ 20k tokens) on the cluster content fed to the LLM. When a cluster exceeds the budget, memories are sampled evenly across the cluster (preserving first and last to span the time range), and a note is added to the prompt indicating that sampling occurred. All original L5 memories are still soft-deleted on success.

**Rationale:** Cosine-similarity clustering produces unbounded cluster sizes. A single long Claude Code session can generate thousands of near-duplicate observations that all cluster together at any reasonable threshold. Before this cap, such clusters produced 200k+ token prompts that exceeded both local model context windows (32k) and even Anthropic Haiku's 200k limit, causing every L5→L4 compaction run to fail with no L5 memories ever being archived. The threshold knob alone cannot fix this because the offending memories ARE highly similar by design.

**Consequence:** L5→L4 compaction no longer silently fails on huge clusters. The resulting L4 summary is necessarily coarser when sampled (e.g. 80 of 5,000 observations), but a coarse summary is strictly better than a missing one. The minimum of 2 retained memories means a pathological budget setting can still produce valid input; real context-overflow on truly small clusters still surfaces via the LLM chain's error path. Users can disable the cap entirely with `l5_max_cluster_chars: 0` for the original unbounded behavior.
