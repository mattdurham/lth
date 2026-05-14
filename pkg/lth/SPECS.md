# pkg/lth — Invariants

1. `Client` is the sole external entry point for callers outside `internal/`.
2. `NewClient` is the only valid way to create a `Client`; it enforces all wiring.
3. `Close` must be called for clean DB shutdown; unclosed clients leak file handles.
4. All methods accept `context.Context` as first argument and respect cancellation.
5. `Client` methods are thin wrappers over internal packages; no business logic lives here.
