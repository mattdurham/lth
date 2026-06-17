# internal/mdwatcher — Invariants

## Local directory scan

1. `ScanOnce` walks every directory in `cfg.Markdown.Dirs` recursively and ingests every `.md` file found.
2. A file whose path was previously ingested but is absent on the current scan has its derived memories soft-deleted.
3. `maybeGitPull` runs `git pull --ff-only` in a configured dir only if the dir is a git repo, `git_pull` is true, and at least `git_pull_interval_s` has elapsed since the last pull for that dir.

## GitHub repos

4. For each entry in `cfg.Markdown.GitHub.Repos`, `EnsureRepo` clones the repo into `<CacheDir>/<org>/<name>` if missing (shallow when `CloneDepth > 0`), otherwise `git fetch origin` + `git reset --hard origin/<branch>`. Branch defaults to the remote's `origin/HEAD` symbolic ref.
5. EnsureRepo is rate-limited per repo by the same `git_pull_interval_s` knob used for local dirs. On the cycles between refreshes, the cached working copy is reused.
6. `validRepoSpec` rejects any `Repo` value that is not exactly `<org>/<name>` with no slashes, dots, colons, whitespace, or `..` inside the org or name. Invalid specs are skipped with a warning.
7. Auth is delegated to local git (SSH keys, credential helpers). lth never reads, stores, or transmits credentials.
8. The `.git` directory under each cached clone is skipped during file walking.

## Filtering

9. A file inside a github repo is ingested only if both:
    - `extensionMatches(path, FileTypes)` is true (`FileTypes` defaults to `[".md"]` when empty; case-insensitive comparison)
    - `pathAccepted(rel, Include, Exclude)` is true (globs evaluated against the slash-form path relative to the repo root)
10. `globMatch` supports `*` (segment-bounded), `?` (single char, segment-bounded), and `**` (zero or more whole segments, slash-spanning). Consecutive `**` segments are collapsed.
11. `pathAccepted` accepts a path when (a) `Include` is empty or at least one include glob matches, AND (b) no exclude glob matches.

## File chunking

12. Files larger than `maxFileSizeBytes` (currently 600,000 bytes) are passed through `splitForLLM(path, content, maxBytes)` before being sent to the LLM. The strategy depends on the path's extension:
    - `.md`, `.markdown` -> split at lines beginning with `"# "` (H1 headings)
    - `.yaml`, `.yml` -> split at lines whose trimmed content is exactly `"---"` (the YAML document separator; the separator itself is discarded)
    - any other extension (e.g. `.json`, `.jsonnet`, `.libsonnet`) -> size-windowed at line boundaries
13. After the format-aware first pass, any single chunk still larger than `maxBytes` is further size-windowed at line boundaries by `windowByLines`. A single line longer than `maxBytes` is emitted as one oversized chunk; the resulting LLM context-overflow error surfaces through the chain rather than being silently truncated mid-line.
14. `splitLines` is used by all three chunking helpers to split content into lines while dropping the trailing empty element that `strings.Split` returns when input ends with `"\n"`. This prevents phantom empty chunks at file ends.
