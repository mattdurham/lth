# internal/compactor — Test Scenarios

## TestCompactL5toL4
**Scenario:** 55 L5 memories trigger L5→L4 compaction.
**Setup:** Insert 55 L5 memories via store; mock LLM returns "summary".
**Assertions:** RunOnce returns L5toL4 > 0; L5 memories soft-deleted.

## TestCompactL4toL3
**Scenario:** Cluster of similar L4 memories triggers L4→L3.
**Setup:** Insert 5 L4 memories with similar embeddings.
**Assertions:** At least one L3 memory created.

## TestCompactL3toL2
**Scenario:** High-importance, high-access L3 memory triggers L3→L2.
**Setup:** Insert L3 with access_count >= 10 and importance > 7.0.
**Assertions:** L2 created; L3 not soft-deleted.

## TestLLMFailure
**Scenario:** LLM failure causes skip, not crash.
**Setup:** Mock LLM returns error; 55 L5 memories.
**Assertions:** RunOnce returns nil error; no new memories created.
