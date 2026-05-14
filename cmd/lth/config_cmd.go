// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/mattdurham/lth/internal/config"
)

var configForceInit bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage lth configuration",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration",
	RunE:  runConfigInit,
}

func init() {
	configInitCmd.Flags().BoolVar(&configForceInit, "force", false, "overwrite existing config")
	configCmd.AddCommand(configInitCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigInit(_ *cobra.Command, _ []string) error {
	path := flagConfig
	if path == "" {
		var err error
		path, err = config.ConfigPath()
		if err != nil {
			return fmt.Errorf("config path: %w", err)
		}
	}

	if err := config.InitDefault(path, configForceInit); err != nil {
		return err
	}
	fmt.Printf("config written to %s\n", path)
	return nil
}
