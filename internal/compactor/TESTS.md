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

## TestFindL5Clusters
**Scenario:** Semantic clustering groups high-cosine memories; ignores those without embeddings.
**Setup:** Two groups of 3 near-identical unit vectors plus 1 memory with no embedding.
**Assertions:** Returns exactly 2 clusters; each cluster >= minSize; no member has nil embedding.

## TestFindL5ClustersMinSize
**Scenario:** Singleton groups below minSize are excluded.
**Setup:** 3 dissimilar memories, minSize=2.
**Assertions:** No cluster returned has size < 2.

## TestAllPairwiseSimilarThreshold
**Scenario:** Shared threshold helper correctness.
**Setup:** Identical vectors (cosine 1.0) and opposite vectors (cosine -1.0).
**Assertions:** Returns true for identical; false for opposite.

## TestCompactL5toL4SemanticClustering
**Scenario:** L5 memories with similar embeddings compact via semantic clustering.
**Setup:** similarEmbedder (all identical vectors); 6 L5 memories; low threshold; trigger at 5.
**Assertions:** L5toL4 > 0; L4 count > 0 after RunOnce.

## TestCompactL5toL4FallbackNoEmbeddings
**Scenario:** L5 memories with no embeddings compact via fallback windowing.
**Setup:** noEmbedEmbedder (always errors); 20 L5 memories; trigger at 5.
**Assertions:** L5toL4 > 0 via fallback windowing.

## TestCompactSeedNoOp
**Scenario:** compactSeed is a no-op when L2 and L3 are already at or above thresholds.
**Setup:** Pre-populate L2 >= SeedMinL2 and L3 >= SeedMinL3; L5 above threshold.
**Assertions:** compactSeed returns l2=0, l3=0.

## TestCompactSeedL5BelowThreshold
**Scenario:** compactSeed is a no-op when L5 count is below L5Threshold.
**Setup:** L5 count = L5Threshold-1; L2/L3 empty.
**Assertions:** compactSeed returns l2=0, l3=0.

## TestCompactSeedStoresL2AndL3
**Scenario:** Seeding creates L2 rules and L3 skills when layers are sparse and L5 is at threshold.
**Setup:** similarEmbedder so memories cluster; LLM returns valid JSON with rules and skills; L5 > threshold.
**Assertions:** l2n > 0, l3n > 0; DB counts match returned counts.

## TestCompactSeedLLMFailure
**Scenario:** LLM error during seeding causes warn-not-crash.
**Setup:** LLM returns error; L5 above threshold.
**Assertions:** compactSeed returns nil error; l2=0, l3=0.

## TestCompactSeedMalformedJSON
**Scenario:** Malformed LLM JSON output is skipped gracefully.
**Setup:** LLM returns non-JSON string; L5 above threshold.
**Assertions:** compactSeed returns nil error; l2=0, l3=0.

## TestCompactSeedStopsWhenLayersFull
**Scenario:** Seeding stops as soon as both layers reach their targets.
**Setup:** Low SeedMinL2/SeedMinL3; LLM returns enough items to fill both in one batch.
**Assertions:** DB L2 >= SeedMinL2 and L3 >= SeedMinL3 after run.

## TestCompactSeedSampleCapClusters
**Scenario:** compactSeed processes at most SeedSample clusters per run.
**Setup:** SeedSample=1; similarEmbedder; LLM returns 3 rules + 3 skills.
**Assertions:** Total l2n+l3n <= 6 (one batch's worth).

## TestParseSeedResponse
**Scenario:** JSON parsing handles plain JSON, markdown code fences, and invalid input.
**Setup:** Table-driven; inputs include plain JSON, ```json fence, generic fence, invalid JSON, empty.
**Assertions:** Correct rule counts; error returned only for invalid JSON.

## TestBuildSeedPrompt
**Scenario:** Prompt construction includes observation content.
**Setup:** Two memories with short content strings.
**Assertions:** Prompt non-empty; both content strings appear as substrings.

## TestRunOnceIncludesSeedCounts
**Scenario:** RunOnce populates report.SeedL2 and report.SeedL3 from the seed path.
**Setup:** similarEmbedder; valid LLM JSON; L5 above threshold; L2/L3 empty.
**Assertions:** report.SeedL2 > 0 OR report.SeedL3 > 0.

## TestCompactSeedDoesNotDeleteL5
**Scenario:** Seeding never soft-deletes L5 memories.
**Setup:** L5 above threshold; LLM returns valid JSON.
**Assertions:** L5 count after compactSeed equals inserted count.

## TestCompactSeedNoL5Clusters
**Scenario:** compactSeed is a no-op when no L5 clusters form (all memories dissimilar).
**Setup:** mockEmbedder (hash-based, 768 dims) producing distinct vectors; near-1 cluster threshold.
**Assertions:** l2=0, l3=0 (no clusters → no LLM calls → nothing stored).
