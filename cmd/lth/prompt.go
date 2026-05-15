// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	promptTopEach    int
	promptCWD        bool
	promptFollowEdges bool
)

var promptCmd = &cobra.Command{
	Use:   "prompt <query>",
	Short: "Generate a structured agent context block from memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runPrompt,
}

func init() {
	promptCmd.Flags().IntVar(&promptTopEach, "top-each", 5, "results per layer group")
	promptCmd.Flags().BoolVar(&promptCWD, "cwd", false, "filter L4 results to memories where cwd matches current working directory")
	promptCmd.Flags().BoolVar(&promptFollowEdges, "follow-edges", false, "follow graph edges from L4 results into connected L5 nodes")
	rootCmd.AddCommand(promptCmd)
}

func runPrompt(cmd *cobra.Command, args []string) error {
	query := args[0]

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	// L1+L2: Role & Principles
	principles, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{1, 2},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L1/L2: %w", err)
	}

	// L3: Relevant Techniques
	techniques, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{3},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L3: %w", err)
	}

	// L4 only: Current Project Context (L5 excluded — too ephemeral, too noisy)
	context, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:  query,
		Layers: []int{4},
		TopK:   promptTopEach,
	})
	if err != nil {
		return fmt.Errorf("search L4: %w", err)
	}

	// Optionally filter L4 results by cwd attribute.
	if promptCWD {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}
		filtered := make([]*lth.SearchResult, 0, len(context))
		for _, r := range context {
			if r.Attrs["cwd"] == cwd {
				filtered = append(filtered, r)
			}
		}
		context = filtered
	}

	// Optionally follow graph edges from L4 results into connected L5 nodes.
	var episodes []*lth.Memory
	if promptFollowEdges && len(context) > 0 {
		seen := make(map[string]bool)
		for _, r := range context {
			edges, err := client.GraphNeighbors(cmd.Context(), r.ID, 1)
			if err != nil {
				continue
			}
			for _, e := range edges {
				neighborID := e.ToID
				if e.FromID != r.ID {
					neighborID = e.FromID
				}
				if seen[neighborID] {
					continue
				}
				seen[neighborID] = true
				mem, err := client.Get(cmd.Context(), neighborID)
				if err != nil || mem.Layer != 5 {
					continue
				}
				episodes = append(episodes, mem)
			}
		}
	}

	formatPromptOutput(os.Stdout, principles, techniques, context, episodes)
	return nil
}

// formatPromptOutput writes a structured markdown agent context block to w.
// Empty sections are omitted entirely. Each entry includes its memory ID so
// agents can explore further with: lth get <id>, lth graph show --from <id>
func formatPromptOutput(w io.Writer, principles, techniques, context []*lth.SearchResult, episodes []*lth.Memory) {
	fmt.Fprintln(w, "# Agent Context") //nolint:errcheck

	if len(principles) > 0 {
		fmt.Fprintln(w, "\n## Role & Principles") //nolint:errcheck
		for _, r := range principles {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", r.Content, r.ID) //nolint:errcheck
		}
	}

	if len(techniques) > 0 {
		fmt.Fprintln(w, "\n## Relevant Techniques") //nolint:errcheck
		for _, r := range techniques {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", r.Content, r.ID) //nolint:errcheck
		}
	}

	if len(context) > 0 {
		fmt.Fprintln(w, "\n## Current Project Context") //nolint:errcheck
		for _, r := range context {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", r.Content, r.ID) //nolint:errcheck
		}
	}

	if len(episodes) > 0 {
		fmt.Fprintln(w, "\n## Related Episodes") //nolint:errcheck
		for _, m := range episodes {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", m.Content, m.ID) //nolint:errcheck
		}
	}

	// Print a reference block so agents know how to explore further.
	allIDs := collectIDs(principles, techniques, context, episodes)
	if len(allIDs) > 0 {
		fmt.Fprintln(w, "\n## Memory IDs (for exploration)") //nolint:errcheck
		fmt.Fprintln(w, "Use these IDs to explore further:")                                               //nolint:errcheck
		fmt.Fprintln(w, "  lth get <id>                    — read full memory")                            //nolint:errcheck
		fmt.Fprintln(w, "  lth graph show --from <id>      — traverse graph edges")                        //nolint:errcheck
		fmt.Fprintln(w, "  lth graph ppr --seeds <id,...>  — personalized pagerank from seeds")            //nolint:errcheck
		fmt.Fprintln(w, "") //nolint:errcheck
		for _, id := range allIDs {
			fmt.Fprintf(w, "  %s\n", id) //nolint:errcheck
		}
	}
}

// collectIDs gathers all memory IDs from search results and raw memories, deduped.
func collectIDs(principles, techniques, context []*lth.SearchResult, episodes []*lth.Memory) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, r := range principles {
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	for _, r := range techniques {
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	for _, r := range context {
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	for _, m := range episodes {
		if !seen[m.ID] {
			seen[m.ID] = true
			ids = append(ids, m.ID)
		}
	}
	return ids
}
