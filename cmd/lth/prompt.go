// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mattdurham/lth/pkg/lth"
	"github.com/spf13/cobra"
)

var (
	promptTopEach      int
	promptCWD          bool
	promptFollowEdges  bool
	promptPPR          bool
	promptPPRTop       int
	promptExpand       bool
	promptFilterAttrs  []string // key=value pairs for attribute boosting
	promptDomain       string   // hard-filter L1/L2 by domain attr; universal rules (no domain) always included
	promptIntentFlag   string   // auto|research|build|review — controls per-layer topK and multi-pass output
	promptProjectBoost float64 // extra score multiplier for results matching --attr project=X (on top of search-layer boost)
	promptFull         bool    // show full memory content instead of the default condensed single-paragraph view
)

// intentConfig holds per-layer topK overrides and the section label for one retrieval pass.
type intentConfig struct {
	principlesTopK int
	techniquesTopK int
	contextTopK    int
	label          string
}

var intentConfigs = map[string]intentConfig{
	"research": {principlesTopK: 2, techniquesTopK: 7, contextTopK: 5, label: "Research"},
	"build":    {principlesTopK: 5, techniquesTopK: 5, contextTopK: 5, label: "Implementation"},
	"review":   {principlesTopK: 4, techniquesTopK: 3, contextTopK: 7, label: "Review"},
}

// promptPass holds the results of one intent-specific retrieval.
type promptPass struct {
	label      string
	principles []*lth.SearchResult
	techniques []*lth.SearchResult
	context    []*lth.SearchResult
}

var promptCmd = &cobra.Command{
	Use:   "prompt <query>",
	Short: "Generate a structured agent context block from memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runPrompt,
}

func init() {
	promptCmd.Flags().IntVar(&promptTopEach, "top-each", 5, "results per layer group (used when --intent is not auto)")
	promptCmd.Flags().BoolVar(&promptCWD, "cwd", false, "filter L4 results to memories where cwd matches current working directory")
	promptCmd.Flags().BoolVar(&promptFollowEdges, "follow-edges", false, "follow graph edges from L4 results into connected L5 nodes")
	promptCmd.Flags().BoolVar(&promptPPR, "ppr", true, "expand context via Personalized PageRank from search result seeds")
	promptCmd.Flags().IntVar(&promptPPRTop, "ppr-top", 5, "number of PPR-expanded memories to include")
	promptCmd.Flags().BoolVar(&promptExpand, "expand", true, "expand queries via LLM for broader context retrieval (default true)")
	promptCmd.Flags().StringArrayVar(&promptFilterAttrs, "attr", nil, "boost memories matching attribute key=value (repeatable, e.g. --attr project=tempo)")
	promptCmd.Flags().StringVar(&promptDomain, "domain", "", "hard-filter L1/L2 behavioral rules by domain (e.g. coding, email); universal rules (no domain attr) always included")
	promptCmd.Flags().StringVar(&promptIntentFlag, "intent", "auto", "retrieval intent: auto|research|build|review — auto detects from query keywords; multiple detected intents produce separate sections")
	promptCmd.Flags().Float64Var(&promptProjectBoost, "project-boost", 3.0, "score multiplier applied to results matching --attr project=X; non-matching stay included but rank lower")
	promptCmd.Flags().BoolVar(&promptFull, "full", false, "show complete memory content instead of the default condensed single-paragraph view")
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
	filterProject := filterAttrs["project"] // hard-exclude memories tagged to a different project

	intents := resolveIntents(promptIntentFlag, query)

	var passes []promptPass
	for _, intent := range intents {
		cfg, ok := intentConfigs[intent]
		if !ok {
			cfg = intentConfigs["build"]
		}

		// L1+L2: Role & Principles — hard-filtered by domain when --domain is set.
		principles, err := client.Search(cmd.Context(), &lth.SearchRequest{
			Query:       query,
			Layers:      []int{1, 2},
			TopK:        cfg.principlesTopK,
			Expand:      promptExpand,
			FilterAttrs: filterAttrs,
		})
		if err != nil {
			return fmt.Errorf("search L1/L2: %w", err)
		}
		principles = filterByDomain(principles, promptDomain)

		// L3: Relevant Techniques
		techniques, err := client.Search(cmd.Context(), &lth.SearchRequest{
			Query:       query,
			Layers:      []int{3},
			TopK:        cfg.techniquesTopK,
			Expand:      promptExpand,
			FilterAttrs: filterAttrs,
		})
		if err != nil {
			return fmt.Errorf("search L3: %w", err)
		}
		techniques = boostByProject(techniques, filterProject, promptProjectBoost)

		// L4 only: Current Project Context (L5 excluded — too ephemeral, too noisy)
		context, err := client.Search(cmd.Context(), &lth.SearchRequest{
			Query:       query,
			Layers:      []int{4},
			TopK:        cfg.contextTopK,
			Expand:      promptExpand,
			FilterAttrs: filterAttrs,
		})
		if err != nil {
			return fmt.Errorf("search L4: %w", err)
		}

		context = boostByProject(context, filterProject, promptProjectBoost)

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

		passes = append(passes, promptPass{
			label:      cfg.label,
			principles: principles,
			techniques: techniques,
			context:    context,
		})
	}

	// PPR expansion: seed from all passes combined to avoid duplicate graph traversal.
	var related []*lth.Memory
	if promptPPR {
		var allPrinciples, allTechniques, allContext []*lth.SearchResult
		for _, p := range passes {
			allPrinciples = append(allPrinciples, p.principles...)
			allTechniques = append(allTechniques, p.techniques...)
			allContext = append(allContext, p.context...)
		}
		seeds := collectIDs(allPrinciples, allTechniques, allContext, nil)
		if len(seeds) > 0 {
			pprScores, err := client.GraphPPR(cmd.Context(), seeds)
			if err == nil && len(pprScores) > 0 {
				seedAttrs := collectSeedAttrs(allPrinciples, allTechniques, allContext)

				type scored struct {
					id    string
					score float64
				}
				ranked := make([]scored, 0, len(pprScores))
				for id, score := range pprScores {
					ranked = append(ranked, scored{id, score})
				}

				seen := make(map[string]bool, len(seeds))
				for _, id := range seeds {
					seen[id] = true
				}

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
					if attrAffinityScore(mem.Attrs, seedAttrs) > 0 {
						score *= 1.5
					}
					if len(filterAttrs) > 0 && attrSubsetMatch(mem.Attrs, filterAttrs) {
						score *= 1.5
					}
					// Extra boost for memories from the requested project (on top of the generic attr boost).
					if filterProject != "" && mem.Attrs["project"] == filterProject {
						score *= promptProjectBoost
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
	if promptFollowEdges {
		seen := make(map[string]bool)
		for _, pass := range passes {
			for _, r := range pass.context {
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
	}

	formatPromptOutput(os.Stdout, passes, related, episodes, promptFull)
	return nil
}

// resolveIntents returns the ordered list of intents to run. "auto" uses keyword detection on
// the query; explicit values are passed through. Multiple intents produce separate output sections.
func resolveIntents(flag, query string) []string {
	if flag != "auto" {
		if _, ok := intentConfigs[flag]; ok {
			return []string{flag}
		}
		return []string{"build"}
	}

	q := strings.ToLower(query)

	researchKW := []string{"how does", "what is", "why does", "explain", "understand", "research", "investigate", "explore", "describe", "overview", "tell me about", "what are"}
	buildKW := []string{"code", "implement", "build", "write", "create", "add", "fix", "develop", "modify", "want to code", "working on", "using the", "change", "update"}
	reviewKW := []string{"review", "evaluate", "check", "assess", "audit", "is this correct", "does this work", "should i", "is this right"}

	hasResearch := containsAny(q, researchKW)
	hasBuild := containsAny(q, buildKW)
	hasReview := containsAny(q, reviewKW)

	var intents []string
	if hasResearch {
		intents = append(intents, "research")
	}
	if hasBuild {
		intents = append(intents, "build")
	}
	if hasReview {
		intents = append(intents, "review")
	}
	if len(intents) == 0 {
		intents = []string{"build"}
	}
	return intents
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// boostByProject applies an extra score multiplier to results matching the given project,
// then re-sorts so they rank higher. Memories with no project attr or a different project
// are kept but score lower. No-ops when project is empty.
func boostByProject(results []*lth.SearchResult, project string, multiplier float64) []*lth.SearchResult {
	if project == "" || multiplier <= 1 {
		return results
	}
	for _, r := range results {
		if r.Attrs["project"] == project {
			r.Score = float32(float64(r.Score) * multiplier)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
}

const condensedMaxChars = 280

// condensedContent returns a condensed single-paragraph view of a memory.
// It skips leading markdown heading lines (# ...) and returns the first
// substantive paragraph, capped at condensedMaxChars characters.
func condensedContent(content string) string {
	// Split into paragraphs on blank lines.
	paragraphs := strings.Split(content, "\n\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Skip pure heading paragraphs (every line starts with #).
		lines := strings.Split(p, "\n")
		allHeadings := true
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), "#") {
				allHeadings = false
				break
			}
		}
		if allHeadings {
			continue
		}
		// Strip any leading heading lines within the paragraph, keep the rest.
		var kept []string
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), "#") {
				kept = append(kept, l)
			}
		}
		text := strings.TrimSpace(strings.Join(kept, " "))
		// Collapse multiple spaces left by markdown bold markers etc.
		for strings.Contains(text, "  ") {
			text = strings.ReplaceAll(text, "  ", " ")
		}
		if len(text) > condensedMaxChars {
			// Break at a word boundary.
			cut := strings.LastIndex(text[:condensedMaxChars], " ")
			if cut < 0 {
				cut = condensedMaxChars
			}
			return text[:cut] + "…"
		}
		return text
	}
	return strings.TrimSpace(content)
}

// filterByDomain hard-filters L1/L2 results by domain attribute. Rules with no domain attr are
// always included (universal). Rules with a mismatching domain attr are excluded.
func filterByDomain(results []*lth.SearchResult, domain string) []*lth.SearchResult {
	if domain == "" {
		return results
	}
	out := make([]*lth.SearchResult, 0, len(results))
	for _, r := range results {
		d, hasDomain := r.Attrs["domain"]
		if !hasDomain || d == domain {
			out = append(out, r)
		}
	}
	return out
}

// formatPromptOutput writes a structured markdown agent context block to w.
// Single-pass output uses flat headers (## Role & Principles). Multi-pass output uses
// nested headers (## Research / ### Role & Principles) with one section per intent.
// Empty sections are omitted. Each entry includes its memory ID for further exploration.
func formatPromptOutput(w io.Writer, passes []promptPass, related, episodes []*lth.Memory, full bool) {
	content := func(s string) string {
		if full {
			return s
		}
		return condensedContent(s)
	}

	fmt.Fprintln(w, "# Agent Context") //nolint:errcheck

	multiPass := len(passes) > 1

	for _, pass := range passes {
		if multiPass {
			fmt.Fprintf(w, "\n## %s\n", pass.label) //nolint:errcheck
		}

		h := func(title string) string {
			if multiPass {
				return "### " + title
			}
			return "## " + title
		}

		if len(pass.principles) > 0 {
			fmt.Fprintf(w, "\n%s\n", h("Role & Principles")) //nolint:errcheck
			for _, r := range pass.principles {
				fmt.Fprintf(w, "%s\n\n> id: %s\n\n", content(r.Content), r.ID) //nolint:errcheck
			}
		}

		if len(pass.techniques) > 0 {
			fmt.Fprintf(w, "\n%s\n", h("Relevant Techniques")) //nolint:errcheck
			for _, r := range pass.techniques {
				fmt.Fprintf(w, "%s\n\n> id: %s\n\n", content(r.Content), r.ID) //nolint:errcheck
			}
		}

		if len(pass.context) > 0 {
			fmt.Fprintf(w, "\n%s\n", h("Current Project Context")) //nolint:errcheck
			for _, r := range pass.context {
				fmt.Fprintf(w, "%s\n\n> id: %s\n\n", content(r.Content), r.ID) //nolint:errcheck
			}
		}
	}

	if len(related) > 0 {
		fmt.Fprintln(w, "\n## Related Context (via graph)") //nolint:errcheck
		for _, m := range related {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", content(m.Content), m.ID) //nolint:errcheck
		}
	}

	if len(episodes) > 0 {
		fmt.Fprintln(w, "\n## Related Episodes") //nolint:errcheck
		for _, m := range episodes {
			fmt.Fprintf(w, "%s\n\n> id: %s\n\n", content(m.Content), m.ID) //nolint:errcheck
		}
	}

	// Collect all IDs across passes for the exploration block.
	var allPrinciples, allTechniques, allContext []*lth.SearchResult
	for _, p := range passes {
		allPrinciples = append(allPrinciples, p.principles...)
		allTechniques = append(allTechniques, p.techniques...)
		allContext = append(allContext, p.context...)
	}

	allIDs := collectIDs(allPrinciples, allTechniques, allContext, append(related, episodes...))
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

	// Project filter hints.
	projects := collectResultProjects(allPrinciples, allTechniques, allContext, related, episodes)
	if len(projects) > 0 {
		fmt.Fprintln(w, "\n## Filter by project")                        //nolint:errcheck
		fmt.Fprintln(w, "Memories from these projects are present:")      //nolint:errcheck
		for _, p := range projects {
			fmt.Fprintf(w, "  lth prompt \"...\" --attr project=%s\n", p) //nolint:errcheck
		}
		fmt.Fprintln(w, "  lth projects  — list all tracked projects")               //nolint:errcheck
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
