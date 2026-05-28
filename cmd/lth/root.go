// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mattdurham/lth/internal/config"
	"github.com/spf13/cobra"
)

var (
	flagDB      string
	flagConfig  string
	flagVerbose bool
	flagJSON    bool
	globalCfg   *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "lth",
	Short: "lth — hierarchical agentic memory system",
	Long:  "lth stores, searches, and compacts memories for AI agents across 5 hierarchy layers.",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// Set log level.
		level := slog.LevelInfo
		if flagVerbose {
			level = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

		// Load config.
		cfgPath := flagConfig
		if cfgPath == "" {
			var err error
			cfgPath, err = config.ConfigPath()
			if err != nil {
				return fmt.Errorf("config path: %w", err)
			}
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			// Fall back to defaults if config doesn't exist.
			cfg = config.Default()
		}

		// Override DB path if flag provided.
		if flagDB != "" {
			cfg.DB.Path = flagDB
		}

		globalCfg = cfg

		// Auto-start daemon unless this is a watch or config command.
		if !isDaemonExempt(cmd) {
			if err := ensureDaemon(cfg); err != nil {
				// Non-fatal: log warning but continue (daemon may already be running).
				slog.Warn("could not ensure daemon is running", "err", err)
			}
		}

		return nil
	},
}

// isDaemonExempt returns true for commands that should not auto-start the daemon.
// "compact" is exempt because it opens its own DB connection and runs compaction
// directly — starting the daemon would cause double compaction on the same DB.
func isDaemonExempt(cmd *cobra.Command) bool {
	path := cmd.CommandPath()
	words := strings.Fields(path)
	for _, w := range words {
		if w == "watch" || w == "config" || w == "compact" || w == "export" || w == "import" || w == "sync" {
			return true
		}
	}
	return false
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDB, "db", "", "path to memory database (default: ~/.lth/memory.db)")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config file (default: ~/.lth/config.toml)")
	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "enable verbose logging")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "output as JSON")
	rootCmd.PersistentFlags().Int("metrics-port", 10010, "port for Prometheus metrics server (daemon only)")
}
