## 1. Language detection via repo-name map

*Added: 2026-05-15*

**Decision:** Detect language by looking up `problem.Repo` in a hard-coded `repoLanguage` map.

**Rationale:** The SWE-bench Multilingual HuggingFace API response has no `language` field. The 5 Go repos are fixed and known at the time of writing. A hard-coded map is simple, readable, and requires no API changes.

**Consequence:** Adding a new language requires updating the map. Only Go is supported in v1.
