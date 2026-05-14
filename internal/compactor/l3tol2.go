// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mattdurham/lth/internal/graph"
)

// compactL3toL2 promotes eligible L3 memories to L2 via LLM pattern recognition.
// Triggers when access_count >= L3EpisodesMin AND importance > L3ImportanceMin.
// L3 memories are NOT soft-deleted after promotion (they remain active).
func (c *Compactor) compactL3toL2(ctx context.Context) (int, error) {
	l3Memories, err := c.store.ListLayer(ctx, 3)
	if err != nil {
		return 0, fmt.Errorf("list L3: %w", err)
	}
	if len(l3Memories) == 0 {
		return 0, nil
	}

	minEpisodes := c.cfg.Compaction.L3EpisodesMin
	minImportance := c.cfg.Compaction.L3ImportanceMin

	promoted := 0
	for _, m := range l3Memories {
		if m.AccessCount < minEpisodes || m.Importance <= minImportance {
			continue
		}

		// Check if this L3 already has a derived L2 (via neighbors).
		neighbors := c.graph.Neighbors(m.ID, []string{"derived_from"})
		if len(neighbors) > 0 {
			continue // already promoted
		}

		n, err := c.promoteToL2(ctx, m.ID, m.Content)
		if err != nil {
			c.logger.Warn("L3→L2 promotion failed", "err", err, "memory_id", m.ID)
			continue
		}
		promoted += n
	}

	return promoted, nil
}

// promoteToL2 creates an L2 memory from a single L3 memory.
func (c *Compactor) promoteToL2(ctx context.Context, sourceID, content string) (int, error) {
	prompt := fmt.Sprintf(
		"What general rule or heuristic does this repeated skill represent? "+
			"State it as a concise behavioral rule.\nSkill: %s", content)

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	rule, err := c.llm.Complete(llmCtx, prompt)
	if err != nil {
		return 0, fmt.Errorf("LLM rule extraction: %w", err)
	}

	attrs := map[string]string{"source": "compactor"}
	l2, err := c.store.Store(ctx, 2, rule, attrs)
	if err != nil {
		return 0, fmt.Errorf("store L2: %w", err)
	}

	// Add derived_from edge: L2 → L3.
	e := &graph.Edge{
		ID:       uuid.New().String(),
		FromID:   l2.ID,
		ToID:     sourceID,
		EdgeType: "derived_from",
		Weight:   1.0,
		Created:  time.Now().UTC(),
	}
	if err := c.graph.AddEdge(ctx, e); err != nil {
		c.logger.Warn("failed to add derived_from edge", "err", err)
	}

	// L3 is NOT soft-deleted — it remains active.
	return 1, nil
}
