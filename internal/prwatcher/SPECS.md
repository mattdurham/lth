# internal/prwatcher — Invariants

## Scanning

1. `Run` is hot-reload friendly: it loops forever, re-checking `cfg.PR.Sources` on each iteration. While empty it sleeps 60s between checks; otherwise it scans then sleeps for `cfg.PR.IntervalS` (default 21600s / 6h).
2. `resolveSourcePath` resolves each `PRSource` to a local checkout: if `Path` is set, it is used as-is and only fast-forward-pulled (`pullFastForward`, non-fatal on failure — the caller owns that checkout). If `Path` is empty, `ensureFullClone` clones/updates the repo itself into `cfg.PR.CacheDir` (default `~/.lth/pr-repos-cache`, dedicated to prwatcher — deliberately NOT shared with `Markdown.GitHub.CacheDir`; see NOTES.md decision #8), always converging to a full (non-shallow) clone — deepening it first via `git fetch --unshallow` if it was left shallow.
3. `commitsSince(path, dir, since)` returns the commit SHAs on the current branch that touched `dir` (or the whole repo when `dir` is empty) at or after `since`, oldest first. A zero `since` (the default, `cfg.PR.LookbackDays <= 0`) means unbounded — the entire history of `dir` is mined. `since` is always computed as `now - cfg.PR.LookbackDays` fresh each scan — a fixed rolling window, never a persisted cursor — so a budget-capped or interrupted scan cannot permanently lose a PR: anything not yet resolved simply reappears in the next scan's window.
4. `scanAll` shares one `cfg.PR.MaxPerScan` (default 10) budget across all configured sources per tick, so one source with a large (or, with unbounded `LookbackDays`, effectively unlimited) backlog cannot starve the others or burst a large batch of `gh`/LLM calls in a single tick. A source too big to finish in one scan continues on the next, replaying its history gradually in chronological order.

## Commit → PR resolution and dedup

5. `discoverNewPRs(rs, shas, budget, resolve)` walks `shas` oldest-first and stops resolving once `budget` distinct not-yet-decided PR numbers have been found — this bounds `gh api` resolve calls to roughly `budget` per scan regardless of how large `shas` is (in particular, regardless of an unbounded `LookbackDays`).
6. Each not-yet-seen, not-yet-budget-capped commit SHA is resolved to a PR number via `gh api repos/<repo>/commits/<sha>/pulls`. A commit reached only by direct push (no PR) is marked seen immediately — it can never resolve to a PR, so it is never re-checked.
7. A commit belonging to a PR whose fate is already decided (`SummarizedPRs` has an entry for that PR number) is marked seen and skipped.
8. A commit belonging to a still-open PR is deliberately **not** marked seen, so it is re-resolved on every subsequent scan until the PR is merged (or otherwise decided). This is the one intentional case of repeated `gh api` calls for the same commit.
9. Once a PR's fate is decided (merged-and-summarized, or merged-and-skipped for a bot author), every commit SHA that mapped to that PR number in the current scan is marked seen and the PR number is recorded in `SummarizedPRs`, so neither the commit nor the PR is ever re-resolved again.
10. A commit left unresolved because the scan hit its `budget` of newly-discovered PRs is **not** marked seen and is not counted against `SummarizedPRs` — it is simply re-examined (and, being oldest-first, examined *before* any newer commit) on the next scan.

## Cloning

11. `ensureFullClone` accepts only a validated `<org>/<name>` repo spec (`validRepoSpec`, same shape/character restrictions as `mdwatcher.validRepoSpec`). Clone URL is always `https://github.com/<repo>.git`; auth is delegated to local git (SSH keys, credential helpers), never handled by lth directly.
12. A not-yet-cloned repo is cloned in full (no `--depth`) — always, unconditionally, regardless of any depth another feature might use for the same repo elsewhere.
13. An already-cloned repo is fetched and hard-reset to `origin/<default-branch>` (resolved via `origin/HEAD`, falling back to `main` then `master`), after first running `git fetch --unshallow` if `isShallow` reports the clone is shallow.
14. A `fetch`/`fetch --unshallow` failure is tolerated, not fatal, EXCEPT when an unshallow attempt fails and the repo is still shallow afterward (that case is a real failure and returns an error). `ensureFullClone` always proceeds to the reset step using whatever local refs already exist. This defends against any transient fetch failure (network blip, GitHub-side hiccup); it was originally added for a shared-cache-directory ref-lock race that no longer applies now that `PR.CacheDir` is dedicated (NOTES.md decisions #6 and #8), but remains correct general defense.

## Summarization

15. `classifyPR(state, mergedAt)` is the single source of truth for a PR's disposition: `prMerged` (state `MERGED` with a non-empty `mergedAt`) proceeds to summarization; `prRejected` (state `CLOSED`) is terminal and recorded as `Skipped` with no memory written -- a closed-without-merge PR will never become `MERGED`, so treating it as still-open would re-resolve it via `gh pr view` forever; anything else (`OPEN`, or a `MERGED` state with an empty `mergedAt`) is `prStillOpen` and returns a non-terminal outcome (see invariant 8).
16. A PR authored by a login in `cfg.PR.SkipAuthors` (case-insensitive) is recorded as a terminal, `Skipped` outcome with no memory written and no LLM or diff call made.
17. The diff fetched via `gh pr diff` is truncated to `maxDiffChars` (40,000) before being sent to the LLM, with a truncation note appended. A diff fetch failure degrades to summarizing from title/body alone rather than failing the PR.
18. The stored memory's `attrs["created_at"]` is set to the PR's `mergedAt` (RFC3339), which `memory.Store` uses to backdate the memory's `CreatedAt` instead of the insertion time — see `internal/memory` invariant on the `created_at` attr override. This makes an old PR decay in search like an old memory instead of scoring as freshly created.
19. An LLM error or an empty summary response is treated as non-terminal (transient) and retried on the next scan, matching invariant 3's no-data-loss guarantee.

## State

20. State persists at `~/.lth/pr-state.json`, keyed by `<repo>|<dir>` per source, and is loaded once on the first tick after `cfg.PR.Sources` becomes non-empty (not re-loaded on every tick).
21. State is saved via `persistSourceState` immediately after every terminal PR outcome within `scanSource`'s loop -- not once at the end of the batch. An interrupted scan (daemon restart, context cancellation mid-batch) can therefore lose at most the one PR that was in flight when it was interrupted, never the PRs already summarized and stored earlier in the same batch. (A batch that discovers zero new PRs saves nothing, which is a correct no-op: the loaded state is already up to date.)
22. `saveState` logs (`slog.Error`) any `os.MkdirAll` or `os.WriteFile` failure rather than discarding it. A silently-failed write would otherwise defeat invariant 21's guarantee: the in-memory state is already updated before `saveState` runs, so an unlogged disk failure would make a restart re-discover and re-summarize PRs whose "persisted" state never actually reached disk, with no signal until duplicate memories appear.
