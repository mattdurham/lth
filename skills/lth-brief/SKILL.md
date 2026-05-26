---
name: lth:brief
description: Generate a structured task brief from lth memory before starting work
user-invocable: true
category: memory
---

# lth:brief — Task Brief Generator

Transforms a one-line task description into a structured brief that agents can act on directly. Pulls prior art, constraints, and applicable patterns from lth memory.

## Usage

```
/lth:brief "Add rate limiting to API"
```

## Output

Writes `.bob/state/brief.md` with:
- **Context** — relevant memories from lth (principles, techniques, recent history)
- **Prior Art** — similar work done before (from L4/L5 memory)
- **Constraints** — rules and invariants that apply (from L1/L2)
- **Suggested Approach** — synthesized from above

## Instructions

ARGUMENTS handling: use the provided argument as the task description.

1. Ensure daemon is running:
   ```bash
   export PATH="$HOME/bin:$PATH"
   ~/bin/lth stats
   ```

2. Generate context using lth prompt:
   ```bash
   CONTEXT=$(~/bin/lth prompt "[TASK_DESCRIPTION]" --top-each 6 2>/dev/null)
   ```
   If lth prompt is not yet available (pre-install), fall back to:
   ```bash
   PRINCIPLES=$(~/bin/lth search "[TASK_DESCRIPTION]" --layers L1,L2 --top 5)
   TECHNIQUES=$(~/bin/lth search "[TASK_DESCRIPTION]" --layers L3 --top 8)
   PRIOR_ART=$(~/bin/lth search "[TASK_DESCRIPTION]" --layers L4,L5 --top 5)
   ```

3. Create .bob/state/ directory if needed:
   ```bash
   mkdir -p .bob/state
   ```

4. Write `.bob/state/brief.md` with this structure:
   ```markdown
   # Task Brief: [TASK_DESCRIPTION]

   Generated: [date]

   ## Context
   [paste lth prompt output or formatted principles/techniques]

   ## Prior Art
   [L4/L5 memories showing similar past work]

   ## Constraints
   [L1/L2 principles and rules that apply to this task]

   ## Suggested Approach
   [synthesize from above: 3-5 bullet points recommending how to tackle the task]
   ```

5. Print confirmation: `✓ Brief written to .bob/state/brief.md`
