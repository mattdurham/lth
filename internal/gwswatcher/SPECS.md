# internal/gwswatcher — Invariants

1. The watcher only scans when `cfg.GWS.Enabled` is true, but `Run` itself always loops forever regardless -- see invariant 11 for the hot-reload-friendly loop-and-recheck shape. `Run` never returns early and never starts zero goroutines; disabling `Enabled` just idles the existing loop on a 60s recheck.
2. `New(cfg)` never resolves the `gws` binary and never returns an error -- see invariant 12. Binary resolution is lazy, deferred to the first scan after the watcher is enabled.
3. Each scan cycle calls the `gws` CLI exactly twice per new-or-updated doc: one `drive files list` and one `docs documents get`. No other Google APIs are invoked.
4. The watcher delegates ALL authentication to the `gws` CLI. lth never reads, persists, or transmits Google credentials.
5. `ScanOnce` writes one markdown file per matched doc into `cfg.GWS.OutputDir`, named `<YYYY-MM-DD>_<slug>__<docID>.md`. The filename embeds the doc ID so the same doc maps to the same file across cycles.
6. A doc whose on-disk file has an `mtime >= driveFile.modifiedTime` is `upToDate` and is NOT re-fetched. After writing, the watcher stamps the file's mtime to the Drive `modifiedTime` so this check is stable across daemon restarts.
7. Per-doc fetch or write failures are logged at WARN and the cycle continues with the remaining docs. The cycle as a whole only returns an error from `ScanOnce` if the directory cannot be created or the Drive list call fails.
8. `buildDriveQuery` always includes `mimeType='application/vnd.google-apps.document'` and a `modifiedTime > sinceUTC` clause. Include patterns are OR'd; exclude patterns are NAND'd against the include result. Quotes in patterns are escaped via `escapeDriveString` per Drive query syntax.
9. `docToMarkdown` maps `TITLE`/`HEADING_1..HEADING_6` to `#`..`######` prefixes, bullets to `- ` (indented by `nestingLevel * 2` spaces), tables to pipe-delimited rows, and skips section breaks, images, equations, and other unsupported element kinds. Unrecognised elements never cause an error.
10. The watcher writes only into the configured `OutputDir`. It never reads, modifies, or deletes files outside that directory.
11. `Run` is hot-reload friendly: it loops forever, re-checking `cfg.GWS.Enabled` on each iteration. While disabled it sleeps for 60s between checks; while enabled it sleeps for `cfg.GWS.IntervalH` hours between scans. Toggling `Enabled` via in-place config reload takes effect on the next poll without restarting the goroutine.
12. `New(cfg)` does not require `gws` to be on PATH; the binary is resolved lazily by `ensureRunner` on the first scan after the watcher transitions to enabled. The missing-binary warning is logged at most once per watcher lifetime.
