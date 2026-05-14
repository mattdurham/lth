# internal/compactor — Invariants

1. Compaction is never destructive: soft delete only via `compacted_at` tombstone; no hard deletes.
2. `RunOnce` is idempotent: running twice in succession produces no additional changes if triggering conditions are not re-met.
3. LLM failure causes skip-not-crash: each window/cluster is independent; one LLM failure does not abort the entire run.
4. L2→L1 promotion is never automatic; no code in this package creates L1 memories.
5. `Run` blocks until the context is cancelled; it never returns nil except on context cancellation.
5a. `RunOnce` calls `graph.LoadAll` after any successful promotion (L5→L4, L4→L3, or L3→L2 count > 0) to keep the adjacency cache consistent with the DB.
6. `compactL5toL4` clusters L5 memories by pairwise cosine similarity (threshold: `L5ClusterThreshold`, min cluster size: `L5MinClusterSize`). The count/age trigger still applies: compaction only runs when `CountByLayer(5) > L5Threshold` OR the oldest L5 memory is older than `L5MaxAge` hours. Memories without embeddings (and unclustered memories with embeddings when count >= windowSize) fall back to chronological windowing.
6a. L5 memories without embeddings are only compacted via fallback windowing, never via cosine clustering.
7. `compactL4toL3` requires a cluster of size >= `L4ClusterSize` with **pairwise** cosine >= 0.85. `findClusters` uses greedy expansion with full pairwise validation: a candidate is only added to a cluster if its cosine similarity to every existing cluster member is >= 0.85.
8. `compactL3toL2` requires `access_count >= L3EpisodesMin AND importance > L3ImportanceMin`.
9. `compactSeed` runs before normal compaction in `RunOnce`. It triggers when `L5 count >= L5Threshold` AND (`L2 count < SeedMinL2` OR `L3 count < SeedMinL3`). It clusters L5 memories by cosine similarity (reusing `findL5Clusters`) and calls the LLM once per cluster to infer behavioral rules (L2) and skills (L3). It never modifies or deletes L5 memories. At most `SeedSample` clusters are processed per run. `CompactionReport.SeedL2` and `CompactionReport.SeedL3` record how many memories were created.
