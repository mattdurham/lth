# internal/gwswatcher — Invariants

1. The watcher only runs when `cfg.GWS.Enabled` is true. With it false, `Run` returns immediately and starts no goroutines.
2. `New(cfg)` resolves the `gws` binary via `exec.LookPath` at construction. If `gws` is not on PATH (or at the `gws_binary` override), `New` returns an error and the caller must skip starting the watcher.
3. Each scan cycle calls the `gws` CLI exactly twice per new-or-updated doc: one `drive files list` and one `docs documents get`. No other Google APIs are invoked.
4. The watcher delegates ALL authentication to the `gws` CLI. lth never reads, persists, or transmits Google credentials.
5. `ScanOnce` writes one markdown file per matched doc into `cfg.GWS.OutputDir`, named `<YYYY-MM-DD>_<slug>__<docID>.md`. The filename embeds the doc ID so the same doc maps to the same file across cycles.
6. A doc whose on-disk file has an `mtime >= driveFile.modifiedTime` is `upToDate` and is NOT re-fetched. After writing, the watcher stamps the file's mtime to the Drive `modifiedTime` so this check is stable across daemon restarts.
7. Per-doc fetch or write failures are logged at WARN and the cycle continues with the remaining docs. The cycle as a whole only returns an error from `ScanOnce` if the directory cannot be created or the Drive list call fails.
8. `buildDriveQuery` always includes `mimeType='application/vnd.google-apps.document'` and a `modifiedTime > sinceUTC` clause. Include patterns are OR'd; exclude patterns are NAND'd against the include result. Quotes in patterns are escaped via `escapeDriveString` per Drive query syntax.
9. `docToMarkdown` maps `TITLE`/`HEADING_1..HEADING_6` to `#`..`######` prefixes, bullets to `- ` (indented by `nestingLevel * 2` spaces), tables to pipe-delimited rows, and skips section breaks, images, equations, and other unsupported element kinds. Unrecognised elements never cause an error.
10. The watcher writes only into the configured `OutputDir`. It never reads, modifies, or deletes files outside that directory.
