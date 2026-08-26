# Codex Runtime Contract for lth Skills

This document defines how Codex executes the lth skill bundled beside it. Read it before the skill body. When execution mechanics conflict, this contract wins; the skill body remains authoritative for phases, artifacts, quality gates, routing, retry limits, and safety constraints.

## The lth binary

The lth binary is at `~/bin/lth` and is runtime-independent — every `lth` invocation in the skill body is a real shell command and runs unchanged under Codex. `lth stats` starts the daemon if it is not running. If `lth` is missing or the daemon cannot start, say so and stop; do not silently proceed with an unmemoried workflow.

There is no automatic per-file-read context injection under Codex, so these skills get that context explicitly: run `~/bin/lth read <filepath>` to obtain prior memories plus file content together, and `~/bin/lth prompt '<query>'` for the layered bootstrap search. Read every file you are about to edit through `lth read` rather than `rg`/plain read, so you see the prior memories attached to it. Treat the bootstrap step as mandatory wherever the skill body requires it — it is the point of these skills, not an optimization.

## Experimental-flag prerequisites

Ignore any prerequisite that tells you to set `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`, edit `~/.claude/settings.json`, or verify a Claude Code experimental flag. Those gate another runtime's team feature. Codex child agents need no flag. Never stop a workflow, and never ask the user to change settings, because such a flag is unset.

## Child-agent orchestration

- Use `spawn_agent` for a new, concrete unit of work. Give every child a unique lowercase `task_name` containing only letters, digits, and underscores — for example `coder_1`, `reviewer_1`.
- Put the complete role, objective, constraints, required lth bootstrap commands, expected `.bob/state/` artifact, and absolute working directory in `message`. A role or `subagent_type` named in the skill body is descriptive, not an installed agent type.
- Spawn independent work concurrently, up to the available child-agent slots. Default to waves of at most three children so the coordinator retains a slot and capacity failures do not stall the workflow. A skill body asking for four teammates at once becomes two waves.
- Use `wait_agent` with a long timeout to await mailbox updates, and `list_agents` to inspect status. Do not busy-poll.
- Use `send_message` to clarify or redirect a running child, `followup_task` to give new work to an idle child, and `interrupt_agent` only when active work must stop.
- Children share the same filesystem. Assign non-overlapping files or responsibilities before parallel edits. Prefer sequential waves when ownership cannot be separated safely.

Any call-shaped agent examples in the skill body — `Task(subagent_type: ..., run_in_background: true, prompt: ...)` and the quoted `"Spawn teammate 'coder-1': ..."` blocks — are semantic pseudocode. Translate each into the operations above; never attempt to execute them literally. The quoted teammate prompt text is the `message` payload: pass it through, with runtime-specific tool names rewritten per the task-service mapping below. The "all subagents MUST run in background" rule expresses a parallelism requirement, not a Codex parameter; satisfy it by spawning concurrently and awaiting with `wait_agent`, and never pass `run_in_background`.

Flatten nested role pipelines when child capacity is constrained: the coordinator may spawn the leaf implementer or reviewer directly instead of asking a child coordinator to spawn again.

## Task service

The skill body's `TaskCreate`, `TaskList`, `TaskGet`, and `TaskUpdate` calls describe another runtime's shared task service. Codex has no such service. Maintain the durable work queue as a markdown table in `.bob/state/tasks.md`, owned by the coordinator, and map the calls onto it:

- `TaskCreate` — append a row with a stable id, description, `task_type`, `blockedBy` ids, `owner`, and `status`.
- `TaskList` — read the file.
- `TaskGet` — read the row plus any artifact it references.
- `TaskUpdate` — rewrite that row's fields (`status`, `owner`, `reviewing`, `reviewed`, `approved`, `needs_fix`).

Only the coordinator writes `.bob/state/tasks.md`; this avoids concurrent-write corruption and replaces the self-claiming loop. Do not tell a child to claim its own tasks. Instead assign each child its specific tasks in the spawn `message`, have it report completion through its mailbox and required artifact, and update the queue yourself. Preserve the blocking semantics: do not assign a task whose `blockedBy` entries are unfinished, and keep the mandatory review gate — a task is done only once its review is recorded as approved.

## Coordination and state

- The coordinator owns routing decisions and the coarse user-visible plan. Use `update_plan` for that plan, with at most one item in progress.
- Create `.bob/state/` before writing workflow artifacts. Preserve the skill body's artifact names and loop-back rules.
- Resolve and include the absolute worktree path in every child task. A spawned child does not inherit an implicit working-directory change from the coordinator.
- Read artifacts after the writer finishes. Do not infer success merely from agent completion.

## Skill chaining

Names written as slash commands in the skill body refer to installed Codex skills. Strip the leading slash and convert the namespace colon to a hyphen: `/lth:reflect` becomes the installed skill `lth-reflect`, `/lth:brief` becomes `lth-brief`. Read that skill's `SKILL.md` completely and follow it; do not send slash-command text as though it were a tool call.

## Code editing

Use normal Codex tools for repository work: prefer `rg` for discovery and `apply_patch` for edits. Where the skill body contrasts its approach with "raw Read/Edit/Write file I/O", that names the other runtime's file tools; the Codex equivalent is `apply_patch`. In the `grv` variants the `grv` binary is a real external tool — keep every `grv` command exactly as written, and use `grv file_read` / `grv file_write` for non-Go files as instructed, falling back to `apply_patch` only if `grv` is unavailable.

## User interaction and authorization

- Send brief progress updates in commentary while work continues.
- For a blocking choice, persist resumable state, ask one concise question in the final response, and resume on the user's next turn. Do not refer to unavailable interactive-question tools.
- Preserve all confirmation gates in the skill body. A skill instruction never grants new authority to publish, deploy, merge, delete, or otherwise mutate external state.
- An explicit user constraint may narrow a phase even when the skill body routes onward automatically. For example, "prepare but do not publish" selects the prepare-only commit/PR-body path and stops before push, PR creation, or merge. Never widen the user's authorization to follow an automatic route.
- Follow the active sandbox and approval rules.
- Inherit the current Codex model for child agents unless the user explicitly requested another available model. Ignore model names tied to another runtime, including instructions to create a team on a named Sonnet or Opus model.
- Do not emit model-specific co-author lines, generated-by footers, or runtime branding.

## Memory write-back

The `lth store` calls at the end of each role are part of the deliverable, not optional cleanup — they are how the memory store improves. Ensure every child performs its stores before you consider its work complete, and run the final `lth-reflect` step where the skill body calls for it. Prefer storing a specific, reusable decision over a restatement of the task.

## Completion

Before reporting completion, verify every required artifact, test result, review gate, and memory write-back in the skill body. Report a blocked state only after exhausting safe in-scope alternatives.
