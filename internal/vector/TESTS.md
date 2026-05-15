# internal/vector — Test Scenarios

## TestCosine
**Scenario:** Verify cosine similarity function across edge cases.
**Cases:**
- Identical vectors → 1.0
- Zero vectors → 0.0 (no panic)
- Orthogonal vectors → 0.0
- Opposite direction → -1.0
- Different length vectors → 0.0
- Unit vectors → computed value within epsilon

## TestToBytesFromBytes
**Scenario:** Verify round-trip serialization.
**Cases:**
- Empty vector → empty bytes → empty vector
- Single float → correct 4 bytes → same float
- 768-element vector → exact round-trip equality

## TestOllamaEmbedder
**Scenario:** Embed a text string via mock server.
**Cases:**
- Happy path: mock returns float64 array; verify float32 conversion
- Server error 500: verify error returned
- Context timeout: verify deadline exceeded error
