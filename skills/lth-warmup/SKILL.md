---
name: lth:warmup
description: Session warmup — surface recent project context from lth memory
user-invocable: true
category: memory
---

# lth:warmup — Session Warmup

Surface what happened in previous sessions for the current project. Run this at the start of a session to reconstruct context without re-reading code.

## What it does

Searches lth memory for recent work (L4/L5) tagged with the current project directory, then prints a "here's where we left off" summary.

## Usage

```
/lth:warmup
```

No arguments needed — project context is detected automatically from your working directory.

## Instructions

1. Get current working directory:
   ```bash
   CWD=$(pwd)
   PROJECT=$(basename $CWD)
   ```

2. Search for recent project context:
   ```bash
   export PATH="$HOME/bin:$PATH"
   ~/bin/lth stats
   ~/bin/lth search "project work session recent decisions" --layers L4,L5 --top 15
   ```

3. From the results, filter for memories relevant to this project (look for cwd attribute matching current directory, or content mentioning the project name).

4. Format and print a handoff brief:

   ```
   ## Session Warmup: [project name]

   ### Recent Work
   [bullet points from L4/L5 memories about what was built]

   ### Key Decisions
   [decisions or patterns found in memories]

   ### Suggested Next Steps
   [any "next step" mentions, or leave empty if none]
   ```

5. If no relevant memories found, print:
   ```
   No recent memories found for this project.
   Run /lth:reflect after work sessions to build context.
   ```
