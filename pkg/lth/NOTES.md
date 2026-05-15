# pkg/lth — Design Notes

## 1. Thin Wrapper Pattern

*Added: 2026-05-14*

**Decision:** `pkg/lth.Client` is a thin delegation layer with no logic.

**Rationale:** Separating the public API from internal implementation allows:
- External consumers to import only `pkg/lth` without pulling in internal details
- Future API evolution without changing internal package structure
- Clear boundary for testing via the public surface

**Consequence:** All business logic lives in `internal/memory`, `internal/graph`, etc.
The `pkg/lth` package only wires dependencies and delegates.
