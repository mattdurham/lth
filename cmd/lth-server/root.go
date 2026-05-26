// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	flagConfig string
	flagPort   int
)

var rootCmd = &cobra.Command{
	Use:   "lth-server",
	Short: "lth-server: HTTP sync server for lth memories",
	RunE:  runServer,
}

// Execute runs the root command. Called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "lth-server.yaml", "path to YAML config file")
	rootCmd.PersistentFlags().IntVar(&flagPort, "port", 0, "port to listen on (overrides config)")
}

func runServer(cmd *cobra.Command, _ []string) error {
	cfg, err := loadServerConfig(flagConfig)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("load config: %w", err)
	}
	if flagPort != 0 {
		cfg.Port = flagPort
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv, err := newServer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	return srv.Start(ctx)
}
