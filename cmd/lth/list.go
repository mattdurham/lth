// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattdurham/lth/internal/config"
	"github.com/mattdurham/lth/internal/db"
	"github.com/spf13/cobra"
)

var (
	listLayer int
	listTop   int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all memories in a layer",
	Long:  "List all active memories in a layer ordered by creation time. Unlike search, no query is required.",
	RunE:  runList,
}

func init() {
	listCmd.Flags().IntVar(&listLayer, "layer", 0, "layer to list (1-5, required)")
	listCmd.Flags().IntVar(&listTop, "top", 0, "limit results (0 = all)")
	_ = listCmd.MarkFlagRequired("layer")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, _ []string) error {
	if listLayer < 1 || listLayer > 5 {
		return fmt.Errorf("--layer must be 1-5, got %d", listLayer)
	}

	cfgPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.Default()
	}

	d, err := db.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer d.Close() //nolint:errcheck

	rows, err := d.ListLayer(cmd.Context(), listLayer, true)
	if err != nil {
		return fmt.Errorf("list layer: %w", err)
	}

	if listTop > 0 && len(rows) > listTop {
		rows = rows[len(rows)-listTop:] // most recent N
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(rows)
	}

	if len(rows) == 0 {
		fmt.Printf("no memories in L%d\n", listLayer)
		return nil
	}

	for _, r := range rows {
		fmt.Printf("> id: %s  created: %s\n\n%s\n\n---\n\n",
			r.ID, r.CreatedAt.Format("2006-01-02 15:04:05"), r.Content)
	}
	return nil
}
