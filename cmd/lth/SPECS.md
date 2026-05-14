# cmd/lth — Invariants

1. Every command that touches the DB calls `ensureDaemon` before executing (except `watch` and `config` subcommands).
2. `--json` always produces valid JSON to stdout even on partial errors.
3. All error messages go to stderr (never stdout).
4. A non-zero exit code is returned on any error.
5. `lth watch daemon` is a hidden Cobra command; it is not shown in `--help`.
6. `lth config init` does not start the daemon.
