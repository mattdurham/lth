# internal/memory — Design Notes

## 1. Composite Scoring Formula

*Added: 2026-05-14*

**Decision:** `score = α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m)` with λ=0.995/hour.

**Rationale:** Balances recency (exponential decay), curated importance (1-10 LLM score), and
semantic relevance (cosine similarity). Equal weighting (1/3 each) is the default, allowing
callers to bias toward any dimension.

**Consequence:** L1 memories (decay_rate=0) are effectively permanent — their time component
doesn't decay. L5 memories decay quickly (decay_rate=0.5).

---

## 2. Async Importance Scoring

*Added: 2026-05-14*

**Decision:** The LLM importance call runs in a background goroutine. The returned Memory has
importance=5.0 until the goroutine completes.

**Rationale:** LLM calls can take 1-5 seconds. Making them synchronous would block `Store` and
degrade user experience. The default of 5.0 is neutral — it won't bias search results dramatically.

**Consequence:** There is a brief window where the stored memory has importance=5.0. For low-latency
workflows, callers should use FTS5 or vector search rather than importance-based ranking immediately
after store.

---

## 3. Layer Decay Rates

*Added: 2026-05-14*

**Decision:** L1=0.0, L2=0.01, L3=0.05, L4=0.1, L5=0.5 (base decay rates per layer).

**Rationale:** Higher layers are more important and persistent. L1 (axioms) never decay. L5 (raw
observations) decay quickly to avoid cluttering search results.

**Consequence:** Ebbinghaus stability modifies these rates: actual decay = base / stability.
Frequently accessed memories become more persistent over time.
