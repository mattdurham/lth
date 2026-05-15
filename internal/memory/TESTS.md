# internal/memory — Test Scenarios

## TestStoreDedup
**Scenario:** Storing the same content twice returns the same memory ID.
**Setup:** Mock embedder, mock LLM, temp DB.
**Assertions:** Two Store calls with same content return same ID; DB has exactly 1 row.

## TestStoreSearch
**Scenario:** Stored memory is findable by search.
**Setup:** Store 3 memories with distinct content; search with matching query.
**Assertions:** Top result is the semantically closest memory.

## TestGetUpdatesAccess
**Scenario:** Get increments access_count and updates stability.
**Setup:** Store a memory; call Get twice.
**Assertions:** access_count == 2; stability > initial value.

## TestSearchLayers
**Scenario:** Search filters by layer.
**Setup:** Store L1 and L5 memories; search with layers=[1].
**Assertions:** L5 memory not returned.

## TestSearchTopK
**Scenario:** Search returns at most TopK results.
**Setup:** Store 20 memories; search with TopK=5.
**Assertions:** Exactly 5 results returned.

## TestSoftDeleteExcludes
**Scenario:** Soft-deleted memories don't appear in search.
**Setup:** Store memory; SoftDelete; search.
**Assertions:** Deleted memory not in results.
