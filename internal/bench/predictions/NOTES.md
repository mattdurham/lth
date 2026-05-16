## 1. Official SWE-bench prediction format

*Added: 2026-05-15*

**Decision:** Write predictions in the official SWE-bench JSONL format with fields `instance_id`, `model_patch`, `model_name_or_path`, one record per line.

**Rationale:** The official SWE-bench evaluation harness (`swebench.harness.run_evaluation`) expects exactly this format. Using the same format avoids a conversion step and allows direct evaluation after inference.

**Consequence:** The `model_name_or_path` field is set to the approach name (e.g. "lth-work") rather than a model identifier, so evaluation results are labeled by approach.
