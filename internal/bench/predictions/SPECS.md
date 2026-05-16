# predictions — Specifications

## Invariants

1. `Writer` opens files in O_APPEND|O_CREATE|O_WRONLY mode — it never truncates existing content.
2. Each call to `Append` writes exactly one JSON line followed by a newline character.
3. The JSON fields must match the official SWE-bench format: `instance_id`, `model_patch`, `model_name_or_path`.
4. `PredictionsPath` returns `"predictions-{approach}.jsonl"` — callers must use this to produce per-approach output files.
5. Empty `model_patch` (for no-patch outcomes) is written as an empty string, not omitted — the official harness requires the field to be present.
