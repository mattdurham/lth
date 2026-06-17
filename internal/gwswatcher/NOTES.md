# internal/gwswatcher — Design Notes

## 1. Shell Out to the `gws` CLI, Don't Embed Google Client Libs

*Added: 2026-06-17*

**Decision:** The watcher invokes the `gws` CLI (Google Workspace CLI from
npm) via `exec.CommandContext` rather than calling the Google Drive / Docs /
Meet REST APIs directly with embedded client libraries.

**Rationale:** Identical reasoning to `internal/issueswatcher` shelling out
to `gh`: avoid pulling in the substantial Google API Go client library tree,
let the user's existing OAuth/refresh setup do the auth, and keep the lth
codebase free of credential handling. The `gws` binary already handles
keyring storage, refresh tokens, scope checks, and quota project routing -
duplicating any of that in lth would be a security and maintenance hazard.

**Consequence:** The daemon requires `gws` on the user's PATH (or at the
configured `gws_binary` path) to enable this watcher. `New` returns an error
when the binary is missing; the daemon logs a warning and starts without
this component. No transitive Go dependencies were added.

---

## 2. Produce Files, Don't Ingest Directly

*Added: 2026-06-17*

**Decision:** The watcher writes markdown files into `cfg.GWS.OutputDir`
and relies on the existing `internal/mdwatcher` to ingest them as memories.
The daemon auto-appends `OutputDir` to `cfg.Markdown.Dirs` at startup so the
user does not have to wire the two together manually.

**Rationale:** The markdown watcher already has battle-tested fact
extraction, hash-based deduplication, soft-deletion on file removal, and
format-aware chunking via `splitForLLM`. Re-implementing that logic in this
package would duplicate the most subtle code in the project. The producer/
consumer split also means a user can disable `gws.enabled` without losing
already-ingested files, or point the markdown watcher at a different
directory and still benefit from prior cycles' downloads.

**Consequence:** New code in this package is ~600 LOC of CLI plumbing and
Docs-to-markdown conversion, not a parallel memory store path. The downside
is that ingestion latency picks up the mdwatcher's tick (default 5 minutes)
on top of this watcher's hourly cadence; for meeting notes that's fine.

---

## 3. Hot-Reload via Loop-and-Recheck Run Loop

*Added: 2026-06-17*

**Decision:** `Run` does not early-return when `cfg.GWS.Enabled` is false.
Instead, it loops forever, checking `Enabled` on each iteration. When
false, it sleeps 60 seconds and re-checks. When true, it scans then sleeps
for `IntervalH` hours.

**Rationale:** The daemon's config hot-reload (cmd/lth/watch.go:configReloadLoop)
reloads `globalCfg` in place every 60 seconds. If the watcher captured
`Enabled` once at startup and returned on false, enabling it mid-run would
require a daemon restart -- defeating the point of hot-reload. With the
loop-and-recheck pattern, the watcher is always running as a cheap idle
goroutine and self-activates on the next poll after the flip.

**Consequence:** The daemon spawns this watcher unconditionally at startup.
The `gws` binary lookup also moves into the (deferred) first-scan path so
an install-gws-then-flip-enabled sequence works without daemon restart.

---

## 4. Doc-ID-Embedded Filenames for Stable Dedup

*Added: 2026-06-17*

**Decision:** Output filenames embed the Google doc ID
(`<date>_<slug>__<docID>.md`). The slug exists only for human readability;
the doc ID is the dedup key.

**Rationale:** Slugs derived from doc titles are unstable -- a meeting
organizer renaming "Q3 Planning" to "Q3 Planning v2" would otherwise create
a duplicate file. Using the Google doc ID as the canonical key means one
doc maps to exactly one file on disk forever. The mtime sync with Drive's
`modifiedTime` lets us skip re-fetching unchanged docs without maintaining
a separate state file.

**Consequence:** If the user manually renames or deletes a file in
`OutputDir`, the next cycle will re-create it. That's the right behaviour:
the directory is treated as a write-only cache owned by this watcher, not
as user-editable content.
