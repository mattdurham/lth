// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/mattdurham/lth/pkg/lth"
)

var (
	searchLayers string
	searchTop    int
	searchAlpha  float32
	searchBeta   float32
	searchGamma  float32
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memories",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().StringVar(&searchLayers, "layers", "", "comma-separated layers to search (e.g. L1,L3)")
	searchCmd.Flags().IntVar(&searchTop, "top", 0, "number of results to return (default: config default)")
	searchCmd.Flags().Float32Var(&searchAlpha, "alpha", 0, "time decay weight")
	searchCmd.Flags().Float32Var(&searchBeta, "beta", 0, "importance weight")
	searchCmd.Flags().Float32Var(&searchGamma, "gamma", 0, "cosine similarity weight")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	layers, err := parseLayers(searchLayers)
	if err != nil {
		return err
	}

	client, err := lth.NewClient(globalCfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	defer client.Close() //nolint:errcheck

	req := &lth.SearchRequest{
		Query:  query,
		Layers: layers,
		TopK:   searchTop,
		Alpha:  searchAlpha,
		Beta:   searchBeta,
		Gamma:  searchGamma,
	}
	results, err := client.Search(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	formatSearchTable(os.Stdout, results)
	return nil
}

// parseLayers converts "L1,L3" to [1,3].
func parseLayers(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	layers := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(p, "L"), "l"))
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid layer %q: %w", p, err)
		}
		if n < 1 || n > 5 {
			return nil, fmt.Errorf("layer %d out of range 1-5", n)
		}
		layers = append(layers, n)
	}
	return layers, nil
}

// formatSearchTable prints a human-readable table of search results.
func formatSearchTable(w io.Writer, results []*lth.SearchResult) {
	if len(results) == 0 {
		fmt.Fprintln(w, "no results") //nolint:errcheck
		return
	}
	fmt.Fprintf(w, "%-10s %-4s %-6s %s\n", "ID", "L", "Score", "Content")         //nolint:errcheck
	fmt.Fprintf(w, "%-10s %-4s %-6s %s\n", "----------", "----", "------", "-------") //nolint:errcheck
	for _, r := range results {
		shortID := r.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		content := r.Content
		if len(content) > 80 {
			content = content[:77] + "..."
		}
		fmt.Fprintf(w, "%-10s %-4d %-6.3f %s\n", shortID, r.Layer, r.Score, content) //nolint:errcheck
	}
}
