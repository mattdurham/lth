# internal/mdwatcher — Design Notes

## 1. GitHub Repos as a First-Class Source

*Added: 2026-06-16*

**Decision:** Extend the markdown watcher with an optional `Markdown.GitHub`
config section that lets users specify GitHub repos by `<org>/<name>` and
have lth clone/fetch them into a cache directory before each scan, rather
than requiring the user to pre-clone the repos and list local paths in
`Markdown.Dirs`.

**Rationale:** Many sources of organisational knowledge live in shared repos
that the user doesn't otherwise check out (deployment configs, runbooks,
docs). Asking the user to manually `git clone` each one and add the path
to config is friction. Letting lth manage the cache means a single config
edit adds an ongoing knowledge source.

**Consequence:** The mdwatcher takes on light cache-directory ownership at
`~/.lth/repos-cache/<org>/<name>/`. Updates are `git reset --hard` since
nobody should be editing the cache. Errors per repo are isolated and
logged; one bad repo never breaks the scan.

---

## 2. Auth Delegated to Local git

*Added: 2026-06-16*

**Decision:** lth shells out to the system `git` binary and lets it pick up
credentials from the user's existing setup (SSH keys, `git-credential-*`
helpers, `gh auth`).

**Rationale:** Building credential handling into lth is a security-sensitive
distraction. Every developer machine already has working git auth. SSO-gated
private repos, HTTPS-via-PAT, and SSH-via-keys all work transparently.

**Consequence:** If `git clone` fails (no auth, bad URL, network), lth logs
a warning and skips that repo for the cycle. No partial state, no credential
prompts in the daemon.

---

## 3. Glob Filtering Instead of Path Prefixes

*Added: 2026-06-16*

**Decision:** `Include` and `Exclude` accept doublestar-style globs
(`**/tempo/**`, `docs/**/*.md`) implemented in ~30 lines using
`filepath.Match` for single segments and a recursive `**`-aware matcher
for slash-spanning segments. No third-party glob dependency.

**Rationale:** Real use cases need flexibility a prefix list cannot
express: "any directory named `tempo` anywhere in the tree", "all `.md`
files under any depth of `docs`", "everything except `vendor/**`". A
recursive segment matcher gives full doublestar semantics in trivial
code, and the call rate (one per file per scan, scans every 5 minutes)
makes performance a non-concern.

**Consequence:** Users get familiar glob syntax. Adding doublestar as a
dep would have provided a marginally faster matcher (compiled state
machine vs. recursion) for no practical benefit at lth's scale.

---

## 4. Multi-File-Type Ingestion

*Added: 2026-06-16*

**Decision:** Each repo has a `file_types` list (e.g. `[".md", ".yaml",
".jsonnet"]`) that defaults to `[".md"]` when empty. The same fact-
extraction prompt is reused across all types; only the wording changed
from "this markdown document" to "this document" so YAML/Jsonnet inputs
don't look out of place.

**Rationale:** Kubernetes manifest repos and other infrastructure-as-code
trees are mostly YAML/Jsonnet. Restricting to `.md` would miss the actual
signal. The LLM (Qwen3-4B / Haiku) handles structured config formats fine
-- it extracts facts like "service X listens on port Y" from raw YAML
without prompt engineering.

**Consequence:** Per-repo file_types means a docs-only repo stays on
markdown while a config repo opens up to all relevant types. Defaulting
empty to `[".md"]` preserves exact back-compat for anyone using only
`Markdown.Dirs` or repos without specifying `file_types`.

---

## 5. Format-Aware Chunking

*Added: 2026-06-17*

**Decision:** Replace the single `splitByHeading` helper with `splitForLLM(path, content, maxBytes)` that dispatches on file extension: `# ` headings for markdown, `---` document separators for YAML, and size-windowed line chunking for everything else (`.json`, `.jsonnet`, `.libsonnet`, etc.). After format-aware splitting, an outer pass applies `windowByLines` to any chunk still over the byte cap.

**Rationale:** Before this, large JSON or Jsonnet files would walk straight through `splitByHeading` (which only knows `# ` lines), produce one chunk equal to the entire file, and fail the LLM call with a context-overflow error. The same bug applied to large YAML documents. With format-aware splitting, each chunk is bounded by both natural format boundaries and a hard byte cap, so big files get a sensible sequence of LLM calls instead of one failing call.

**Consequence:** A single oversized line (e.g., minified JSON on one line) is emitted as one oversized chunk and the LLM rejection bubbles up through the chain. This is intentional: silently truncating mid-line would corrupt the extracted facts in subtle ways, while an explicit error in the daemon log is easy to diagnose.

## 6. Persist State Per-File, Not Once Per Scan Batch

*Added: 2026-07-11*

**Decision:** `processFile` now calls `saveState` immediately after recording its result
(`w.st.Files[path] = fileState{...}`), instead of relying solely on `ScanOnce`'s end-of-batch
`saveState` call.

**Rationale:** Found by an adversarial review of `internal/prwatcher`'s NOTES.md decision #7
(the same bug, first found and fixed there): `ScanOnce` can walk hundreds of files across
`Markdown.Dirs` and `Markdown.GitHub.Repos` in one call, each triggering an LLM extraction call
via `processFile`. The old code only persisted `mdwatcher-state.json` once, after the entire
walk finished. A daemon restart mid-scan lost every file's progress since the last save; the
next scan's `prev.Hash != hash` check then failed to recognize those files as already-ingested
(since the on-disk state still showed the old hash or no entry at all), triggering a full
LLM re-extraction and a fresh `Store()` call for each. Because the stored content is LLM-
generated ("facts" extracted from the file), it is **not** byte-identical between runs, so
`Store`'s content-hash dedup never caught the duplicate -- unlike `issueswatcher`'s analogous
gap, where deterministic GitHub API content usually saves it. This is architecturally identical
to the incident that produced 31+ duplicate PR memories in `prwatcher`, just triggered by any of
today's several daemon restarts hitting `mdwatcher` mid-scan instead.

**Consequence:** Every successfully-ingested (or confirmed-unchanged) file's state hits disk
before the next file is processed, so an interrupted scan loses at most the one file in flight,
matching `prwatcher`'s guarantee. `ScanOnce`'s existing end-of-batch `saveState` call is kept
(now effectively a redundant final flush covering the soft-delete-for-removed-files cleanup
loop, which is idempotent and lower-risk since it only re-applies already-known soft-deletes).
