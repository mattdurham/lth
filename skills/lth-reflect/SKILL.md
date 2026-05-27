---
name: lth:reflect
description: Retroactively store session learnings to lth memory without manual store calls
user-invocable: true
category: memory
---

# lth:reflect — Retroactive Session Enrichment

After completing work, automatically extracts key decisions and learnings from git history and workflow artifacts, then stores them to lth memory. Closes the feedback loop without requiring manual `lth store` calls.

## Usage

```
/lth:reflect
```

Run at the end of a session after committing code or finishing a workflow.

## What it stores

- **L4 memories**: Project-specific decisions, what was built, problems encountered
- **L3 memories**: Reusable techniques or patterns discovered (only if clearly generalizable)

## Instructions

1. Gather raw material:
   ```bash
   export PATH="$HOME/bin:$PATH"
   CWD=$(pwd)
   PROJECT=$(basename $CWD)

   # Recent git activity
   git log --oneline -5 2>/dev/null || echo "no git history"
   git diff HEAD~1 --stat 2>/dev/null || git diff --cached --stat 2>/dev/null || echo "no diff"
   git show HEAD --stat 2>/dev/null | head -20
   ```

2. Read workflow artifacts if present:
   ```bash
   cat .bob/state/brainstorm.md 2>/dev/null | head -50
   cat .bob/state/plan.md 2>/dev/null | head -30
   cat .bob/state/review.md 2>/dev/null | head -30
   ```

3. Analyze and identify what's worth storing:
   - What was built or changed (L4)
   - Key decisions made and why (L4)
   - Problems encountered and how they were solved (L4)
   - Any pattern/technique applicable beyond this project (L3)

   Do NOT store trivial observations — only hard-won findings.

4. Store each finding:
   ```bash
   # For project-specific decisions:
   ~/bin/lth store --layer 4 --attr "project=$PROJECT" --attr "tags=session-reflect" "[finding]"

   # For reusable techniques (only if clearly generalizable):
   ~/bin/lth store --layer 3 --attr "topic=[domain]" --attr "tags=[relevant,tags]" "[technique]"
   ```

5. Print summary:
   ```
   Stored N memories to lth:
   - [id1]: [preview of content]
   - [id2]: [preview of content]
   ...
   ```

## What makes a good reflection memory

Good:
- "Implemented X using Y approach because Z constraint — worked well"
- "Debugging tip: when A fails in this project, check B"
- "Pattern: use errgroup for bounded parallel DB queries in this codebase"

Not worth storing:
- "Edited file X" (too specific, no insight)
- "Tests pass" (not informative)
- Copies of code
