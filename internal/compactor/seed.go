// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/memory"
)

// seedResponse is the LLM's structured output for the seed compaction path.

// seedSkill holds a skill description and its tags.

// compactSeed auto-seeds L2/L3 from L5 history when those layers are sparse.
// Runs before normal compaction in RunOnce. Uses semantic clustering (same as L5→L4)
// so each LLM call receives topically coherent memories — producing specific, useful output.
// L5 memories are never soft-deleted by this path.
func (c *Compactor) compactSeed(ctx context.Context) (l2Count, l3Count int, err error) {
	stats, err := c.store.Stats(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("seed stats: %w", err)
	}

	needsL2 := stats.ByLayer[2] < c.cfg.Compaction.SeedMinL2
	needsL3 := stats.ByLayer[3] < c.cfg.Compaction.SeedMinL3
	l5Count := stats.ByLayer[5]

	if (!needsL2 && !needsL3) || l5Count < c.cfg.Compaction.L5Threshold {
		return 0, 0, nil
	}

	// Load L5 memories and cluster by cosine similarity for topical coherence.
	l5, err := c.store.ListLayer(ctx, 5)
	if err != nil {
		return 0, 0, fmt.Errorf("list L5 for seed: %w", err)
	}

	// Exclude L5 memories already consumed as a seed source in a previous
	// run. Without this, the same L5 cluster gets re-selected and
	// re-summarized by a non-deterministic LLM on every tick while
	// SeedMinL2/SeedMinL3 remain unmet (easily true for a long time on a
	// low-traffic install) -- and since the LLM rarely regenerates
	// byte-identical wording, content-hash dedup in Store never fires,
	// producing duplicate-in-substance L2/L3 memories describing the same
	// underlying cluster. Mirrors compactL3toL2's derived_from Neighbors
	// check -- the same entity-ID guard pattern, applied to this path too.
	fresh := make([]*memory.Memory, 0, len(l5))
	for _, m := range l5 {
		if len(c.graph.Neighbors(m.ID, []string{"compacted_from"})) > 0 {
			continue // already used as a seed source
		}
		fresh = append(fresh, m)
	}

	clusters := findL5Clusters(fresh, c.cfg.Compaction.L5ClusterThreshold, 2)

	// Process at most SeedSample clusters per run.
	processed := 0
	for _, cluster := range clusters {
		if processed >= c.cfg.Compaction.SeedSample {
			break
		}

		l2n, l3n, batchErr := c.processSeedBatch(ctx, cluster, needsL2, needsL3)
		if batchErr != nil {
			c.logger.Warn("seed batch failed", "err", batchErr, "cluster_size", len(cluster))
			processed++
			continue
		}
		l2Count += l2n
		l3Count += l3n
		processed++

		// Stop seeding once layers are full enough.
		if l2Count+stats.ByLayer[2] >= c.cfg.Compaction.SeedMinL2 {
			needsL2 = false
		}
		if l3Count+stats.ByLayer[3] >= c.cfg.Compaction.SeedMinL3 {
			needsL3 = false
		}
		if !needsL2 && !needsL3 {
			break
		}
	}

	return l2Count, l3Count, nil
}

// processSeedBatch calls the LLM on one cluster and stores the results.
// It never modifies or deletes L5 memories.
func (c *Compactor) processSeedBatch(ctx context.Context, batch []*memory.Memory, wantL2, wantL3 bool) (l2n, l3n int, err error) {
	prompt := buildSeedPrompt(batch, wantL2, wantL3)

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	resp, err := c.llm.Complete(llmCtx, prompt)
	if err != nil {
		return 0, 0, fmt.Errorf("seed LLM: %w", err)
	}

	sr, err := parseSeedResponse(resp)
	if err != nil {
		return 0, 0, fmt.Errorf("parse seed response: %w", err)
	}

	attrs := map[string]string{"source": "compactor-seed"}

	if wantL2 {
		for _, rule := range sr.Rules {
			if strings.TrimSpace(rule) == "" {
				continue
			}
			m, storeErr := c.store.Store(ctx, 2, rule, attrs)
			if storeErr != nil {
				c.logger.Warn("store seed L2 failed", "err", storeErr)
				continue
			}
			c.addLineageEdges(ctx, m.ID, batch)
			l2n++
		}
	}

	if wantL3 {
		for _, skill := range sr.Skills {
			if strings.TrimSpace(skill.Content) == "" {
				continue
			}
			skillAttrs := map[string]string{"source": "compactor-seed", "tags": skill.Tags}
			m, storeErr := c.store.Store(ctx, 3, skill.Content, skillAttrs)
			if storeErr != nil {
				c.logger.Warn("store seed L3 failed", "err", storeErr)
				continue
			}
			c.addLineageEdges(ctx, m.ID, batch)
			l3n++
		}
	}

	return l2n, l3n, nil
}

// buildSeedPrompt constructs the LLM prompt for a semantically coherent cluster of L5 memories.
func buildSeedPrompt(batch []*memory.Memory, wantL2, wantL3 bool) string {
	var sb strings.Builder
	sb.WriteString("You are analyzing an engineer's work history. Based on these observations, extract:\n\n")

	if wantL2 {
		sb.WriteString("BEHAVIORAL RULES: 2-3 rules this person consistently follows.\n")
		sb.WriteString("Format: concise imperative, e.g. \"Always handle errors explicitly\"\n\n")
	}
	if wantL3 {
		sb.WriteString("SKILLS: 3-5 specific technical skills or procedures demonstrated.\n")
		sb.WriteString("Format: concise description + comma-separated tags field\n\n")
	}

	sb.WriteString("Observations:\n")
	for _, m := range batch {
		sb.WriteString("- ")
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sb.WriteString(content)
		sb.WriteByte('\n')
	}

	sb.WriteString("\nRespond with ONLY valid JSON:\n")
	sb.WriteString("{\n  \"rules\": [\"rule1\", \"rule2\"],\n")
	sb.WriteString("  \"skills\": [{\"content\": \"skill description\", \"tags\": \"go,errors\"}, ...]\n}\n")
	sb.WriteString("If you cannot identify rules or skills, return {\"rules\": [], \"skills\": []}.")

	return sb.String()
}

// parseSeedResponse parses the LLM JSON response, stripping markdown code fences if present.
func parseSeedResponse(resp string) (*seedResponse, error) {
	resp = strings.TrimSpace(resp)
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var sr seedResponse
	if err := json.Unmarshal([]byte(resp), &sr); err != nil {
		return nil, fmt.Errorf("unmarshal seed response: %w", err)
	}
	return &sr, nil
}
