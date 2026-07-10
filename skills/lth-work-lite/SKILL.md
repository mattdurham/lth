---
name: lth:work-lite
description: Memory-driven single-agent workflow — no subagents, serial process with lth bootstrap at each phase
user-invocable: true
category: workflow
---

# lth:work-lite — Memory-Driven Single-Agent Workflow

<!-- AGENT CONDUCT: Be direct and challenging. Flag gaps, risks, and weak ideas proactively. Hold your ground and explain your reasoning clearly. -->

You are the implementer. You perform the entire workflow yourself using lth memory to bootstrap guidance before each phase.

This is a single-agent alternative to `/lth:work`. The key difference from standard workflows:

**Every phase bootstraps its guidance from lth memory before executing.** Instead of hardcoded instructions, you search the lth store for relevant principles and context, then apply what you find.

This means:
- You improve automatically as lth accumulates experience
- Guidance emerges from real past decisions, not static documentation
- You store your findings back to lth at the end, closing the loop

## lth Binary

The lth binary is at `~/bin/lth`. All phases use this path. `lth stats` starts the daemon if not running.

## Hard Rules

- Do not spawn subagents or create teams
- Do the work yourself directly: inspect, plan, edit, test, review, and report
- Keep state in `.bob/state/` so the workflow is resumable
- Use LSP/code-intelligence tools as the primary path for navigation, references, refactor previews, diagnostics, and linting when available
- For spec-driven modules, update `SPECS.md`, `NOTES.md`, `TESTS.md`, and `BENCHMARKS.md` according to the repository rules
- Do not commit, push, or merge unless explicitly requested

## Workflow

```
INIT → WORKTREE → [BOOTSTRAP] → BRAINSTORM → PLAN → EXECUTE → TEST → REVIEW → STORE → COMPLETE
                     ↑                                  ↓        ↓       ↑
                     └───────────────────────────────────loop on gaps────┘
```

## lth Bootstrap Pattern (Used in Every Phase)

Before each major phase, run:
```bash
export LTH_ACTIVE=1  # enables file-level lth context injection on every Read
~/bin/lth stats      # start daemon if not running
~/bin/lth prompt "<phase-specific query>"
```

`lth prompt` runs layered searches (L1/L2 principles, L3 techniques, L4 context) plus PPR graph expansion in one call. `LTH_ACTIVE=1` enables automatic lth context injection whenever a file is read.

---

## Phase 1: INIT

State the task in one or two lines:

```
lth:work-lite — memory-driven workflow starting.

Task: [feature description]

You will bootstrap guidance from lth memory before each phase.
```

Check current repo state:
```bash
git status --short --branch
pwd
```

If the working tree has unrelated changes, leave them alone and work around them. Ask only if they block the task.

---

## Phase 2: WORKTREE

Use your current directory if it's already the intended worktree. If a separate worktree is needed:

```bash
COMMON_DIR=$(git rev-parse --git-common-dir 2>/dev/null)
GIT_DIR=$(git rev-parse --git-dir 2>/dev/null)

if [ "$COMMON_DIR" != "$GIT_DIR" ] && [ "$COMMON_DIR" != ".git" ]; then
  echo "WORKTREE_PATH=$(git rev-parse --show-toplevel)"
else
  REPO=$(basename "$(git rev-parse --show-toplevel)")
  FEATURE=<descriptive-slug-from-task>
  WORKTREE="../${REPO}-worktrees/${FEATURE}"
  mkdir -p "../${REPO}-worktrees"
  git worktree add "$WORKTREE" -b "$FEATURE"
  echo "WORKTREE_PATH=$(cd "$WORKTREE" && pwd)"
fi
```

Create `.bob/state/` in the active worktree:
```bash
mkdir -p .bob/state
```

---

## Phase 3: [BOOTSTRAP] — Preload lth Context

Before brainstorming, load project context from lth memory:
```bash
~/bin/lth prompt "[TASK_DESCRIPTION]" --top-each 5 > .bob/state/context.md 2>/dev/null || true
```

Check memory density:
```bash
COUNT=$(~/bin/lth search "[TASK_DESCRIPTION]" --layers L3,L4 --top 20 2>/dev/null | grep -c "^[a-f0-9]" || echo 0)
```
If COUNT < 3: print `Warning: Memory sparse for this domain ($COUNT memories). Apply general knowledge.`

---

## Phase 4: BRAINSTORM

**Bootstrapping:** Read `.bob/state/context.md` if it exists — this contains pre-loaded lth memory context. Build on this rather than re-searching from scratch.

Then bootstrap your guidance:
```bash
~/bin/lth stats
~/bin/lth prompt '[task description]'
```

Apply what you find as your brainstorming principles.

Then research the codebase yourself:
- Use `lsp_symbols`, `lsp_definition`, `lsp_references` before broad search
- Use `rg`/`read_file` for docs, specs, tests, and non-code artifacts
- Existing specs and tests before inventing new behavior

Write `.bob/state/brainstorm.md` with:
- task summary
- relevant files and current design
- constraints and risks
- at least two implementation approaches when there's a meaningful choice
- recommended approach with rationale

After writing, store key findings:
```bash
~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=brainstorm,research' '[key insight]'
```

---

## Phase 5: PLAN

**Bootstrapping:** Read `.bob/state/context.md` if it exists.

Then bootstrap:
```bash
~/bin/lth stats
~/bin/lth prompt '[task description] implementation planning'
```

Apply what you find as your planning principles.

Write `.bob/state/plan.md` with a concise TDD-first plan:
- files to change
- tests to add/update
- spec docs to update
- implementation steps (2-5 min each)
- validation commands

After writing, store key planning decisions:
```bash
~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=planning,architecture' '[key decision]'
```

---

## Phase 6: EXECUTE

**Bootstrapping:** Read `.bob/state/context.md` if it exists.

Then bootstrap:
```bash
~/bin/lth stats
~/bin/lth prompt '[task description] implementation'
```

Apply what you find as your coding principles.

Implement the plan directly:
- Write or update tests before behavior where practical
- Keep edits scoped to the task
- Prefer existing project patterns over new abstractions
- Use `lsp_refactor_preview` before renames or shared API refactors
- Use `lsp_diagnostics` or `lsp_lint` after source edits before falling back to raw shell validation
- Update `.bob/state/progress.md` after meaningful milestones

If implementation reveals the plan is wrong, update `.bob/state/plan.md` and continue. If the approach is fundamentally wrong, loop back to BRAINSTORM.

After completing implementation tasks, store patterns that worked:
```bash
~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=implementation' '[pattern or decision that worked]'
```

---

## Phase 7: TEST

**Bootstrapping:** Read `.bob/state/context.md` if it exists.

Then bootstrap:
```bash
~/bin/lth stats
~/bin/lth prompt '[task description] testing'
```

Apply what you find as your testing principles.

Run the narrowest meaningful tests first, then broaden based on risk:
- `go test ./path/to/touched/package`
- `go test ./...`
- Project-specific `make test`, `make lint`, `make precommit`

Record results in `.bob/state/test-results.md`:
- command run
- pass/fail
- important failure output
- files/tests implicated

After testing, store notable findings:
```bash
~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=testing' '[test finding or pattern]'
```

If tests fail, fix the issue and repeat EXECUTE → TEST.

---

## Phase 8: REVIEW

**Bootstrapping:** Read `.bob/state/context.md` if it exists.

Then bootstrap:
```bash
~/bin/lth stats
~/bin/lth prompt '[task description] code review'
```

Apply what you find as your review criteria.

Review your own diff in code-review mode:
```bash
git diff --stat
git diff
```

Look for:
- behavioral bugs or regressions
- missing tests
- spec/doc drift
- poor API boundaries
- race/concurrency issues
- stale generated artifacts

Write `.bob/state/review.md` with findings grouped by severity. Fix CRITICAL and HIGH findings before COMPLETE.

After reviewing, store key insights:
```bash
~/bin/lth store --layer 3 --attr 'topic=review' --attr 'tags=code-review,go' '[review pattern or insight]'
~/bin/lth store --layer 4 --attr 'project=[repo]' '[specific finding worth remembering]'
```

---

## Phase 9: STORE

Run `/lth:reflect` to automatically extract and store learnings from git history and workflow artifacts:
```bash
~/bin/lth:reflect  # or invoke via the skill if available
```

This captures decisions, problems encountered, and reusable patterns.

You may also store a workflow-level summary manually:
```bash
~/bin/lth store --layer 4 \
  --attr "project=[repo-name]" \
  --attr "tags=workflow,completed" \
  "[What was built, key decisions made, what worked well]"
```

---

## Phase 10: COMPLETE

Finish with:
- summary of changes
- validation run
- any remaining risks or follow-ups
- whether the worktree is clean or what files remain changed

Do not ask to merge unless explicitly requested.

---

## State Files

| File | Purpose |
|------|---------|
| `.bob/state/context.md` | Pre-loaded lth context from brainstorm phase |
| `.bob/state/brainstorm.md` | Research, options, recommended approach |
| `.bob/state/plan.md` | TDD-first implementation plan |
| `.bob/state/progress.md` | Running progress notes |
| `.bob/state/test-results.md` | Validation commands and results |
| `.bob/state/review.md` | Self-review findings and fixes |

---

## Autonomous Progression

You drive forward without stopping. When issues arise, fix them and continue.

Brief status updates between phases:
```
✓ [BOOTSTRAP] complete → .bob/state/context.md
Moving to BRAINSTORM...
```
```
✓ BRAINSTORM complete → .bob/state/brainstorm.md
Moving to PLAN...
```
```
✓ PLAN complete
Moving to EXECUTE...
```
```
✓ All tasks complete → routing to TEST
```
```
✓ Tests passing → routing to REVIEW
```
