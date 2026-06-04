---
name: lth:grv
description: Memory-driven team workflow with AST-aware code editing via grv — agents read and write Go code through the grv tool instead of raw file I/O. INIT → WORKTREE → BRAINSTORM → PLAN → EXECUTE → REVIEW → COMPLETE
user-invocable: true
category: workflow
requires_experimental: agent_teams
---

# lth:grv — Memory-Driven Workflow with AST-Aware Code Editing

<!-- AGENT CONDUCT: Be direct and challenging. Flag gaps, risks, and weak ideas proactively. Hold your ground and explain your reasoning clearly. -->

This is `lth:work` with one key difference: **all agents read and write Go code through the `grv` tool** instead of raw Read/Edit/Write file I/O. `grv` is an AST-aware code manipulation binary that understands Go structure — functions, types, imports, symbols — and prevents the class of edits that silently corrupt syntax or miss references.

For non-Go files (YAML, markdown, JSON, shell scripts), agents use `grv file_read` and `grv file_write`.

---

## grv Quick Reference

`grv` is at `grv`. All agents must use it. Syntax: `grv <command> --flag value`.

### Reading Code

| Command | What it does | Example |
|---|---|---|
| `ast_directory` | Inventory all files + symbols in a dir | `grv ast_directory --dir cmd/lth/` |
| `ast_list` | List top-level declarations in a file | `grv ast_list --file cmd/lth/sync.go` |
| `ast_find_symbols` | Find symbols by name across a dir | `grv ast_find_symbols --dir cmd/lth/ --query syncPull` |
| `ast_query` | Get the AST node at a path | `grv ast_query --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'` |
| `ast_query_many` | Get multiple nodes in one call | `grv ast_query_many --file cmd/lth/sync.go --paths '[...]'` |
| `ast_meta` | Line, col, complexity, size for a node | `grv ast_meta --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'` |
| `ast_node_at` | Find node at a specific line/col | `grv ast_node_at --file cmd/lth/sync.go --line 200 --col 1` |
| `ast_find` | Find nodes matching a JSON pattern | `grv ast_find --file cmd/lth/sync.go --pattern '{"kind":"CallExpr"}'` |
| `ast_find_refs` | Find all references to a symbol | `grv ast_find_refs --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'` |
| `ast_find_def` | Find definition of an identifier | `grv ast_find_def --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'` |
| `ast_find_impls` | Find types implementing an interface | `grv ast_find_impls --file pkg/store/store.go --path '[{"kind":"TypeSpec","name":"Store"}]'` |
| `ast_list_imports` | List imports with usage info | `grv ast_list_imports --file cmd/lth/sync.go` |
| `file_read` | Read any non-Go file | `grv file_read --file README.md` |

**Note:** `grv file_read` rejects `.go` files — use `ast_*` tools for those.

### Writing Code

| Command | What it does | Example |
|---|---|---|
| `ast_replace` | Replace a node at a selector path | `grv ast_replace --file f.go --path '[...]' --node '{...}' --dry_run true` |
| `ast_insert` | Insert a new node into a list container | `grv ast_insert --file f.go --path '[...]' --index 0 --node '{...}'` |
| `ast_delete` | Delete a node from a list container | `grv ast_delete --file f.go --path '[...]'` |
| `ast_rename` | Rename identifier at declaration site | `grv ast_rename --file f.go --path '[...]' --to newName` |
| `ast_add_import` | Add an import to a Go file | `grv ast_add_import --file f.go --path '"context"'` |
| `ast_delete_import` | Remove an import | `grv ast_delete_import --file f.go --path '"context"'` |
| `file_write` | Write any non-Go file | `grv file_write --file README.md --content 'text...'` |
| `gomod_require` | Add/update a go.mod require | `grv gomod_require --file go.mod --path github.com/foo/bar --version v1.2.3` |
| `gomod_drop_require` | Remove a go.mod require | `grv gomod_drop_require --file go.mod --path github.com/foo/bar` |

**Always use `--dry_run true` first for write operations.** Review the diff, then re-run without it.

### Path Selector Format

AST paths are JSON arrays of step objects. The `ast_list` and `ast_find_symbols` output gives you exact paths:

```json
[{"kind":"FuncDecl","name":"syncPull"}]
[{"kind":"TypeSpec","name":"syncCfg"},{"kind":"StructType"},{"kind":"FieldList"},{"kind":"Field","name":"url"}]
```

Use the `path` field from `ast_find_symbols` output directly as your `--path` value.

---

## grv Examples

### Explore a package before editing

```bash
# What's in this directory?
grv ast_directory --dir cmd/lth/

# What's in one file?
grv ast_list --file cmd/lth/sync.go

# Find a specific function
grv ast_find_symbols --dir cmd/lth/ --query syncPull

# Read its full source
grv ast_query --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'

# Check its complexity and line range
grv ast_meta --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'
```

### Safe function replacement

```bash
# 1. Read current body
grv ast_query --file cmd/lth/sync.go --path '[{"kind":"FuncDecl","name":"syncPull"}]'

# 2. Dry-run the replacement
grv ast_replace --file cmd/lth/sync.go \
  --path '[{"kind":"FuncDecl","name":"syncPull"}]' \
  --node '{"kind":"FuncDecl","name":"syncPull","body":{"kind":"BlockStmt","list":[...]}}' \
  --dry_run true

# 3. If diff looks correct, apply
grv ast_replace --file cmd/lth/sync.go \
  --path '[{"kind":"FuncDecl","name":"syncPull"}]' \
  --node '{"kind":"FuncDecl","name":"syncPull","body":{"kind":"BlockStmt","list":[...]}}' \
  --dry_run false
```

### Add a function to a file

```bash
grv ast_insert --file cmd/lth/sync.go \
  --path '[{"kind":"FuncDecl","name":"syncPull"}]' \
  --index 1 \
  --node '{"kind":"FuncDecl","name":"newHelper","type":{"kind":"FuncType","params":{"kind":"FieldList"}},"body":{"kind":"BlockStmt","list":[]}}' \
  --dry_run true
```

### Non-Go files

```bash
grv file_read --file .bob/state/plan.md
grv file_write --file .bob/state/notes.md --content "key finding here"
```

---

## lth Binary

The lth binary is at `~/bin/lth`. All agents use this path. `lth stats` starts the daemon if not running.

---

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

---

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
export LTH_ACTIVE=1  # enables file-level lth context injection on every Read
~/bin/lth stats      # start daemon if not running
~/bin/lth prompt "<task or role query>"
```

`lth prompt` runs layered searches (L1/L2 principles, L3 techniques, L4 context) plus PPR graph expansion in one call. Apply findings as operating principles. If lth returns nothing, proceed with general knowledge.

---

## Team Architecture

```
Team Lead (You)
  ├── coder-1   (self-prompted via lth, edits via grv)
  ├── coder-2   (self-prompted via lth, edits via grv)
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
   "lth:grv — memory-driven workflow with AST-aware code editing starting.

   Task: [feature description]

   Agents will bootstrap guidance from lth and edit Go code through grv.
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
     description: "Bootstrap from lth, explore codebase with grv",
     run_in_background: true,
     prompt: "You are a researcher/brainstormer.

     Before starting: if .bob/state/context.md exists, read it for pre-loaded lth context.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth prompt '[task description]'

     Apply what you find. Then explore the codebase using grv:

       # Inventory the relevant package
       grv ast_directory --dir <relevant-dir>/

       # Find symbols related to the task
       grv ast_find_symbols --dir <relevant-dir>/ --query <keyword>

       # Read specific functions
       grv ast_query --file <file> --path '[{"kind":"FuncDecl","name":"<fn>"}]'

     Then:
     1. Read .bob/state/brainstorm-prompt.md
     2. Consider multiple implementation approaches
     3. Identify risks, edge cases, constraints
     4. Write findings to .bob/state/brainstorm.md

     AFTER writing — store key findings back to lth:
       ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=brainstorm,research' '[key decision or insight]'

     Working directory: [worktree-path]")
```

**Output:** `.bob/state/brainstorm.md`

---

## Phase 4: PLAN

**Goal:** Create a detailed implementation plan as a task list.

```
Task(subagent_type: "workflow-planner",
     description: "Bootstrap from lth and create plan",
     run_in_background: true,
     prompt: "You are an implementation planner.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth prompt '[task description]'

     Apply what you find as your planning principles.

     Use grv to understand the current code structure before planning:
       grv ast_directory --dir <relevant-dir>/
       grv ast_find_symbols --dir <relevant-dir>/ --query <keyword>

     Then:
     1. Read .bob/state/brainstorm.md
     2. Create a concrete, bite-sized plan:
        - Exact file paths
        - Function signatures and types (use grv ast_query to read existing signatures)
        - TDD approach (tests first)
        - Step-by-step actions (2-5 min each)
        - Integration and verification steps
     3. Write plan to .bob/state/plan.md

     AFTER writing — store key planning decisions:
       ~/bin/lth store --layer 4 --attr 'project=[repo]' --attr 'tags=planning,architecture' '[key decision]'

     Working directory: [worktree-path]")
```

After planner completes: read `.bob/state/plan.md`, then create tasks via TaskCreate.

---

## Phase 5: SPAWN TEAM

**Goal:** Create team and spawn self-prompting teammates.

**Step 1:** Create agent team (2 coders, 2 reviewers, Sonnet model).

**Step 2:** Spawn coder-1:
```
"Spawn teammate 'coder-1':

You are a software engineer (coder-1).

IMPORTANT: You read and write ALL Go code through the grv tool.
grv is at grv. Syntax: grv <command> --flag value

NEVER use Read/Edit/Write tools on .go files. Use grv instead:
  - Read Go code:  grv ast_list, grv ast_query, grv ast_find_symbols, grv ast_directory
  - Write Go code: grv ast_replace, grv ast_insert, grv ast_delete, grv ast_rename
  - Non-Go files:  grv file_read, grv file_write

Always dry_run first: add --dry_run true, review the output, then apply without it.

First read .bob/state/context.md if it exists — pre-loaded lth memory context.

BEFORE WRITING ANY CODE — bootstrap your guidance from lth:
  ~/bin/lth stats
  ~/bin/lth prompt '[task description]'

Apply what you find as your coding principles.

Your job:
1. Check TaskList for available tasks (pending, no blockedBy, no owner)
2. Claim a task: TaskUpdate(status: in_progress, owner: coder-1)
3. Read task: TaskGet
4. Read the plan: grv file_read --file .bob/state/plan.md
5. Before editing any file — read it with grv to understand structure:
   grv ast_directory --dir <dir>/
   grv ast_list --file <file>
   grv ast_query --file <file> --path '[{"kind":"FuncDecl","name":"<fn>"}]'
   Also run: ~/bin/lth read <filepath>  (injects lth context before the file content)
6. Implement using TDD (tests first for implementation tasks)
7. Mark task completed
8. Repeat until no tasks available

SPEC-DRIVEN MODULES: Before editing any directory, check for SPECS.md, NOTES.md,
TESTS.md, BENCHMARKS.md, or .go files with '// NOTE: Any changes...'. If found,
update those docs using grv file_read / grv file_write alongside code changes.

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

Use grv to read code during review — it gives you structural context beyond raw text:
  grv ast_list --file <file>             # see declarations
  grv ast_meta --file <file> --path [...] # check complexity
  grv ast_find_refs --file <file> --path [...] # check for unreferenced symbols

BEFORE REVIEWING — bootstrap your guidance from lth:
  ~/bin/lth stats
  ~/bin/lth prompt '[task description] code review'

Apply what you find as your review criteria.

Your job:
1. Monitor TaskList for completed, unreviewed tasks
2. Claim: TaskUpdate(metadata.reviewing: true, reviewer: reviewer-1)
3. Read task: TaskGet — understand what was implemented
4. Review with grv: read changed files, check quality/correctness/tests/error handling
5. Decide:
   - APPROVE: TaskUpdate(metadata.reviewed: true, approved: true)
   - NEEDS_FIXES: TaskUpdate(metadata.reviewed: true, approved: false, needs_fix: true)
     AND TaskCreate describing exactly what to fix (WHAT/WHY/WHERE with grv path selectors)
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

**Goal:** Coders implement via grv, reviewers review — concurrently.

**Step 1:** Broadcast kickoff:
```
"Broadcast: Let's go. Task list has [N] tasks.
Coders: claim and implement using grv for all Go file edits.
Reviewers: review completed work with grv as it comes in.
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

```
Task(subagent_type: "workflow-tester",
     description: "Bootstrap from lth and run tests",
     run_in_background: true,
     prompt: "You are a test runner.

     FIRST — bootstrap your guidance from lth:
       ~/bin/lth stats
       ~/bin/lth prompt '[task description] testing'

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

---

## Phase 9: STORE

**Step 1:** Run `/lth:reflect` to automatically extract and store learnings:
```
Invoke: /lth:reflect
```

**Step 2:** Store a workflow-level summary:
```bash
~/bin/lth store --layer 4 \
  --attr "project=[repo-name]" \
  --attr "tags=workflow,completed" \
  "[What was built, key decisions made, what worked well, what was difficult]"
```

Store high-value insights at L3 if reusable:
```bash
~/bin/lth store --layer 3 \
  --attr "topic=[domain]" \
  --attr "tags=[relevant,tags]" \
  "[Reusable technique or pattern discovered during this workflow]"
```

---

## Phase 10: COMPLETE

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
