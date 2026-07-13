# internal/prwatcher — Design Notes

## 1. Rolling Time Window Instead of a Sync Cursor

*Added: 2026-07-10*

**Decision:** `commitsSince` is always called with `since = now - LookbackDays`, a
fixed rolling window recomputed every scan. There is no persisted "last scan
timestamp" cursor like `issueswatcher`'s `LastSync`.

**Rationale:** A cursor-based design was tried first: advance `since` to `now`
after every scan, relying on the cursor never revisiting old commits. That
design has a data-loss bug: if `MaxPerScan` truncates the list of new PRs
found in a window, the un-processed PRs' commits still get marked resolved
(to avoid re-querying `gh api` for them), the cursor advances past their
commits, and those PRs then vanish from every future scan's window — silently
dropped forever. A rolling window sidesteps this entirely: anything not fully
resolved this scan (budget-capped, or the PR is still open) is simply visible
again next scan, because the window recomputes from `now` rather than
advancing from where the last scan stopped. Correctness comes from the
per-commit/per-PR dedup state (`SeenCommits`, `SummarizedPRs`), not from the
window boundary.

**Consequence:** Every scan re-runs `git log --since=...` over the full
lookback window (cheap, local, no API calls) instead of a short delta. The
`gh api commits/.../pulls` calls — the only per-scan cost that matters — are
still skipped for any commit already in `SeenCommits`, so the steady-state
cost stays proportional to genuinely new commits, not to `LookbackDays`.

## 2. Commits Belonging to Open PRs Are Never Marked Seen

*Added: 2026-07-10*

**Decision:** A commit that resolves to a PR is only added to `SeenCommits`
once that PR reaches a terminal state (merged-and-summarized or
merged-and-skipped). A commit on a still-open PR is left unmarked so it gets
re-resolved via `gh api` on every subsequent scan.

**Rationale:** The alternative — mark the commit seen as soon as it resolves
to *any* PR, regardless of merge state — means a PR open at check-time and
merged later would never be re-examined, since its commit would already be
in `SeenCommits`. Re-resolving a handful of open PRs per scan is a bounded,
acceptable cost; permanently missing a PR is not.

**Consequence:** Repos with long-lived open PRs touching the watched paths
pay a small, constant `gh api` cost per scan for those PRs until they merge
or close. This is intentional and does not scale with history size.

## 3. Backdating via `memory.Store`'s `created_at` Attr, Not a New Method

*Added: 2026-07-10*

**Decision:** To make an old PR's stored summary decay in search like an old
memory (rather than scoring as freshly created, since the exp-decay time
score in `internal/memory/search_impl.go` is keyed off `CreatedAt`), prwatcher
sets `attrs["created_at"]` to the PR's `mergedAt` and relies on
`memory.MemoryStore.Store` to consume it as a `CreatedAt` override. See
`internal/memory`'s NOTES.md-equivalent decision for the mechanism itself.

**Rationale:** A reserved attrs key requires zero signature changes to the
`Store` interface, `pkg/lth.Client`, the REST API, or any other watcher — the
override is transparently available to every future backfill use case that
stores memories describing past events, not just this one.

**Consequence:** `attrs["created_at"]` is a reserved key: any caller that
sets it is opting into backdating, and the value must parse as RFC3339 or
`Store` returns an error. The key is stripped before persisting the memory's
literal attributes, since the information already lives in the `CreatedAt`
column.

## 4. Unbounded Lookback by Default, Bounded Per-Scan Work Instead

*Added: 2026-07-10*

**Decision:** `LookbackDays` defaults to 0 (unbounded — mine a source's
entire history) rather than a fixed window. `MaxPerScan` defaults to 10,
shared across all configured sources per scan, and now bounds `gh api`
*resolve* calls via `discoverNewPRs` (see invariant 5), not just LLM
summarization calls.

**Rationale:** The motivating use case (`grafana/deployment_tools`) calls
for the full PR history of a directory, replayed over time, not a recent
window. An earlier version of this decision bounded `LookbackDays` to 90
days specifically to keep first-run cost small; once `discoverNewPRs`
existed to cap *resolution* work per scan (not just summarization work),
the time-window bound became unnecessary for that purpose — the per-scan
budget alone keeps every scan small regardless of how far back history
goes, so there was no longer a reason to also silently drop everything
older than the window.

**Consequence:** A source with more history than `MaxPerScan` can process in
one tick catches up gradually, one `IntervalS` tick at a time, in
chronological order (oldest first), until fully replayed. `LookbackDays` is
still available as an opt-in bound for anyone who genuinely only wants
recent history.

## 5. Auto-Clone Reuses mdwatcher's Cache Directory, Not Its Code

*Added: 2026-07-10*

**⚠️ SUPERSEDED by decision #8 (2026-07-11).** The directory-sharing choice below and its
"safe because" claim are **incorrect** — decision #6 documents a ref-lock race and decision
#8 documents a second, more damaging incident (447 false "file removed" soft-deletes) caused
by exactly the gap this decision's safety argument missed: it only examined what each watcher
*reads*, never what both watchers *write* (git reset/checkout) to the shared directory.
`PR.CacheDir` is now dedicated to prwatcher; nothing shares a cache directory with mdwatcher
today. The entry below is kept for historical record only — do not treat its "Consequence" as
current behavior.

**Decision:** When `PRSource.Path` is empty, prwatcher clones/updates the
repo itself into `cfg.Markdown.GitHub.CacheDir` — the literal same
config value (and therefore, by default, the literal same on-disk
directory: `~/.lth/repos-cache/<org>/<name>/`) that `mdwatcher`'s
GitHub-repos feature uses. The clone/fetch/reset implementation itself
(`ensureFullClone`, `isShallow`, `defaultBranch`, `validRepoSpec`) is a
self-contained copy in `internal/prwatcher/git.go`, not a call into
`mdwatcher.EnsureRepo`.

**Rationale:** Sharing the cache *directory* means a repo configured under
both `markdown.github.repos` and `pr.sources` is cloned once, not twice —
this is what makes "same folder as the others" true on disk, which is the
property that was actually asked for. Sharing the cache *code*, by having
prwatcher import `mdwatcher` and call `EnsureRepo`, would couple two
sibling watcher packages together and pull in `MarkdownGitHubRepo`'s
Include/Exclude/FileTypes/Branch fields that PR mining has no use for, for
the sake of ~30 lines of clone/fetch logic. Every existing watcher
(`watcher`, `mdwatcher`, `gwswatcher`, `issueswatcher`) already duplicates
small helpers (`sleepCtx`, `expandHome`) rather than sharing a common
package; this follows the same convention one level up, for clone/fetch.

**Consequence:** `mdwatcher.EnsureRepo`'s behavior (shallow clone by
default, hard-reset-on-refresh) and prwatcher's `ensureFullClone`
(always-full clone, unshallow-if-needed) are two independent
implementations against the same directory. This is safe because both are
read-only consumers of repo *content* at HEAD — mdwatcher resets to
`origin/<branch>` and reads current file content; prwatcher additionally
needs full commit history, which is why it deepens a shallow clone rather
than assuming whoever cloned first got the depth right. Neither writes
anything a concurrent reader would see torn.

## 6. Tolerate `git fetch` Failures on an Already-Cloned Repo

*Added: 2026-07-10*

**Decision:** In `ensureFullClone`, a `fetch`/`fetch --unshallow` failure on
an already-cloned repo is logged and tolerated, not fatal — `ensureFullClone`
proceeds to the reset step using whatever local refs already exist. The one
exception: if an unshallow attempt fails and a follow-up `isShallow` check
shows the repo is *still* shallow, that's treated as a real failure.

**Rationale:** Decision #5's claim that mdwatcher and prwatcher operating on
the same shared clone is safe because "neither writes anything a concurrent
reader would see torn" undersold a real failure mode that isn't about torn
reads: two *concurrent writers* (mdwatcher's hourly `git fetch origin` and
prwatcher's `git fetch --unshallow origin`, both against
`Markdown.GitHub.CacheDir`) can race on updating the same remote-tracking
ref (e.g. `refs/remotes/origin/master`). Git's ref-lock protects against
corruption, but the loser's `git fetch` exits non-zero with "cannot lock
ref ... is at X but expected Y" — even though the object data (and often the
ref itself) ends up correctly updated by the *winning* fetch. This first
showed up in production: the initial unshallow of `grafana/deployment_tools`
deepened correctly (verified via `git rev-parse --is-shallow-repository`
after the fact), but the command still returned exit status 1, and the
original code treated that as fatal and gave up the whole scan for a full
`IntervalS` (default 6h).

**Consequence:** Every path through `ensureFullClone` treats "the remote
ref didn't get updated by *this* fetch call" as fine — the reset step uses
whatever ref value is currently on disk, which is either already current
(updated by the other watcher's fetch) or will catch up on the next scan.
Only a genuinely stuck shallow clone (unshallow attempted and failed AND
still shallow) is a real error worth surfacing.

## 7. Persist State Per-PR, Not Once Per Batch

*Added: 2026-07-11*

**Decision:** `scanSource` now calls `persistSourceState` immediately after
each PR's outcome is decided (inside the loop), instead of accumulating all
outcomes in memory and calling `saveState` once after the whole `newPRs`
loop finished.

**Rationale:** Found in production, not in review: with `MaxPerScan=100`
and a slow LLM backend, a single scan's batch can run long enough to still
be in flight when the daemon is restarted (which we did several times while
tuning `IntervalS`/`MaxPerScan` and swapping the LLM primary). The original
once-at-the-end save meant every PR successfully summarized and stored
*before* the interruption point was invisible to the persisted state --
`SummarizedPRs` on disk still showed the previous batch's ending point. The
next scan then re-discovered those same PRs (their `SeenCommits` markers
were also never persisted) and re-summarized them from scratch. Since the
LLM doesn't produce byte-identical text twice, `Store`'s content-hash dedup
didn't catch it: the second summary got a different hash and became a
second, independent memory for the same PR. This produced 12 real duplicate
memories in `grafana/deployment_tools` before the fix -- confirmed by
comparing `len(memories with source=github_pr)` against the count of
distinct `pr_number` attribute values.

**Consequence:** Every terminal PR outcome triggers its own JSON marshal +
file write of the whole state map (not just a delta), which is more I/O
than the previous once-per-batch save. At the scale this watcher operates
at (tens of PRs per scan, not thousands), this is a non-issue; correctness
under interruption matters more than shaving a few dozen small file writes
per scan.

## 8. Superseding Decision #5: Dedicated Cache Directory, Not Shared

*Added: 2026-07-11*

**Decision:** `PRConfig.CacheDir` (default `~/.lth/pr-repos-cache`) replaces
`Markdown.GitHub.CacheDir` as the directory `ensureFullClone` clones/updates
auto-managed `PRSource`s into. A repo configured under both
`markdown.github.repos` and `pr.sources` is now cloned into two independent
directories, not shared.

**Rationale:** Decision #5 argued sharing the directory was safe because
both watchers are "read-only consumers of repo content." That's true of the
*data* each watcher reads, but not of the *filesystem operations* each
watcher performs to get there: both run `git reset --hard`/checkout against
their cache dir, and one watcher's reset mid-flight through another's
`filepath.WalkDir` scan can make files transiently disappear to the
scanner. `mdwatcher`'s file-tracking logic treats "not seen in this scan"
as "deleted" and soft-deletes every memory derived from that file --
found in production: 447 false "file removed" events in one day, each
triggering a needless re-ingest-from-scratch of a file that was never
actually gone. Decision #6 already fixed the analogous ref-lock race for
prwatcher's own fetches; this is the second, more damaging shared-directory
failure mode, and this time the fix is to stop sharing rather than tolerate
the race.

**Consequence:** A repo used by both features costs a second clone on disk
(negligible compared to the value of eliminating an active-data-corruption
race). Decision #6's fetch-failure tolerance in `ensureFullClone` remains in
place regardless -- it's still correct defense against other transient
fetch failures (network blips, GitHub-side hiccups), just no longer load
-bearing for the specific mdwatcher-collision scenario that motivated it.

## 9. Closed-Without-Merge PRs Are Terminal, Not "Still Open"

*Added: 2026-07-11*

**Decision:** `summarizePR` now classifies a fetched PR via a pure `classifyPR(state,
mergedAt)` function into `prMerged` / `prRejected` / `prStillOpen`, instead of the original
single check `if pr.State != "MERGED" || pr.MergedAt == "" { return non-terminal }`, which
bucketed `CLOSED` (rejected without merging) together with `OPEN`.

**Rationale:** Found by adversarial review: a closed-without-merge PR will never transition
to `MERGED`, so treating it as "still open, recheck next scan" means it is re-resolved via
`gh pr view` on every single scan forever. Since discovery in `discoverNewPRs` is
oldest-commit-first and stops once `budget` new PRs are found, a repo with several old
closed-without-merge PRs early in its mined history can permanently consume the entire
`MaxPerScan` budget on PRs that can never resolve, starving discovery of genuinely new merged
PRs behind them.

**Consequence:** A closed PR is now recorded as a terminal, `Skipped` outcome (same treatment
as a bot-authored PR) the first time it's resolved, and never re-checked again.
`classifyPR` is a pure function specifically so this state-machine decision is unit-testable
without a real `gh` call.
