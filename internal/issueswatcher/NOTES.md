# internal/issueswatcher — Design Notes

## 1. Persist State Per-Issue, Not Once Per Repo Sync

*Added: 2026-07-11*

**Decision:** `processIssue` now calls `saveState` immediately after recording its result
(`rs.Issues[issue.NumberStr()] = is`), instead of relying solely on `syncRepo`'s end-of-batch
`saveState` call.

**Rationale:** Found by an adversarial review of `internal/prwatcher`'s NOTES.md decision #7
(the same bug, first found and fixed there, then in `internal/mdwatcher`): `syncRepo` can
process many issues (and their comments) in one poll window, each triggering a `Store()` call.
The old code only persisted `issues-state.json` once, after the entire batch finished. A daemon
restart mid-batch lost every issue's progress since the last save; the next poll's
`prev.UpdatedAt == issue.UpdatedAt && prev.MemoryID != ""` guard then failed to recognize those
issues as already-processed, triggering a redundant re-fetch and re-`Store()` call for each.

Unlike `prwatcher`/`mdwatcher`, this is **not** a data-corruption bug in the common case: issue
and comment content is deterministic (formatted directly from GitHub API fields — title, body,
state, labels), not LLM-generated, so a re-`Store()` call usually hits content-hash dedup and
returns the *same* memory ID rather than creating a duplicate row. The residual damage without
this fix was: `IssuesIngestedTotal` double-incremented on the dedup-hit retry, wasted `gh api`
calls re-fetching an already-handled issue's comments, and (the one case where content really
does change between the crash and the retry) a legitimate new memory whose progress was still
needlessly at risk of being lost again on a second interruption.

**Consequence:** Every successfully-processed issue's state hits disk before the next issue in
the batch is processed, matching `prwatcher`'s and `mdwatcher`'s guarantee. `syncRepo`'s
end-of-batch `saveState` call is kept for `LastSync`, which is only meaningfully useful once
the whole poll window has been consumed; a stale `LastSync` after an interrupted batch just
means the next poll re-fetches (from GitHub) a window it already mostly handled, which the
per-issue `UpdatedAt` guard above then no-ops on -- not free, but self-correcting rather than
duplicate-producing.
