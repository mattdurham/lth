# internal/memory — Design Notes

## 1. Composite Scoring Formula

*Added: 2026-05-14*

**Decision:** `score = α·exp(-λ·Δt) + β·importance/10 + γ·cosine(q,m)` with λ=0.9995/hour (half-life ~58 days; changed from the original 0.995/hour ~6-day half-life in `607e70d`).

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

**Consequence:** `Get`'s Ebbinghaus stability update computes and persists `actual decay = base /
stability` into `row.DecayRate` on every access, so the field itself does reflect frequently-
accessed memories becoming more persistent. However, `scoreMemory` (search_impl.go) does not
currently read `row.DecayRate` or `row.Stability` -- the search time score uses the single global
`scoringLambda` constant for every memory regardless of layer or access history. So today,
per-row decay/stability affects nothing a user can observe via search ranking; it's tracked state
without a consumer. Found by adversarial review 2026-07-11 (`internal/memory/SPECS.md` invariant 19).

---

## 4. Async Tag Extraction

*Added: 2026-05-14*

**Decision:** Tag extraction runs in the same async goroutine as importance scoring, immediately
after scoring completes. Tags are stored in `memory_attributes` with key `"tags"` as a
comma-separated string.

**Rationale:** Tags enable structured filtering (`lth search --tags go,error-handling`) without
requiring schema changes — the existing `memory_attributes` table stores them as KV pairs.
Running in the same goroutine keeps the concurrency model simple: one goroutine per stored memory,
two LLM calls within it.

**Consequence:** Tags are not available immediately after `Store` returns. The `Attrs["tags"]`
field will be empty in the returned Memory until the async goroutine completes. Search results
from already-tagged memories support tag filtering immediately.

---

## 5. Valence: Non-Linear Scoring and Outcome Polarity

*Added: 2026-05-14*

**Decision:** Add `valence` (float32, -1.0 to +1.0) to Memory. Use a sign-preserving square `v×|v|` for the score contribution rather than a linear term. Weight δ=0.15. `valence_scored` bool tracks whether LLM has evaluated the memory.

**Rationale:** A linear weight would make near-zero memories (0.1, -0.1) contribute meaningfully, creating noise. The non-linear `v×|v|` maps: +1.0→+1.0, +0.5→+0.25, 0.0→0.0, -0.5→-0.25, -1.0→-1.0. This naturally suppresses neutral noise while amplifying strongly positive/negative outcomes. The `valence_scored` flag distinguishes "not evaluated yet" (0.0, false) from "truly neutral" (0.0, true), preventing false negatives on backfill filtering.

**Consequence:** Three LLM calls per Store (importance, tags, valence). The async goroutine timeout is 10 seconds — sufficient for fast models. Backfill goroutine in daemon handles pre-existing unscored memories. The `ValenceCompactionMin=0.15` threshold means only memories with `|v|≥0.15` (mapped through non-linear: `|v×|v||≥0.0225`) are considered worth compacting upward.

---

## 6. Valence Backfill Goroutine

*Added: 2026-05-14*

**Decision:** `BackfillValence` runs in the daemon as a background goroutine, polling `ListUnscored` every 5 seconds in batches of 10.

**Rationale:** Memories stored before this feature existed have `valence_scored=0`. Rather than a one-time migration (which could block startup), lazy backfilling via a low-priority goroutine processes them incrementally without impacting user-facing operations.

**Consequence:** On large databases, full backfill may take minutes. During this window, search scores for old memories will lack valence contribution (0.0 contribution = neutral, no bias). `slog.Info` logs progress for observability.

---

## 7. Valence Backfill Give-Up After Repeated Parse Failures

*Added: 2026-08-05*

**Decision:** Add a `valence_attempts` counter column. On each valence LLM response that fails to parse, call `db.IncrementValenceAttempts`, which increments the counter and, once it reaches `maxValenceAttempts` (3), also sets `valence_scored=1` — giving up with the default neutral valence (0.0).

**Rationale:** Some memories' content is a bare reference/identifier with no descriptive text (e.g. "SPEC-VI-18 through SPEC-VI-22"), so the LLM correctly and consistently refuses to rate an outcome. Before this change, `ListUnscored` (ordered oldest-first) kept resurfacing these same rows every poll interval forever, since a parse error never set `valence_scored`. This burned an LLM call per retry indefinitely for memories that could never succeed — the same failure mode `BackfillEmbeddings` already handles for oversized payloads (invariant 21) via soft-delete.

**Consequence:** A memory that fails to parse 3 times ends up with `valence=0.0, valence_scored=1` — indistinguishable from a memory the LLM genuinely rated as neutral. This is an acceptable trade-off since these are already edge-case, low-signal memories. LLM *transport* errors (timeouts, rate limits) do not count toward `valence_attempts` — only parse failures, since those are the ones that reproduce deterministically against the same content.
