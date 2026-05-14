// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	graphFromID    string
	graphDepth     int
	graphPPRSeeds  string
	graphPPRTopN   int
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Explore the memory graph",
}

var graphShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show graph edges from a memory ID",
	RunE:  runGraphShow,
}

var graphPPRCmd = &cobra.Command{
	Use:   "ppr",
	Short: "Run Personalized PageRank from seed memory IDs",
	RunE:  runGraphPPR,
}

func init() {
	graphShowCmd.Flags().StringVar(&graphFromID, "from", "", "source memory ID (required)")
	graphShowCmd.Flags().IntVar(&graphDepth, "depth", 3, "BFS depth")
	_ = graphShowCmd.MarkFlagRequired("from")

	graphPPRCmd.Flags().StringVar(&graphPPRSeeds, "seeds", "", "comma-separated seed memory IDs (required)")
	graphPPRCmd.Flags().IntVar(&graphPPRTopN, "top", 10, "number of top-scored nodes to show")
	_ = graphPPRCmd.MarkFlagRequired("seeds")

	graphCmd.AddCommand(graphShowCmd, graphPPRCmd)
	rootCmd.AddCommand(graphCmd)
}

func runGraphShow(cmd *cobra.Command, _ []string) error {
	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	edges, err := client.GraphNeighbors(cmd.Context(), graphFromID, graphDepth)
	if err != nil {
		return fmt.Errorf("graph neighbors: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(edges)
	}

	if len(edges) == 0 {
		fmt.Printf("%s (no connections)\n", graphFromID)
		return nil
	}

	fmt.Printf("%s\n", graphFromID)
	for _, e := range edges {
		fmt.Printf("  -[%s w=%.2f]-> %s\n", e.EdgeType, e.Weight, e.ToID)
	}
	return nil
}

func runGraphPPR(cmd *cobra.Command, _ []string) error {
	seeds := strings.Split(graphPPRSeeds, ",")
	for i, s := range seeds {
		seeds[i] = strings.TrimSpace(s)
	}

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	scores, err := client.GraphPPR(cmd.Context(), seeds)
	if err != nil {
		return fmt.Errorf("ppr: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(scores)
	}

	// Print top N results.
	type kv struct {
		k string
		v float64
	}
	var sorted []kv
	for k, v := range scores {
		sorted = append(sorted, kv{k, v})
	}
	// Simple insertion sort for small N.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].v > sorted[j-1].v; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	limit := graphPPRTopN
	if limit > len(sorted) {
		limit = len(sorted)
	}
	for _, kv := range sorted[:limit] {
		shortID := kv.k
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("%-10s %.4f\n", shortID, kv.v)
	}
	return nil
}
