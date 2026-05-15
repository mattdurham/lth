# lth — Project-Level Test Scenarios

## Integration Test: Full E2E Pipeline

**Scenario:** Store memories across layers, search, get, compact, verify stats.

**Setup:**
- Temp SQLite DB
- Mock embedder (deterministic FNV-based vectors)
- Mock LLM (returns fixed strings)

**Assertions:**
1. Store 5 memories (L3, L4, L5) — all succeed, unique IDs
2. Store duplicate content — returns same ID, no new row
3. Search for query — top result is semantically closest memory
4. Get by ID — all fields match stored content
5. Compact (L5→L4) — L5 count decreases, L4 count increases
6. Stats — layer counts correct

## Unit Test Coverage Goals

| Package | Target |
|---------|--------|
| internal/config | 100% (pure functions) |
| internal/vector | 100% (pure functions) |
| internal/db | 90% |
| internal/graph | 80% |
| internal/memory | 80% |
| internal/compactor | 80% |
| internal/watcher | 70% |
| pkg/lth | 70% |
