# internal/memory — Invariants

1. `Store` is idempotent on `content_hash`: storing the same content twice returns the existing `*Memory` with the same ID and creates no duplicate rows.
2. `importance` defaults to `5.0` when the LLM is unavailable; a background goroutine updates it later.
3. `Search` always returns at most `TopK` results regardless of the candidate pool size.
4. L1 memories always have `decay_rate=0`; they never decay.
5. Ebbinghaus stability increases monotonically with each successful `MarkAccessed` call.
6. `SoftDelete` does not hard-delete any row; it only sets `compacted_at`.
7. Returned `*Memory` from `Store` reflects the state at store time (importance may be 5.0 before async update).
8. `MemoryStore` satisfies the `Store` interface at compile time.
9. The composite search score is: `α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m)` with λ=0.9995/hour (half-life ~58 days). `λ` is a single package-level constant (`scoringLambda`) applied uniformly to every memory's time score; it is NOT derived from the memory's own `row.DecayRate`/`row.Stability` fields -- see invariant 19.
10. Auto-links are created via `graph.AutoLink` synchronously inside `Store` for any layer that produces a non-empty embedding. AutoLink is not restricted to L5.
11. `Store` async-extracts tags via LLM and sets `attrs["tags"]` as a comma-separated list of up to 5 lowercase tags. Tags are set concurrently with importance scoring in the same goroutine.
12. `parseTags` strips markdown code fences, lowercases all tags, and caps at 5 entries; returns "" on any parse error.
13. `valence` defaults to 0.0 at store time; the async goroutine scores it via LLM and calls `UpdateValence` (which sets `valence_scored=1`) within 10 seconds.
14. The composite search score formula is: `α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m) + δ·(v×|v|)` where δ=0.15 and `v×|v|` is a sign-preserving square that amplifies extremes (+1.0→+1.0, +0.5→+0.25, 0.0→0.0, -0.5→-0.25, -1.0→-1.0).
15. `BackfillValence` runs as a background daemon goroutine; it finds memories with `valence_scored=0`, scores them via LLM in batches of configurable size, and sleeps 5 seconds between batches to avoid rate limits.
16. After L5→L4 compaction creates an L4 memory, it should be scored for valence based on the full cluster context, not individual messages.
17. L4 memories with `|valence| < ValenceCompactionMin` (default 0.15) are considered neutral noise and should be skipped during L4→L3 clustering. The non-linear score `|v×|v|| < ValenceCompactionMin²` naturally handles this threshold.
18. `attrs["created_at"]` (RFC3339), if present, overrides the stored row's `CreatedAt` (and thus the search time score's `Δt`) instead of insertion time. The key is popped from `attrs` before the remaining attributes are persisted via `SetAttributes`, so it never appears as a literal stored attribute. An unparseable value makes `Store` return an error; there is no silent fallback to now.
19. `row.DecayRate` and `row.Stability` are maintained per-memory (initialized at `Store` time; updated by `Get`'s Ebbinghaus stability increment) but `scoreMemory` does not read either field -- the search time score always uses the single global `scoringLambda` constant. Per-row decay/stability are tracked for potential future use and are not currently load-bearing for ranking.
20. `Store` copies its `attrs` parameter internally before mutating it (e.g. popping `created_at`); the caller's original map is never modified.
21. `BackfillEmbeddings` retries any embed failure on the next batch, EXCEPT `vector.ErrPayloadTooLarge`, which is treated as permanent: the memory is soft-deleted (`compacted_at` set, per invariant 6) and the caller-supplied `onGiveUp` callback (may be nil) is invoked once. This prevents a memory whose content is too large for the embedder even after truncation from being re-attempted every batch forever.
