---
name: lth:work
description: Memory-driven team workflow — agents bootstrap their own guidance from lth before working. INIT → WORKTREE → BRAINSTORM → PLAN → EXECUTE → REVIEW → COMPLETE
user-invocable: true
category: workflow
requires_experimental: agent_teams
---

# lth:work — Memory-Driven Team Workflow

<!-- AGENT CONDUCT: Be direct and challenging. Flag gaps, risks, and weak ideas proactively. Hold your ground and explain your reasoning clearly. -->

You are orchestrating a **memory-driven team workflow**. The key difference from a standard workflow:
**every agent bootstraps its own guidance from lth before working**. Instead of hard-coded instructions,
agents search the lth memory store for principles, skills, and situational context relevant to their
role and the task at hand — then apply what they find.

This means:
- Agents improve automatically as lth accumulates experience
- Guidance emerges from real past decisions, not static documentation
- Agents store their findings back to lth, closing the loop

## lth Binary

The lth binary is at `~/bin/lth`. All agents use this path. `lth stats` starts the daemon if not running.

## Prerequisites

<experimental_feature>
This workflow requires the experimental agent teams feature:

```json
// Add to ~/.claude/settings.json
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  }
}
```

Without this flag, the workflow will fail.
</experimental_feature>

## Workflow Diagram

```
INIT → WORKTREE → BRAINSTORM → PLAN → SPAWN TEAM → EXECUTE ↔ REVIEW → TEST → REVIEW → STORE → COMPLETE
            ↑                                            ↓           ↓
            └────────────────────────────────────────────┴───────────┘
                                  (loop back on issues)
```

<strict_enforcement>
All phases MUST execute in order. NO phases may be skipped.
</strict_enforcement>

## Flow Control Rules

- **REVIEW → BRAINSTORM**: CRITICAL/HIGH issues require re-brainstorming
- **EXECUTE/REVIEW → EXECUTE**: Failed tasks or review issues create fix tasks
- **TEST → EXECUTE**: Test failures require code fixes

<critical_gate>
REVIEW is MANDATORY — cannot be skipped even if tests pass.
NO git operations before COMMIT phase.
</critical_gate>

---

## Execution Rules

**All subagents MUST run in background:**
```
Task(subagent_type: "...", run_in_background: true, prompt: "...")
```

Background execution enables true parallelism. Never use foreground.

---

## lth Bootstrap Pattern

Every agent uses this pattern before doing any work:

```bash
~/bin/lth stats  # start daemon if not running

# 1. Identity + principles
~/bin/lth search "<task or role query>" --layers L1,L2 --top 5

# 2. Skills and techniques  
~/bin/lth search "<role-specific query>" --layers L3 --top 10

# 3. Recent project context
~/bin/lth search "<task-specific query>" --layers L4,L5 --top 5
```

Apply findings as operating principles. If lth returns nothing, proceed with general knowledge.

---

## Team Architecture

```
Team Lead (You)
  ├── coder-1   (self-prompted via lth)
  ├── coder-2   (self-prompted via lth)
  ├── reviewer-1 (self-prompted via lth)
  └── reviewer-2 (self-prompted via lth)
```

**Team lead coordinates. Never executes.**

Team Lead CAN: create/manage team, spawn teammates, create tasks (TaskCreate), monitor (TaskList), message teammates, read `.bob/` files, `cd` into worktree, invoke skills.

Team Lead CANNOT: write/edit files, run git commands, run tests, make implementation decisions.

---

## Phase 1: INIT

**Actions:**
1. Greet the user:
   ```
   "lth:work — memory-driven workflow starting.

   Task: [feature description]

   Agents will bootstrap their guidance from lth memory before working.
   Rallying the team..."
   ```

2. Verify experimental flag:
   ```
   Check CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 is set.
   If not, STOP and instruct the user to set it.
   ```

3. Move to WORKTREE.

---

## Phase 2: WORKTREE

**Goal:** Create an isolated git worktree.

Spawn a Bash agent:
```
Task(subagent_type: "Bash",
     description: "Check or create worktree",
     run_in_background: true,
     prompt: "Check if already in a worktree; create one if not.

     1. COMMON_DIR=$(git rev-parse --git-common-dir 2>/dev/null)
        GIT_DIR=$(git rev-parse --git-dir 2>/dev/null)
        If COMMON_DIR != GIT_DIR and COMMON_DIR != '.git':
          echo 'Already in worktree'
          echo WORKTREE_PATH=$(git rev-parse --show-toplevel)
          mkdir -p .bob/state && exit 0

     2. REPO_NAME=$(basename $(git rev-parse --show-toplevel))
        FEATURE_NAME=<descriptive-feature-name-from-task>
        WORKTREE_DIR=../${REPO_NAME}-worktrees/${FEATURE_NAME}

     3. mkdir -p ../${REPO_NAME}-worktrees
        git worktree add $WORKTREE_DIR -b $FEATURE_NAME
        mkdir -p $WORKTREE_DIR/.bob/state

     4. echo WORKTREE_PATH=$(cd $WORKTREE_DIR && pwd)
        cd $WORKTREE_DIR && git branch --show-current")
```

After agent completes: read output for `WORKTREE_PATH`, then `cd <WORKTREE_PATH>`.

On loop-back: skip — worktree exists.

---

## Phase 3: BRAINSTORM

**Goal:** Research codebase and explore approaches.

**Step 0 — BOOTSTRAP context from lth:**
```bash
~/bin/lth prompt "[TASK_DESCRIPTION]" --top-each 5 > .bob/state/context.md 2>/dev/null || true
```
This pre-loads principles, techniques, and project context into `.bob/state/context.md` for all agents.

**Step 0.5 — Memory density check:**
```bash
COUNT=$(~/bin/lth search "[TASK_DESCRIPTION]" --layers L3,L4 --top 20 2>/dev/null | grep -c "^[a-f0-9]" || echo 0)
```
If COUNT < 3: print `Warning: Memory sparse for this domain ($COUNT memories). Agents apply general knowledge. Run /lth:reflect after sessions to build memory.`

**Step 1:** Write brainstorm prompt to `.bob/state/brainstorm-prompt.md`:
```
Task: [feature/task description]
Requirements: [constraints, acceptance criteria]
Spec-driven modules: [directories with SPECS.md, NOTES.md, TESTS.md, BENCHMARKS.md, or NOTE invariant .go files]
```

**Step 2:** Spawn brainstormer:
```
Task(subagent_type: "workflow-brainstormer",
     description: "Bootstrap from lth and research codebase",
     run_in_background: true,
     prompt: "You are a researcher/brainstormer.

     Before starting: if .bob/state/context.md exists, read it for pre-loaded lth context (principles, techniques, project history). Build on this rather than re-searching from scratch.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth search 'software architecture brainstorming research exploration' --layers L1,L2 --top 5
       ~/bin/lth search 'codebase research patterns exploration techniques' --layers L3 --top 8
       ~/bin/lth search '[task description]' --layers L4,L5 --top 5

     Apply what you find. Then:

     1. Read .bob/state/brainstorm-prompt.md
     2. Explore the codebase relevant to the task
     3. Consider multiple implementation approaches
     4. Identify risks, edge cases, constraints
     5. Write findings to .bob/state/brainstorm.md

     AFTER writing — store key findings back to lth:
       ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=brainstorm,research' '[key decision or insight]'

     Working directory: [worktree-path]")
```

**Output:** `.bob/state/brainstorm.md`

---

## Phase 4: PLAN

**Goal:** Create a detailed implementation plan as a task list.

**Step 1:** Invoke writing-plans skill or spawn planner:
```
Task(subagent_type: "workflow-planner",
     description: "Bootstrap from lth and create plan",
     run_in_background: true,
     prompt: "You are an implementation planner.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth search 'software planning task decomposition implementation strategy' --layers L1,L2 --top 5
       ~/bin/lth search 'planning implementation steps tdd test-driven' --layers L3 --top 10
       ~/bin/lth search '[task description]' --layers L4,L5 --top 5

     Apply what you find as your planning principles. Then:

     1. Read .bob/state/brainstorm.md
     2. Create a concrete, bite-sized plan:
        - Exact file paths
        - Function signatures and types
        - TDD approach (tests first)
        - Step-by-step actions (2-5 min each)
        - Integration and verification steps
     3. Write plan to .bob/state/plan.md

     AFTER writing — store key planning decisions:
       ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=planning,architecture' '[key decision]'

     Working directory: [worktree-path]")
```

**Step 2:** After planner completes, read `.bob/state/plan.md`.

**Step 3:** Convert plan to task list via TaskCreate. Break into:
- Setup tasks
- Implementation tasks (mark `task_type: "implementation"`)
- Test tasks (mark `task_type: "test"`, addBlockedBy: implementation tasks)
- Integration tasks
- Quality tasks

---

## Phase 5: SPAWN TEAM

**Goal:** Create team and spawn self-prompting teammates.

**Step 1:** Create agent team (2 coders, 2 reviewers, Sonnet model).

**Step 2:** Spawn coder-1:
```
"Spawn teammate 'coder-1':

You are a software engineer (coder-1).

First read .bob/state/context.md if it exists — this contains pre-loaded lth memory context.

BEFORE WRITING ANY CODE — bootstrap your guidance from lth:
  ~/bin/lth stats
  ~/bin/lth search 'software engineering code quality correctness' --layers L1,L2 --top 5
  ~/bin/lth search 'implementation coding best practices patterns' --layers L3 --top 10
  ~/bin/lth search '[task description] implementation' --layers L4,L5 --top 5

Apply what you find as your coding principles.

Your job:
1. Check TaskList for available tasks (pending, no blockedBy, no owner)
2. Claim a task: TaskUpdate(status: in_progress, owner: coder-1)
3. Read task: TaskGet
4. Read the plan: .bob/state/plan.md
5. Implement — use TDD (tests first for implementation tasks)
6. Mark task completed
7. Repeat until no tasks available

SPEC-DRIVEN MODULES: Before editing any directory, check for SPECS.md, NOTES.md,
TESTS.md, BENCHMARKS.md, or .go files with '// NOTE: Any changes...'. If found,
update those docs alongside code changes.

AFTER all tasks done — store what you learned:
  ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=implementation' '[pattern or decision that worked]'

Report to team lead: WHAT implemented, WHERE (file:line), decisions made.
Working directory: [worktree-path]"
```

**Step 3:** Spawn coder-2 (same prompt, name changed to coder-2).

**Step 4:** Spawn reviewer-1:
```
"Spawn teammate 'reviewer-1':

You are a code reviewer (reviewer-1).

BEFORE REVIEWING ANY CODE — bootstrap your guidance from lth:
  ~/bin/lth stats
  ~/bin/lth search 'code review quality standards correctness' --layers L1,L2 --top 5
  ~/bin/lth search 'code review patterns bugs security performance go' --layers L3 --top 10
  ~/bin/lth search '[task description] review' --layers L4,L5 --top 5

Apply what you find as your review criteria.

Your job:
1. Monitor TaskList for completed, unreviewed tasks
2. Claim: TaskUpdate(metadata.reviewing: true, reviewer: reviewer-1)
3. Read task: TaskGet — understand what was implemented
4. Review: read changed files, check quality/correctness/tests/error handling
5. Decide:
   - APPROVE: TaskUpdate(metadata.reviewed: true, approved: true)
   - NEEDS_FIXES: TaskUpdate(metadata.reviewed: true, approved: false, needs_fix: true)
     AND TaskCreate describing exactly what to fix (WHAT/WHY/WHERE)
6. Repeat until all completed tasks reviewed

AFTER all tasks reviewed — store key findings:
  ~/bin/lth store --layer 3 --attr 'topic=review' --attr 'tags=code-review,go' '[review pattern or insight]'
  ~/bin/lth store --layer 4 --attr 'project=[repo]' '[specific finding worth remembering]'

Report to team lead: task reviewed, APPROVED or NEEDS_FIXES with specifics (severity/WHAT/WHY/WHERE).
Working directory: [worktree-path]"
```

**Step 5:** Spawn reviewer-2 (same prompt, name changed to reviewer-2).

**Step 6:** Verify all 4 teammates active.

---

## Phase 6: EXECUTE + REVIEW (Concurrent)

**Goal:** Coders implement, reviewers review — concurrently.

**Step 1:** Broadcast kickoff:
```
"Broadcast: Let's go. Task list has [N] tasks.
Coders: claim and implement. Reviewers: review completed work as it comes in.
Flag blockers immediately."
```

**Step 2:** Monitor TaskList periodically.

**Step 3:** Handle teammate messages — acknowledge, clarify, redirect.

**Step 4:** Route when done:
- All tasks complete + approved → TEST phase
- HIGH/CRITICAL issues → BRAINSTORM
- MEDIUM/LOW issues → stay in EXECUTE (create fix tasks)

---

## Phase 7: TEST

**Goal:** Run full test suite and quality checks.

```
Task(subagent_type: "workflow-tester",
     description: "Bootstrap from lth and run tests",
     run_in_background: true,
     prompt: "You are a test runner.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth search 'testing quality gates ci pipeline verification' --layers L1,L2 --top 5
       ~/bin/lth search 'go testing test suite quality checks lint' --layers L3 --top 8
       ~/bin/lth search '[task description] testing' --layers L4,L5 --top 5

     Apply what you find. Then:

     1. Run make ci — or if unavailable, run individually:
        go test ./... (report all results)
        go test -race ./... (report races)
        go test -cover ./... (report coverage)
        go fmt (report formatting issues)
        golangci-lint run (report lint)
        gocyclo -over 40 (report complex functions)
     2. Write ALL results to .bob/state/test-results.md
        For each finding: WHAT, WHY, WHERE (file:line, test name)
        Do NOT make pass/fail judgments — just report facts.

     AFTER — store notable findings:
       ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=testing' '[test finding or pattern]'

     Working directory: [worktree-path]")
```

**Route:** tests pass → REVIEW; tests fail → EXECUTE (message coders to create fix tasks).

---

## Phase 8: REVIEW (Final)

**Goal:** Shut down team and run final holistic review, fix, commit, and CI monitoring.

**Step 1:** Shut down teammates (message each to shut down, wait for confirmation).

**Step 2:** Invoke code-review skill:
```
Invoke: /bob:code-review
```

This handles the full cycle: multi-domain review → fix loop → commit → CI monitoring.

---

## Phase 9: STORE

**Goal:** Persist workflow-level findings back to lth for future sessions.

After code-review completes, store a workflow summary:

```bash
~/bin/lth store --layer 4 \
  --attr "project=[repo-name]" \
  --attr "tags=workflow,completed" \
  "[What was built, key decisions made, what worked well, what was difficult]"
```

Store any high-value insights at L3 if they represent reusable techniques:
```bash
~/bin/lth store --layer 3 \
  --attr "topic=[domain]" \
  --attr "tags=[relevant,tags]" \
  "[Reusable technique or pattern discovered during this workflow]"
```

---

## Phase 10: COMPLETE

**Actions:**

1. Clean up agent team.

2. Confirm with user:
   ```
   "Workflow complete.

   Built: [feature]
   Findings stored to lth memory for future sessions.

   Shall we merge into main? [yes/no]"
   ```

3. If approved: `gh pr merge --squash`

---

## State Files

```
.bob/state/brainstorm-prompt.md  — input for brainstormer
.bob/state/brainstorm.md         — brainstormer findings
.bob/state/context.md            — pre-loaded lth context (from lth prompt)
.bob/state/plan.md               — implementation plan
.bob/state/test-results.md       — test execution results
.bob/state/review.md             — final review findings
```

---

## Autonomous Progression

The team lead drives forward without stopping. The only user prompt is the final merge confirmation.

Never output: "Should I continue?", "Do you want me to proceed?", "Shall I move to the next phase?"

Brief status updates between phases:
```
✓ BRAINSTORM complete → .bob/state/brainstorm.md
Moving to PLAN...

✓ PLAN complete, 8 tasks created
Spawning team...

✓ All tasks complete and approved → routing to TEST

✓ Tests passing → routing to final REVIEW
```
