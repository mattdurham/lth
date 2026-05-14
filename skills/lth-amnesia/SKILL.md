---
name: lth:amnesia
description: Bootstrap agent memory from lth — reconstruct context from stored memories before working, store findings after
user-invocable: true
category: memory
---

# lth:amnesia — Agent Memory Bootstrap

You are an agent with amnesia. You have no session memory, but you have a vast persistent memory
store in lth. Your first task before doing ANY work is to reconstruct your context from memory.

## Prerequisites

lth must be installed and the daemon must be running:
```bash
which lth          # should print ~/bin/lth or similar
lth stats          # starts daemon automatically, shows memory counts
```

If lth is not installed: `cd ~/source/lth && make install`
If ANTHROPIC_API_KEY is not set: tag extraction and importance scoring will default silently.

---

## Phase 1: BOOTSTRAP — Reconstruct context from memory

Before writing a single line of code or answering any question, run these searches:

### Step 1: Load your identity and principles (L1/L2)
```bash
lth search "<task or question>" --layers L1,L2 --top 10 --json
```
Read the results. These are your core principles and behavioral rules. They constrain your approach.
If L1/L2 results are empty, the memory store is unseeded — skip to Phase 2.

### Step 2: Find relevant skills and tools (L3)
```bash
lth search "<task or question>" --layers L3 --top 10 --json
```
These are specific techniques, tool knowledge, and procedures relevant to the task.

### Step 3: Find recent situational context (L4/L5)
```bash
lth search "<task or question>" --layers L4,L5 --top 5 --json
```
Recent observations and episode memories — what happened last time something similar was attempted.

### Step 4: Search by tags if you know the domain
```bash
lth search "<task>" --tags go,security --top 10 --json
lth search "<task>" --tags debugging,error-handling --top 10 --json
```

### Step 5: Explore the memory graph from top results
Take the top 2-3 memory IDs from the searches above and traverse their graph:
```bash
lth graph show --from <id> --depth 2 --json
lth graph ppr --seeds <id1>,<id2> --top 10 --json
```
This surfaces related memories that didn't appear in direct search.

---

## Phase 2: CONSTRUCT — Build your approach

Synthesize what you found:

1. **From L1**: What are my core principles that apply here? List them explicitly.
2. **From L2**: What rules or heuristics are relevant? Note any constraints.
3. **From L3**: What tools, techniques, or procedures should I use?
4. **From L4/L5**: What happened before in similar situations? What worked? What failed?
5. **From graph**: What related concepts or patterns are connected to this task?

Write out your constructed approach before starting work. If memory is sparse, state that explicitly
and proceed with general knowledge — do not fabricate memories.

---

## Phase 3: WORK — Execute with memory-informed approach

Execute the task using your constructed approach. As you work, note:
- Decisions made and why
- Problems encountered and solutions found
- Anything surprising or non-obvious
- Tools or techniques that worked particularly well

---

## Phase 4: STORE — Persist findings back to memory

After completing work, store key findings. Be selective — not everything is worth storing.

### Store raw observations (L5 — auto-compacts later):
```bash
lth store --layer 5 --attr "task=<what you were doing>" "<specific observation or finding>"
lth store --layer 5 --attr "outcome=success" "Used X approach for Y problem, result was Z"
```

### Store skills discovered (L3 — if you learned a new technique):
```bash
lth store --layer 3 --attr "topic=<domain>" --attr "tags=<tag1>,<tag2>" "<procedure or technique>"
```
Example: `lth store --layer 3 --attr "topic=go" "Use errgroup.SetLimit(N) for bounded goroutine fan-out"`

### Store situational context (L4 — project or task specific):
```bash
lth store --layer 4 --attr "project=<name>" --attr "tags=<relevant>" "<what is true in this context>"
```

### Store guidance (L2 — only for hard-won rules you'll apply repeatedly):
```bash
lth store --layer 2 "Always validate external inputs at system boundaries before processing"
```

### Store core principles (L1 — very rarely, only for identity-level insights):
```bash
lth store --layer 1 "I prefer explicit error handling — silent failures hide bugs"
```

---

## Scoring Reference

Search results include a composite score: `α·recency + β·importance + γ·similarity`

- **High score** (>0.7): highly relevant + important + recent — follow this guidance closely
- **Medium score** (0.4–0.7): relevant but may be outdated or lower importance
- **Low score** (<0.4): tangentially related — useful context but don't over-weight it

The `TimeScore`, `ImportanceScore`, `VectorScore` breakdown in `--json` output shows which factor dominated.

---

## Seeding L1/L2 (first time setup)

If memory is empty, seed your identity before using the amnesia skill:

```bash
# Who you are
lth store --layer 1 "I am a software engineer — I value correctness, simplicity, and explicit code"

# Core engineering rules  
lth store --layer 2 "Always write tests before implementation (TDD)"
lth store --layer 2 "Prefer returning errors over panicking in library code"
lth store --layer 2 "One public type per file in Go packages"

# Domain skills
lth store --layer 3 --attr "topic=go" "Use context.Context as first parameter on all methods"
lth store --layer 3 --attr "topic=git" "Commit small and often with descriptive messages"
```

---

## Quick Reference

| Command | Purpose |
|---------|---------|
| `lth search "<query>" --layers L1,L2 --top 5` | Load identity + rules |
| `lth search "<query>" --layers L3 --top 10` | Find relevant skills |
| `lth search "<query>" --tags <tag> --top 5` | Tag-filtered search |
| `lth graph show --from <id> --depth 2` | Explore related memories |
| `lth store --layer 5 "<observation>"` | Save raw finding |
| `lth store --layer 3 --attr "topic=X" "<skill>"` | Save technique |
| `lth stats` | Show memory counts + graph size |
| `lth compact --dry-run` | Preview compaction |
| `lth config init` | Create default config |

---

## ARGUMENTS handling

When invoked as `/lth:amnesia <task description>`, use the task description as the search query
in all Phase 1 searches. If no argument is provided, ask the user what task they are working on
before proceeding.
