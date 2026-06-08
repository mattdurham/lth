// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"

	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
	"github.com/mattdurham/lth/internal/vector"
)

// allPairwiseSimilarThreshold returns true if candidate has cosine similarity >= threshold
// with every member of the cluster.
func allPairwiseSimilarThreshold(members []*memory.Memory, candidate []float32, threshold float32) bool {
	for _, m := range members {
		if vector.Cosine(m.Embedding, candidate) < threshold {
			return false
		}
	}
	return true
}

// addLineageEdges creates compacted_from edges from a newly created memory to each source
// memory in the cluster it was derived from. This makes the full compaction chain walkable.
func (c *Compactor) addLineageEdges(ctx context.Context, newID string, sources []*memory.Memory) {
	for _, src := range sources {
		e := &graph.Edge{
			FromID:   newID,
			ToID:     src.ID,
			EdgeType: "compacted_from",
			Weight:   1.0,
		}
		if err := c.graph.AddEdge(ctx, e); err != nil {
			c.logger.Warn("failed to add compacted_from lineage edge", "err", err)
		}
	}
}
