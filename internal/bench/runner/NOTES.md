## 1. Separate binary — no lth dependency

*Added: 2026-05-15*

**Decision:** `cmd/bench` is a standalone binary; `internal/bench/` never imports `pkg/lth` or other lth-internal packages.

**Rationale:** Avoids coupling the benchmark harness to lth's daemon/DB/config dependencies. The harness should build and run without lth infrastructure running.

**Consequence:** Any shared utilities must be copied or placed in a new shared package.

## 2. Sequential runner — no goroutines

*Added: 2026-05-15*

**Decision:** `runBench` iterates with plain `for` loops; no goroutines, channels, or `sync.WaitGroup`.

**Rationale:** Explicit team-lead constraint. Sequential execution is easier to debug, and the bottleneck is Claude latency, not CPU.

**Consequence:** Total runtime = sum of individual run times. 42 problems × 3 approaches × ~5 min = several hours for full run.
