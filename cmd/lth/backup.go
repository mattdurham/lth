// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattdurham/lth/internal/backupwatcher"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage database backup snapshots",
}

var backupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available backup snapshots",
	RunE:  runBackupList,
}

var backupRestoreCmd = &cobra.Command{
	Use:   "restore <file>",
	Short: "Restore the database from a backup snapshot",
	Long: "Stops the daemon if running, saves the current database to <path>.pre-restore, " +
		"decompresses the chosen snapshot into place, and leaves the daemon stopped so you " +
		"can inspect the result before resuming ingestion with `lth watch start`.",
	Args: cobra.ExactArgs(1),
	RunE: runBackupRestore,
}

func init() {
	backupCmd.AddCommand(backupListCmd, backupRestoreCmd)
	rootCmd.AddCommand(backupCmd)
}

func runBackupList(_ *cobra.Command, _ []string) error {
	dir := expandBackupDir()
	if dir == "" {
		fmt.Println("backup.dir is not configured; nothing to list")
		return nil
	}

	snaps, err := backupwatcher.ListSnapshots(dir)
	if err != nil {
		return fmt.Errorf("list snapshots: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(snaps)
	}

	if len(snaps) == 0 {
		fmt.Printf("no snapshots found in %s\n", dir)
		return nil
	}

	// Most recent first for display; ListSnapshots returns oldest first.
	for i := len(snaps) - 1; i >= 0; i-- {
		s := snaps[i]
		fmt.Printf("%s  %10d bytes  %s\n", s.Name, s.Size, s.Time.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func runBackupRestore(_ *cobra.Command, args []string) error {
	snapshotPath := args[0]
	if !filepath.IsAbs(snapshotPath) {
		if dir := expandBackupDir(); dir != "" {
			if candidate := filepath.Join(dir, snapshotPath); fileExists(candidate) {
				snapshotPath = candidate
			}
		}
	}
	if !fileExists(snapshotPath) {
		return fmt.Errorf("snapshot not found: %s", snapshotPath)
	}

	pidFile := pidFilePath(globalCfg)
	if pid, err := readPIDFile(pidFile); err == nil && isProcessAlive(pid) {
		fmt.Println("stopping daemon before restore...")
		if err := runWatchStop(nil, nil); err != nil {
			return fmt.Errorf("stop daemon: %w", err)
		}
	}

	preRestorePath, err := backupwatcher.Restore(globalCfg.DB.Path, snapshotPath)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	fmt.Printf("restored %s -> %s\n", snapshotPath, globalCfg.DB.Path)
	if preRestorePath != "" {
		fmt.Printf("previous database saved to %s\n", preRestorePath)
	}
	fmt.Println("daemon is stopped -- inspect the restored database, then run `lth watch start` when ready.")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func expandBackupDir() string {
	if globalCfg.Backup.Dir == "" {
		return ""
	}
	if !strings.HasPrefix(globalCfg.Backup.Dir, "~/") {
		return globalCfg.Backup.Dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return globalCfg.Backup.Dir
	}
	return filepath.Join(home, globalCfg.Backup.Dir[2:])
}
