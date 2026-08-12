---
name: lth:grv-simple
description: Single-agent, memory-driven development workflow that edits Go code through the AST-aware grv tool instead of raw file I/O. INIT → PLAN → EXECUTE → TEST → REVIEW → COMPLETE.
user-invocable: true
category: workflow
---

# lth:grv — Single Agent, AST-Aware Editing

This is `lth:work-simple` with one difference: **you read and write all Go code
through the `grv` tool** instead of raw Read/Edit/Write. `grv` is an AST-aware
code manipulation binary that understands Go structure — functions, types,
imports, symbols — and prevents edits that silently corrupt syntax or miss
references. Non-Go files (YAML, markdown, JSON, shell) use `grv file_read` /
`grv file_write`.

Strictly single-agent: never call `Task`, `Agent`, `subagent`, create teammates,
or use agent teams. Do everything yourself.

## grv Quick Reference

`grv` is at `grv`. Syntax: `grv <command> --flag value`.

**Reading:** `ast_directory --dir`, `ast_list --file`, `ast_find_symbols --dir --query`,
`ast_query --file --path`, `ast_meta --file --path`, `ast_node_at --file --line --col`,
`ast_find --file --pattern`, `ast_find_refs --file --path`, `ast_find_def --file --path`,
`ast_find_impls --file --path`, `ast_list_imports --file`, `file_read --file` (non-Go only).

**Writing:** `ast_replace --file --path --node`, `ast_insert --file --path --index --node`,
`ast_delete --file --path`, `ast_rename --file --path --to`, `ast_add_import --file --path`,
`ast_delete_import --file --path`, `file_write --file --content` (non-Go only),
`gomod_require --file go.mod --path --version`, `gomod_drop_require --file go.mod --path`.

**Always `--dry_run true` first** for write operations. Review the diff, then
re-run without it.

Path selectors are JSON arrays, e.g. `[{"kind":"FuncDecl","name":"syncPull"}]`.
`ast_list` and `ast_find_symbols` output gives you exact paths to reuse.

## lth Binary

The lth binary is at `~/bin/lth`. `lth stats` starts the daemon if not running.

## Bootstrap pattern

```bash
export LTH_ACTIVE=1
~/bin/lth stats
~/bin/lth prompt "<task description>"
```

Apply what it returns as operating principles before researching or coding. If
it returns nothing, proceed with general knowledge.

## Steps

1. Bootstrap from lth (`lth prompt "<task>"`). Explore the relevant package with
   `grv ast_directory --dir <dir>/` and `grv ast_find_symbols --dir <dir>/ --query <keyword>`
   instead of reading raw file text.
2. Form a concise implementation plan (exact file paths, function signatures read
   via `grv ast_query`, TDD approach) and record it in `.bob/state/plan.md` via
   `grv file_write` (or plain Write for non-Go artifacts).
3. Implement the change through `grv`:
   - Read a function's current body with `grv ast_query` before touching it.
   - Dry-run every mutation (`ast_replace`/`ast_insert`/`ast_delete`/`ast_rename`)
     with `--dry_run true`, review the diff, then apply.
   - Never use Read/Edit/Write on `.go` files.
   - Update SPECS.md/NOTES.md/TESTS.md (via `grv file_write`) alongside code in
     spec-driven directories.
4. Run tests and quality checks (`make ci`, or `go test ./...`, `go vet ./...`,
   `go fmt`, `golangci-lint run`).
5. Review the diff yourself — use `grv ast_meta` for complexity and
   `grv ast_find_refs` to check nothing was left dangling. Fix issues found and
   rerun verification.
6. Store what you learned back to lth:
   ```bash
   ~/bin/lth store --layer 4 --attr "project=<repo>" --attr "tags=workflow,completed" \
     "<what was built, key decisions, what worked, what was difficult>"
   ```
7. Report changed files, verification results, remaining risks, and routing
   recommendation. Commit only when the user requested a commit.

If blocked by a missing decision or external state, explain the exact blocker
instead of spawning another agent.
