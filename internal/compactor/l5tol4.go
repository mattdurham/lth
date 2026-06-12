// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/memory"
)

const windowSize = 20

// compactL5toL4 promotes L5 memories to L4 via LLM summarization.
// Triggers when CountByLayer(5) > L5Threshold OR oldest L5 > L5MaxAge hours.
//
// Memories with embeddings are clustered by pairwise cosine similarity
// (threshold: L5ClusterThreshold, min cluster size: L5MinClusterSize).
// Memories without embeddings fall back to chronological windowing when
// count >= windowSize.
func (c *Compactor) compactL5toL4(ctx context.Context) (int, error) {
	// Check trigger conditions.
	stats, err := c.store.Stats(ctx)
	if err != nil {
		return 0, fmt.Errorf("stats: %w", err)
	}

	l5Count := stats.ByLayer[5]
	if l5Count == 0 {
		return 0, nil
	}

	// Load L5 memories.
	l5Memories, err := c.store.ListLayer(ctx, 5)
	if err != nil {
		return 0, fmt.Errorf("list L5: %w", err)
	}
	if len(l5Memories) == 0 {
		return 0, nil
	}

	// Check trigger: count or age.
	threshold := c.cfg.Compaction.L5Threshold
	maxAgeH := c.cfg.Compaction.L5MaxAgeH
	now := time.Now().UTC()

	var triggered bool
	if l5Count > threshold {
		triggered = true
	}
	if !triggered {
		for _, m := range l5Memories {
			if now.Sub(m.CreatedAt).Hours() > float64(maxAgeH) {
				triggered = true
				break
			}
		}
	}
	if !triggered {
		return 0, nil
	}

	// Separate memories with and without embeddings.
	var withEmb, withoutEmb []*memory.Memory
	for _, m := range l5Memories {
		if len(m.Embedding) > 0 {
			withEmb = append(withEmb, m)
		} else {
			withoutEmb = append(withoutEmb, m)
		}
	}

	promoted := 0

	// Semantic clustering for memories with embeddings.
	clusterThreshold := c.cfg.Compaction.L5ClusterThreshold
	minSize := c.cfg.Compaction.L5MinClusterSize
	clusters := findL5Clusters(withEmb, clusterThreshold, minSize)
	for _, cluster := range clusters {
		n, err := c.summarizeCluster(ctx, cluster)
		if err != nil {
			c.logger.Warn("L5→L4 cluster summarization failed", "err", err)
			continue
		}
		promoted += n
	}

	// Collect unclustered memories with embeddings (not consumed by semantic clustering).
	usedIDs := make(map[string]bool)
	for _, cl := range clusters {
		for _, m := range cl {
			usedIDs[m.ID] = true
		}
	}
	var unclusteredWithEmb []*memory.Memory
	for _, m := range withEmb {
		if !usedIDs[m.ID] {
			unclusteredWithEmb = append(unclusteredWithEmb, m)
		}
	}

	// Build the full fallback set: memories without embeddings + unclustered memories with
	// embeddings. Fall back to chronological windowing when count >= windowSize.
	fallback := append(withoutEmb, unclusteredWithEmb...) //nolint:gocritic
	if len(fallback) >= windowSize {
		for i := 0; i < len(fallback); i += windowSize {
			end := i + windowSize
			if end > len(fallback) {
				end = len(fallback)
			}
			n, err := c.summarizeCluster(ctx, fallback[i:end])
			if err != nil {
				c.logger.Warn("L5→L4 fallback window failed", "err", err)
				continue
			}
			promoted += n
		}
	}

	return promoted, nil
}

// selectForPrompt returns the memories whose content should be included in the
// summarization prompt, downsampled to fit budgetChars. If budgetChars <= 0
// (config not set) or the total fits, the full cluster is returned unchanged.
//
// When sampling, evenly-spaced indices are chosen so the result still spans the
// time range of the cluster (assumes the input is in chronological order, as
// produced by ListLayer + the clustering pass). At least L5MinClusterSize
// memories are always retained, even if it nominally exceeds the budget; the
// LLM call itself surfaces real context-overflow errors via the chain.
func selectForPrompt(cluster []*memory.Memory, budgetChars int) (picked []*memory.Memory, totalChars int, sampled bool) {
	for _, m := range cluster {
		totalChars += len(m.Content)
	}
	if budgetChars <= 0 || totalChars <= budgetChars {
		return cluster, totalChars, false
	}
	// Number of memories to keep, proportional to budget vs total. Floor at 2.
	n := budgetChars * len(cluster) / totalChars
	if n < 2 {
		n = 2
	}
	if n >= len(cluster) {
		return cluster, totalChars, false
	}
	picked = make([]*memory.Memory, 0, n)
	// Evenly-spaced indices including endpoints: 0, step, 2*step, ..., len-1.
	// Using float math for spacing avoids gnarly off-by-one with integer steps.
	for i := 0; i < n; i++ {
		idx := i * (len(cluster) - 1) / (n - 1)
		picked = append(picked, cluster[idx])
	}
	return picked, totalChars, true
}

// findL5Clusters clusters L5 memories by pairwise cosine similarity.
// Uses greedy expansion with full pairwise validation: same algorithm as
// findClusters in l4tol3.go but with configurable threshold and min size.
// Memories without embeddings are silently skipped.
func findL5Clusters(memories []*memory.Memory, threshold float32, minSize int) [][]*memory.Memory {
	used := make([]bool, len(memories))
	var clusters [][]*memory.Memory

	for i, m := range memories {
		if used[i] || len(m.Embedding) == 0 {
			continue
		}

		cluster := []*memory.Memory{m}
		for j := i + 1; j < len(memories); j++ {
			if used[j] || len(memories[j].Embedding) == 0 {
				continue
			}
			// Enforce pairwise constraint: candidate must be >= threshold to ALL current members.
			if allPairwiseSimilarThreshold(cluster, memories[j].Embedding, threshold) {
				cluster = append(cluster, memories[j])
			}
		}

		if len(cluster) >= minSize {
			for _, cm := range cluster {
				for k, om := range memories {
					if om.ID == cm.ID {
						used[k] = true
						break
					}
				}
			}
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// summarizeCluster calls LLM to summarize a cluster of L5 memories and stores the result as L4.
//
// If the cluster's total content size exceeds L5MaxClusterChars, the cluster is
// down-sampled to evenly-spaced representatives before prompt assembly. This
// prevents huge near-duplicate clusters (e.g. thousands of turns from one long
// Claude Code session) from producing prompts that exceed model context limits.
// All original L5 memories are still soft-deleted on success; the resulting L4
// summary represents the entire cluster window, just with sampled detail.
func (c *Compactor) summarizeCluster(ctx context.Context, cluster []*memory.Memory) (int, error) {
	if len(cluster) == 0 {
		return 0, nil
	}

	// Pick which memories' content goes into the prompt, sampling if oversized.
	picked, totalChars, sampled := selectForPrompt(cluster, c.cfg.Compaction.L5MaxClusterChars)
	if sampled {
		c.logger.Warn("L5→L4 cluster oversized, sampling",
			"cluster_size", len(cluster), "sampled_to", len(picked),
			"total_chars", totalChars, "budget", c.cfg.Compaction.L5MaxClusterChars)
	}

	// Build prompt.
	var sb strings.Builder
	sb.WriteString("Summarize these raw observations into 1-3 key insights for future reference.\n")
	sb.WriteString("Focus on decisions made, problems encountered, and solutions found.\n")
	sb.WriteString("Each insight must be one sentence. No headers, no bullet points, no markdown.\n")
	if sampled {
		fmt.Fprintf(&sb, "(Cluster of %d related observations; %d evenly-spaced samples shown.)\n",
			len(cluster), len(picked))
	}
	sb.WriteString("Observations:\n")
	for _, m := range picked {
		sb.WriteString("- ")
		sb.WriteString(m.Content)
		sb.WriteByte('\n')
	}

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	summary, err := c.llm.Complete(llmCtx, sb.String())
	if err != nil {
		return 0, fmt.Errorf("LLM summarize: %w", err)
	}

	// Store summary as L4 memory.
	attrs := map[string]string{
		"source":       "compactor",
		"window_start": cluster[0].CreatedAt.Format(time.RFC3339),
		"window_end":   cluster[len(cluster)-1].CreatedAt.Format(time.RFC3339),
	}
	// Inherit the most common project attribute from the source cluster.
	if p := dominantAttr(cluster, "project"); p != "" {
		attrs["project"] = p
	}
	l4, err := c.store.Store(ctx, 4, summary, attrs)
	if err != nil {
		return 0, fmt.Errorf("store L4 summary: %w", err)
	}

	// Create compacted_from edges: L4 → each L5 source so the lineage is walkable.
	c.addLineageEdges(ctx, l4.ID, cluster)

	// Soft-delete all L5 memories in the cluster.
	ids := make([]string, len(cluster))
	for i, m := range cluster {
		ids[i] = m.ID
	}
	if err := c.store.SoftDelete(ctx, ids, "compacted to L4"); err != nil {
		return 0, fmt.Errorf("soft delete L5 cluster: %w", err)
	}

	return 1, nil // 1 L4 memory created per cluster/window
}

// dominantAttr returns the most frequently occurring value for the given attribute key
// across all memories in the cluster. Returns "" if no memory has the key.
func dominantAttr(cluster []*memory.Memory, key string) string {
	counts := map[string]int{}
	for _, m := range cluster {
		if v := m.Attrs[key]; v != "" {
			counts[v]++
		}
	}
	best, bestN := "", 0
	for v, n := range counts {
		if n > bestN {
			best, bestN = v, n
		}
	}
	return best
}
