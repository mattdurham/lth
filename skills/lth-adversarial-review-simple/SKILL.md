---
name: lth:adversarial-review-simple
description: Adversarial code review of the lth codebase by the current agent, without spawning subagents. Covers the same lth-specific hazards as lth:adversarial-review (spec drift, watcher concurrency, memory/compaction interactions, contract/test gaps, bugs, code quality, architecture) in one pass. Writes findings to .lth-review/state/review.md.
user-invocable: true
category: workflow
---

# lth:adversarial-review — Single Reviewer

Perform a hostile, read-only review of the **lth** codebase
(`github.com/mattdurham/lth` — a 5-layer hierarchical memory database with a
background daemon running multiple watcher goroutines) yourself. Do not spawn
subagents, delegate work, or modify source code. Default assumption: every
watcher, every `Store()` call, every shared directory has at least one issue —
your job is to disprove that, not confirm the code is fine.

## Scope

Verify this is the lth repo first:
```bash
grep -q "module github.com/mattdurham/lth" go.mod 2>/dev/null || echo "NOT THE LTH REPO"
```
If it doesn't match, stop and tell the user this skill is scoped to lth
specifically — suggest `bob-adversarial-review-simple` instead.

If invoked with `DIFF` or `diff`, review only files changed from the merge-base
with `main`:
```bash
git diff --name-only "$(git merge-base HEAD main)..HEAD"
```
Otherwise review the whole codebase (`find . -name '*.go' -not -path './.git/*'`).
Also gather the watcher/path inventory once, up front — every config field that
resolves to a filesystem path (CacheDir, Dirs, Paths) and which watcher(s) read
or write it:
```bash
ls internal/*watcher/ -d 2>/dev/null
grep -rn "cache_dir\|CacheDir" internal/config/config.go internal/config/load.go
grep -n "go .*\.Run(ctx)" cmd/lth/watch.go
```
Read relevant SPECS.md/NOTES.md/TESTS.md/BENCHMARKS.md and CLAUDE.md before
judging behavior. Create `.lth-review/state` if needed.

## Review checklist

Work through all eight areas below against the scoped files. Several checks
exist because they are real bugs found in production in this codebase — treat
those as the highest-priority patterns to hunt for.

1. **Spec & invariant drift** — every `.go` file should carry
   `// NOTE: Any changes to this file must be reflected in the corresponding
   SPECS.md or NOTES.md.`; a changed/new watcher package should have a matching
   SPECS.md/NOTES.md update in the same commit; check hard-delete-only vs
   soft-delete, content-hash dedup on non-deterministic content, `created_at` as
   a reserved attrs key, one-struct-per-file. Cross-check `internal/config/reload.go`'s
   `HotFields` map against every `w.cfg.X.Y` read inside a watcher's loop —
   missing entries silently don't hot-reload; false entries silently do nothing.

2. **Comment accuracy** — verify every "safe because X" concurrency claim by
   checking BOTH sides of the shared access, not just the file under review;
   check comments against actual return semantics, algorithm behavior, and
   NOTES.md decision rationale for staleness.

3. **Watcher concurrency & shared-resource hazards** — lth runs 6+ watcher
   goroutines in one daemon. Using the path inventory: does any filesystem path
   get written to (git ops, `os.Remove`, `os.WriteFile`, `os.Rename`) by more
   than one watcher? Any `git fetch`/reset whose error path treats any non-zero
   exit as fatal without checking actual end state? Any `exec.Command` not using
   `exec.CommandContext(ctx, ...)`, or blocking calls not honoring the watcher's
   `Run(ctx)` context? Any watcher accumulating a whole scan batch in memory and
   persisting state only once at the end (data loss on interrupted restart)? Any
   package-level var or shared struct field (via `*config.Config`, `*metrics.Metrics`,
   `*memory.MemoryStore`) mutated without synchronization? Any unbounded per-tick
   external-call fan-out (gh/LLM calls scaling with unbounded input)?

4. **Memory & compaction interactions** — read `internal/memory/SPECS.md` and
   `internal/compactor/SPECS.md` first. Any backdated `created_at` that changes
   compaction age-eligibility without a documented tradeoff? Any hard-delete
   (`DELETE FROM memories`) instead of `compacted_at`? Any `Store()` call relying
   on content-hash dedup for LLM-generated (non-deterministic) content with no
   entity-ID guard? Layer/decay mismatches; attrs-map aliasing after `Store()`
   pops reserved keys in place; undocumented drift in the search scoring formula.

5. **Contract & test gaps** — unauthorized public API expansion in `internal/`
   packages; new `memory.Store` implementations/wrappers breaking documented
   pre/post-conditions; hardcoded paths instead of deriving from config; a new
   watcher package not wired into `cmd/lth/watch.go`'s `runWatchDaemon`; missing
   metrics parity (ingested-count counter + last-sync gauge); test names that lie
   relative to their body; regression tests that short-circuit before reaching
   the bug; tests not using `t.TempDir()` or depending on real `~/.lth/*`.

6. **Logic bugs** — off-by-ones in loop bounds/budget checks, inverted boolean
   guards, early returns skipping cleanup or state-save calls, shadowed errors,
   accumulators not reset between scan ticks, resource leaks, swallowed errors
   (especially in a persistence path), missing `default` cases on enum switches,
   slice-append aliasing, float/time comparison mistakes.

7. **Code quality** — magic numbers/durations without named constants
   (especially defaults that duplicate `load.go`'s `Default()`), cyclomatic
   complexity (`gocyclo -over 20 ./...`), repeated literals without a shared
   const, non-idiomatic Go, dead exported symbols in `internal/`, one-struct-
   per-file violations (excluding the known `internal/config/config.go`
   baseline), side-effectful `init()`.

8. **Architecture** — cross-watcher package imports (violates the documented
   convention of duplicating small helpers instead of coupling watchers); a new
   feature sharing a directory with an existing feature for the same kind of
   auto-managed resource (see `prwatcher/NOTES.md` decisions #5 and #8); zero-
   field structs with one delegating method; single-implementation interfaces
   not at a real package boundary; constructors returning unexported types; dead
   code paths; single-use abstractions.

## Report

Write `.lth-review/state/review.md`:

```markdown
# lth Adversarial Review (single reviewer) — [branch] — [date]

**Mode:** single reviewer (no subagents)
**Scope:** [DIFF or full repository]

**Total findings: N** | CRITICAL: X | HIGH: Y | MEDIUM: Z | LOW: W

**Recommendation: STOP AND RE-THINK / FIX BEFORE NEXT RESTART / SAFE TO PROCEED**

## CRITICAL — Must Fix Before Merge/Restart
> **`file.go:line` — Title**
> Explanation, which invariant it violates, whether it matches a known
> production-incident pattern.

## HIGH — Should Fix Before Merge/Restart
## MEDIUM — Fix or Accept + Document
## LOW — Optional Cleanup

## Clean Areas
Note any of the 8 checklist areas where nothing was found.

## Shared-Resource Map
(Only if any path is shared across watchers.) Table of path → watchers → what
each does to it (read-only walk / destructive reset / write).
```

Sort findings highest-to-lowest severity. If no findings are verified, say so
explicitly and recommend PASS — don't invent issues to satisfy the adversarial
premise. Also output the report inline so the user can read it immediately.

If `TEST` or `test` is supplied, identify test cases that would reproduce every
feasible CRITICAL or HIGH finding (logic errors, batch-only persistence,
content-hash-dedup-on-nondeterministic-content, missing context propagation,
swallowed errors), but do not create tests automatically in this single-
reviewer mode — list them as a TODO instead.
