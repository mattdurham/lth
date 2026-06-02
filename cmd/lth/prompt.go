// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	promptTopEach     int
	promptCWD         bool
	promptFollowEdges bool
	promptPPR         bool
	promptPPRTop      int
	promptExpand      bool
	promptFilterAttrs []string // key=value pairs for attribute boosting
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
	promptCmd.Flags().BoolVar(&promptPPR, "ppr", true, "expand context via Personalized PageRank from search result seeds")
	promptCmd.Flags().IntVar(&promptPPRTop, "ppr-top", 5, "number of PPR-expanded memories to include")
	promptCmd.Flags().BoolVar(&promptExpand, "expand", true, "expand queries via LLM for broader context retrieval (default true)")
	promptCmd.Flags().StringArrayVar(&promptFilterAttrs, "attr", nil, "boost memories matching attribute key=value (repeatable, e.g. --attr project=tempo)")
	rootCmd.AddCommand(promptCmd)
}

func runPrompt(cmd *cobra.Command, args []string) error {
	query := args[0]

	client, err := newClientFromGlobalCfg()
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	filterAttrs := parseAttrs(promptFilterAttrs)

	// L1+L2: Role & Principles
	principles, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:       query,
		Layers:      []int{1, 2},
		TopK:        promptTopEach,
		Expand:      promptExpand,
		FilterAttrs: filterAttrs,
	})
	if err != nil {
		return fmt.Errorf("search L1/L2: %w", err)
	}

	// L3: Relevant Techniques
	techniques, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:       query,
		Layers:      []int{3},
		TopK:        promptTopEach,
		Expand:      promptExpand,
		FilterAttrs: filterAttrs,
	})
	if err != nil {
		return fmt.Errorf("search L3: %w", err)
	}

	// L4 only: Current Project Context (L5 excluded — too ephemeral, too noisy)
	context, err := client.Search(cmd.Context(), &lth.SearchRequest{
		Query:       query,
		Layers:      []int{4},
		TopK:        promptTopEach,
		Expand:      promptExpand,
		FilterAttrs: filterAttrs,
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

	// PPR expansion: seed from all search results, surface graph-linked memories not yet seen.
	// Seed attributes are collected to apply affinity re-ranking — memories sharing attrs
	// with the seeds score higher, reducing cross-project contamination.
	var related []*lth.Memory
	if promptPPR {
		seeds := collectIDs(principles, techniques, context, nil)
		if len(seeds) > 0 {
			pprScores, err := client.GraphPPR(cmd.Context(), seeds)
			if err == nil && len(pprScores) > 0 {
				// Collect attribute fingerprint from seed memories for affinity scoring.
				seedAttrs := collectSeedAttrs(principles, techniques, context)

				type scored struct {
					id    string
					score float64
				}
				ranked := make([]scored, 0, len(pprScores))
				for id, score := range pprScores {
					ranked = append(ranked, scored{id, score})
				}

				// Fetch candidates and apply affinity re-ranking before selecting top-N.
				seen := make(map[string]bool, len(seeds))
				for _, id := range seeds {
					seen[id] = true
				}

				// Pre-fetch a larger pool then re-rank by attr affinity.
				pool := promptPPRTop * 4
				var candidates []struct {
					mem   *lth.Memory
					score float64
				}
				sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
				for _, s := range ranked {
					if len(candidates) >= pool {
						break
					}
					if seen[s.id] {
						continue
					}
					seen[s.id] = true
					mem, err := client.Get(cmd.Context(), s.id)
					if err != nil {
						continue
					}
					score := s.score
					// Boost memories whose attributes overlap with seed attrs.
					if attrAffinityScore(mem.Attrs, seedAttrs) > 0 {
						score *= 1.5
					}
					// Also apply explicit FilterAttrs boost.
					if len(filterAttrs) > 0 && attrSubsetMatch(mem.Attrs, filterAttrs) {
						score *= 1.5
					}
					candidates = append(candidates, struct {
						mem   *lth.Memory
						score float64
					}{mem, score})
				}

				sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
				for _, c := range candidates {
					if len(related) >= promptPPRTop {
						break
					}
					related = append(related, c.mem)
				}
			}
		}
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

	formatPromptOutput(os.Stdout, principles, techniques, context, related, episodes)
	return nil
}

// formatPromptOutput writes a structured markdown agent context block to w.
// Empty sections are omitted entirely. Each entry includes its memory ID so
// agents can explore further with: lth get <id>, lth graph show --from <id>
func formatPromptOutput(w io.Writer, principles, techniques, context []*lth.SearchResult, related, episodes []*lth.Memory) {
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

	if len(related) > 0 {
		fmt.Fprintln(w, "\n## Related Context (via graph)") //nolint:errcheck
		for _, m := range related {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", m.Content, m.ID) //nolint:errcheck
		}
	}

	if len(episodes) > 0 {
		fmt.Fprintln(w, "\n## Related Episodes") //nolint:errcheck
		for _, m := range episodes {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", m.Content, m.ID) //nolint:errcheck
		}
	}

	// Print a reference block so agents know how to explore further.
	allIDs := collectIDs(principles, techniques, context, append(related, episodes...))
	if len(allIDs) > 0 {
		fmt.Fprintln(w, "\n## Memory IDs (for exploration)")                                    //nolint:errcheck
		fmt.Fprintln(w, "Use these IDs to explore further:")                                    //nolint:errcheck
		fmt.Fprintln(w, "  lth get <id>                    — read full memory")                 //nolint:errcheck
		fmt.Fprintln(w, "  lth graph show --from <id>      — traverse graph edges")             //nolint:errcheck
		fmt.Fprintln(w, "  lth graph ppr --seeds <id,...>  — personalized pagerank from seeds") //nolint:errcheck
		fmt.Fprintln(w, "")                                                                     //nolint:errcheck
		for _, id := range allIDs {
			fmt.Fprintf(w, "  %s\n", id) //nolint:errcheck
		}
	}

	// Project filter hints: collect distinct projects across all results and suggest --attr filter.
	projects := collectResultProjects(principles, techniques, context, related, episodes)
	if len(projects) > 0 {
		fmt.Fprintln(w, "\n## Filter by project")                   //nolint:errcheck
		fmt.Fprintln(w, "Memories from these projects are present:") //nolint:errcheck
		for _, p := range projects {
			fmt.Fprintf(w, "  lth prompt \"...\" --attr project=%s\n", p) //nolint:errcheck
		}
		fmt.Fprintln(w, "  lth projects  — list all tracked projects")        //nolint:errcheck
		fmt.Fprintln(w, "  lth chat \"...\" --attr project=<project> — filtered chat") //nolint:errcheck
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

// collectResultProjects returns distinct project attribute values across all result sets, sorted.
func collectResultProjects(principles, techniques, context []*lth.SearchResult, related, episodes []*lth.Memory) []string {
	seen := map[string]struct{}{}
	for _, group := range [][]*lth.SearchResult{principles, techniques, context} {
		for _, r := range group {
			if p := r.Attrs["project"]; p != "" {
				seen[p] = struct{}{}
			}
		}
	}
	for _, group := range [][]*lth.Memory{related, episodes} {
		for _, m := range group {
			if p := m.Attrs["project"]; p != "" {
				seen[p] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// collectSeedAttrs aggregates attribute key=value counts across all seed memories.
// Keys with consistent values across seeds form the "affinity fingerprint".
func collectSeedAttrs(groups ...[]*lth.SearchResult) map[string]map[string]int {
	counts := map[string]map[string]int{}
	for _, group := range groups {
		for _, r := range group {
			for k, v := range r.Attrs {
				if counts[k] == nil {
					counts[k] = map[string]int{}
				}
				counts[k][v]++
			}
		}
	}
	return counts
}

// attrAffinityScore returns >0 if mem shares any high-frequency attribute values with seeds.
func attrAffinityScore(memAttrs map[string]string, seedCounts map[string]map[string]int) float64 {
	var score float64
	for k, v := range memAttrs {
		if vCounts, ok := seedCounts[k]; ok {
			if n := vCounts[v]; n > 0 {
				score += float64(n)
			}
		}
	}
	return score
}

// attrSubsetMatch returns true if memAttrs contains all key=value pairs in filter.
func attrSubsetMatch(memAttrs, filter map[string]string) bool {
	for k, v := range filter {
		if memAttrs[k] != v {
			return false
		}
	}
	return true
}
