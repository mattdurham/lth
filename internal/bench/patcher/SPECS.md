# patcher — Specifications

## Invariants

1. `ExtractPatch` is a pure function — no I/O, no side effects, deterministic on the same input.
2. `ExtractPatch` finds the *first* `<patch>` … `</patch>` block; subsequent blocks are ignored.
3. `ExtractPatch` trims leading/trailing whitespace from the extracted content.
4. `ApplyPatch` must set `cmd.Dir = repoDir`; it never changes the process working directory.
5. `ApplyPatch` uses `git apply --whitespace=fix` for leniency on whitespace-only differences.
