# internal/memory — Invariants

1. `Store` is idempotent on `content_hash`: storing the same content twice returns the existing `*Memory` with the same ID and creates no duplicate rows.
2. `importance` defaults to `5.0` when the LLM is unavailable; a background goroutine updates it later.
3. `Search` always returns at most `TopK` results regardless of the candidate pool size.
4. L1 memories always have `decay_rate=0`; they never decay.
5. Ebbinghaus stability increases monotonically with each successful `MarkAccessed` call.
6. `SoftDelete` does not hard-delete any row; it only sets `compacted_at`.
7. Returned `*Memory` from `Store` reflects the state at store time (importance may be 5.0 before async update).
8. `MemoryStore` satisfies the `Store` interface at compile time.
9. The composite search score is: `α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m)` with λ=0.995/hour.
10. Auto-links are created via `graph.AutoLink` synchronously inside `Store` for any layer that produces a non-empty embedding. AutoLink is not restricted to L5.
11. `Store` async-extracts tags via LLM and sets `attrs["tags"]` as a comma-separated list of up to 5 lowercase tags. Tags are set concurrently with importance scoring in the same goroutine.
12. `parseTags` strips markdown code fences, lowercases all tags, and caps at 5 entries; returns "" on any parse error.
