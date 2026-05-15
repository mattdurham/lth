// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show memory store statistics",
	RunE:  runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, _ []string) error {
	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	stats, err := client.Stats(cmd.Context())
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(stats)
	}

	fmt.Printf("Total memories: %d\n", stats.TotalMemories)
	fmt.Printf("Total edges:    %d\n", stats.TotalEdges)
	fmt.Println()
	fmt.Printf("%-8s %s\n", "Layer", "Count")
	fmt.Printf("%-8s %s\n", "--------", "-----")
	for layer := 1; layer <= 5; layer++ {
		count := stats.ByLayer[layer]
		fmt.Printf("L%-7d %d\n", layer, count)
	}
	return nil
}
