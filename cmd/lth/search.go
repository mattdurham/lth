// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/mattdurham/lth/internal/llm"
	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	searchLayers     string
	searchTop        int
	searchAlpha      float32
	searchBeta       float32
	searchGamma      float32
	searchTags       string
	searchMinValence float32
	searchMaxValence float32
	searchExpand     bool
	searchAttrs      []string
	searchBrief      bool
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
	searchCmd.Flags().StringVar(&searchTags, "tags", "", "comma-separated tags to filter by (e.g. go,error-handling)")
	searchCmd.Flags().Float32Var(&searchMinValence, "min-valence", 0, "only return memories with valence >= this value (e.g. 0.5 for positive outcomes)")
	searchCmd.Flags().Float32Var(&searchMaxValence, "max-valence", 0, "only return memories with valence <= this value (e.g. -0.5 for failures)")
	searchCmd.Flags().BoolVar(&searchExpand, "expand", false, "expand query via LLM to find related memories")
	searchCmd.Flags().StringArrayVar(&searchAttrs, "attr", nil, "boost memories matching attribute key=value (repeatable, e.g. --attr file=/path/to/foo.go --attr project=github.com/org/repo)")
	searchCmd.Flags().BoolVar(&searchBrief, "brief", false, "return compact output: 3-sentence summary + fetch command instead of full content")
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
		Query:       query,
		Layers:      layers,
		TopK:        searchTop,
		Alpha:       searchAlpha,
		Beta:        searchBeta,
		Gamma:       searchGamma,
		Expand:      searchExpand,
		FilterAttrs: parseAttrs(searchAttrs),
	}

	// Set valence filters only when the flags were explicitly provided.
	if cmd.Flags().Changed("min-valence") {
		v := searchMinValence
		req.MinValence = &v
	}
	if cmd.Flags().Changed("max-valence") {
		v := searchMaxValence
		req.MaxValence = &v
	}

	results, err := client.Search(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// Filter by tags if --tags was specified.
	if searchTags != "" {
		required := strings.Split(searchTags, ",")
		filtered := make([]*lth.SearchResult, 0, len(results))
		for _, r := range results {
			memTags := strings.Split(r.Attrs["tags"], ",")
			if containsAllTags(memTags, required) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if searchBrief {
		return encodeBriefResults(cmd, results)
	}

	if flagJSON {
		return json.NewEncoder(os.Stdout).Encode(results)
	}
	formatSearchTable(os.Stdout, results)
	return nil
}

// BriefResult is the compact output shape for --brief mode.
type BriefResult struct {
	ID      string  `json:"id"`
	Layer   int     `json:"layer"`
	Score   float32 `json:"score"`
	Summary string  `json:"summary"`
	Fetch   string  `json:"fetch"`
}

// encodeBriefResults summarizes each result with Haiku and writes compact JSON.
func encodeBriefResults(cmd *cobra.Command, results []*lth.SearchResult) error {
	haiku := llm.New(globalCfg)

	brief := make([]BriefResult, len(results))
	var wg sync.WaitGroup
	for i, r := range results {
		wg.Add(1)
		go func(i int, r *lth.SearchResult) {
			defer wg.Done()
			summary := summarizeContent(cmd, haiku, r.Content)
			brief[i] = BriefResult{
				ID:      r.ID,
				Layer:   r.Layer,
				Score:   r.Score,
				Summary: summary,
				Fetch:   fmt.Sprintf("lth --json get %s", r.ID),
			}
		}(i, r)
	}
	wg.Wait()

	return json.NewEncoder(os.Stdout).Encode(brief)
}

// summarizeContent calls Haiku to produce a 3-sentence summary.
// Falls back to truncating the content if the LLM call fails.
func summarizeContent(cmd *cobra.Command, l llm.LLM, content string) string {
	prompt := "Summarize the following engineering memory in exactly 3 sentences. Be concise and technical. Return only the summary text, no preamble or labels:\n\n" + content
	summary, err := l.Complete(cmd.Context(), prompt)
	if err != nil || strings.TrimSpace(summary) == "" {
		if len(content) > 300 {
			return content[:297] + "..."
		}
		return content
	}
	return strings.TrimSpace(summary)
}

// containsAllTags returns true if every needle is found in haystack (exact match).
func containsAllTags(haystack, needles []string) bool {
	for _, needle := range needles {
		found := false
		for _, h := range haystack {
			if h == needle {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
	fmt.Fprintf(w, "%-10s %-4s %-6s %-7s %s\n", "ID", "L", "Score", "Valence", "Content")             //nolint:errcheck
	fmt.Fprintf(w, "%-10s %-4s %-6s %-7s %s\n", "----------", "----", "------", "-------", "-------") //nolint:errcheck
	for _, r := range results {
		shortID := r.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		content := r.Content
		if len(content) > 72 {
			content = content[:69] + "..."
		}
		fmt.Fprintf(w, "%-10s %-4d %-6.3f %-7s %s\n", shortID, r.Layer, r.Score, formatValence(r.Valence), content) //nolint:errcheck
	}
}

// formatValence returns a human-readable valence indicator.
// Values > 0.3 are shown as "+N.NN", values < -0.3 as "-N.NN", near-zero as "  0.00".
func formatValence(v float32) string {
	if v > 0.3 {
		return fmt.Sprintf("+%.2f", v)
	}
	if v < -0.3 {
		return fmt.Sprintf("%.2f", v)
	}
	return "  0.00"
}
