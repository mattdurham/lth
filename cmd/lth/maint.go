// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/spf13/cobra"
)

// maint is a small group of database maintenance commands that operate directly
// on the SQLite file. They are safe to run while the daemon is up, but VACUUM
// briefly acquires an exclusive lock and requires roughly 2x the database size
// in free disk space.
var maintCmd = &cobra.Command{
	Use:   "maint",
	Short: "Database maintenance commands (vacuum, wal checkpoint)",
}

var maintVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Rebuild the SQLite database to reclaim free pages",
	Long: `Run VACUUM on the lth SQLite database.

VACUUM rebuilds the database file from scratch, reclaiming all free pages left
behind by deletions, migrations, or NULL-ed BLOB columns. It is the only way to
actually shrink the on-disk size of memory.db after a large cleanup.

Cost: acquires an exclusive lock for the duration (seconds to minutes for a
multi-hundred-MB DB) and uses roughly 2x the current DB size as transient disk
space while it runs. Safe to run with the daemon up.`,
	RunE: runMaintVacuum,
}

var maintCheckpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Truncate the SQLite WAL file (flushes pending pages, then shrinks .db-wal to 0 bytes)",
	Long: `Run PRAGMA wal_checkpoint(TRUNCATE) on the lth SQLite database.

This copies all pending Write-Ahead Log pages into the main .db file and then
truncates the .db-wal sidecar to zero bytes on disk. The daemon already runs
this every 5 minutes, but you can invoke it manually if the WAL has grown large
between checkpoints.`,
	RunE: runMaintCheckpoint,
}

func init() {
	maintCmd.AddCommand(maintVacuumCmd, maintCheckpointCmd)
	rootCmd.AddCommand(maintCmd)
}

func runMaintVacuum(cmd *cobra.Command, _ []string) error {
	d, err := openDBForMaint()
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck

	before, after, err := d.Vacuum(cmd.Context())
	if err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	saved := before - after
	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"before_bytes": before,
			"after_bytes":  after,
			"saved_bytes":  saved,
		})
	}
	fmt.Printf("VACUUM complete: %s → %s (saved %s)\n",
		humanBytes(before), humanBytes(after), humanBytes(saved))
	return nil
}

func runMaintCheckpoint(cmd *cobra.Command, _ []string) error {
	d, err := openDBForMaint()
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck

	walPages, checkpointed, err := d.WALCheckpointTruncate(cmd.Context())
	if err != nil {
		// busy checkpoint returns the partial result alongside the error
		fmt.Fprintf(os.Stderr, "wal pages=%d checkpointed=%d: %v\n", walPages, checkpointed, err)
		return err
	}
	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"wal_pages":    walPages,
			"checkpointed": checkpointed,
		})
	}
	fmt.Printf("Checkpoint complete: %d WAL pages flushed, .db-wal truncated\n", checkpointed)
	return nil
}

// openDBForMaint opens the SQLite file directly (bypassing the daemon) so maint
// commands can run while the daemon is up. The maint commands hold their own
// exclusive lock only for the duration of the operation.
func openDBForMaint() (*db.DB, error) {
	d, err := db.Open(globalCfg.DB.Path, config.EmbeddingDim)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return d, nil
}

// humanBytes formats a byte count in human-readable form (KiB, MiB, GiB).
func humanBytes(n int64) string {
	const (
		_  = iota
		kb = 1 << (10 * iota)
		mb
		gb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// runWithContext is a helper for cobra RunE funcs that need ctx but don't take cmd args.
// (Unused placeholder reserved for future maint subcommands.)
var _ = func() context.Context { return context.Background() }
