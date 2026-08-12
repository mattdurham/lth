---
name: lth:work-simple
description: Single-agent, memory-driven development workflow — bootstrap guidance from lth, then plan, implement, test, and review yourself. INIT → PLAN → EXECUTE → TEST → REVIEW → COMPLETE.
user-invocable: true
category: workflow
---

# lth:work — Single Agent, Memory-Driven

Run the development workflow yourself, in the current workspace. This variant is
strictly single-agent: never call `Task`, `Agent`, `subagent`, create teammates, or
use agent teams. Do not delegate research, planning, implementation, testing,
review, commits, or monitoring. The one difference from a plain single-agent
workflow: you bootstrap your own guidance from lth memory before each phase
instead of relying only on static instructions.

## lth Binary

The lth binary is at `~/bin/lth`. `lth stats` starts the daemon if not running.

## Bootstrap pattern

Before researching, planning, and implementing, pull relevant guidance:

```bash
export LTH_ACTIVE=1  # enables file-level lth context injection on every Read
~/bin/lth stats
~/bin/lth prompt "<task description>"
```

`lth prompt` runs layered searches (L1/L2 principles, L3 techniques, L4 context)
plus graph expansion in one call. Apply what it returns as your operating
principles for this task. If it returns nothing, proceed with general knowledge —
don't block on it.

## Steps

1. Bootstrap from lth (`lth prompt "<task>"`). Inspect the repository, current
   branch, working tree, relevant SPECS.md/NOTES.md, and applicable guidance.
   Preserve unrelated user changes.
2. Form a concise implementation plan and record it in `.bob/state/plan.md`.
3. Implement the requested change directly, keeping the scope minimal. Before
   editing a file, `~/bin/lth read <filepath>` to see prior context alongside the
   content. Update SPECS.md/NOTES.md/TESTS.md alongside code in spec-driven
   directories.
4. Run the most relevant tests, linters, formatters, or verification commands
   (`make ci`, or `go test ./...`, `go vet ./...`, `go fmt`, `golangci-lint run`
   individually).
5. Review the diff yourself for correctness, regressions, spec drift, and missing
   tests. Fix issues found and rerun verification.
6. Store what you learned back to lth so future sessions benefit:
   ```bash
   ~/bin/lth store --layer 4 --attr "project=<repo>" --attr "tags=workflow,completed" \
     "<what was built, key decisions, what worked, what was difficult>"
   ```
   Store any reusable technique at L3 with `--layer 3 --attr "topic=<domain>"`.
7. Report changed files, verification results, remaining risks, and the final
   routing recommendation. Commit only when the user requested a commit.

Use `.bob/state/brainstorm.md`, `.bob/state/plan.md`, and
`.bob/state/test-results.md` for direct artifacts when useful. If blocked by a
missing decision or external state, explain the exact blocker instead of spawning
another agent.
