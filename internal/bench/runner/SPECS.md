# runner — Specifications

## Invariants

1. `RunOne` is the only entry point for executing a problem × approach; callers must not call sub-methods directly.
2. `RunOne` always returns a `Result` — it never panics or returns an error. All failures are encoded in `Result.Outcome`.
3. All git operations target a throwaway worktree in `os.MkdirTemp`; the cache directory is never mutated after initial clone.
4. `runClaude` passes the prompt on stdin; no prompt data is included in CLI arguments (avoids injection via shell quoting).
5. Claude's working directory is set to `repoDir` via `cmd.Dir`; the surrounding process working directory is never changed.
6. `ClaudeTimeout` applies only to the `claude` invocation; git and test operations use the passed-in context.
