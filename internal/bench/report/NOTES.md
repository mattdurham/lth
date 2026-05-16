## 1. JSONL format for crash recovery

*Added: 2026-05-15*

**Decision:** One JSON object per line (JSONL), opened in append mode. Completed runs are detected by loading the file at startup.

**Rationale:** The full benchmark (42 problems × 3 approaches) takes several hours. Incremental append allows resuming after a crash, network failure, or manual interruption without losing completed results.

**Consequence:** The results file may contain partial or duplicate entries from interrupted runs. `LoadCompleted` reads all entries to build the skip-set; callers must handle this correctly.
