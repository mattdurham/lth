# pkg/lth — Test Scenarios

## TestNewClientClose

**Scenario:** Open and close client without errors.

**Setup:** Temp DB, config with mock embedder URL.

**Assertions:** No error on NewClient or Close.

## TestStoreAndGet

**Scenario:** Store memory, retrieve by ID.

**Setup:** NewClient with temp DB; Store content; Get by returned ID.

**Assertions:** Content and Layer match.

## TestStats

**Scenario:** Store memories across layers; verify stats.

**Setup:** Store L3 and L5 memories; call Stats.

**Assertions:** ByLayer counts correct.
