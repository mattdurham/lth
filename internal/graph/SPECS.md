# internal/graph — Invariants

1. After `LoadAll` returns nil, the in-memory adjacency cache is consistent with all edges in the DB.
2. `AddEdge` atomically updates both the DB (via `db.InsertEdge`) and the in-memory cache.
3. `PPR` runs exactly `iters` iterations; it never reads from the DB during iteration.
4. `AutoLink` only creates `relates_to` edges with cosine similarity >= 0.75.
5. Soft-deleted nodes (compacted_at != NULL) remain in the adjacency cache for lineage traversal.
6. The adjacency cache must be rebuilt via `LoadAll` after any compaction batch completes. `Compactor.RunOnce` calls `g.LoadAll` when any promotion occurred (L5toL4, L4toL3, or L3toL2 > 0).
7. `Neighbors` is safe to call concurrently from multiple goroutines.
8. The adjacency cache stores edges in both directions: an edge A→B means B appears in A's list and A appears in B's list.
