// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mattdurham/lth/internal/graph"
	"github.com/mattdurham/lth/internal/memory"
)

const cosineClusterThreshold = float32(0.85)

// compactL4toL3 clusters similar L4 memories and promotes them to L3 via LLM.
// Triggers when a cluster of size >= L4ClusterSize exists with pairwise cosine >= 0.85.
func (c *Compactor) compactL4toL3(ctx context.Context) (int, error) {
	l4Memories, err := c.store.ListLayer(ctx, 4)
	if err != nil {
		return 0, fmt.Errorf("list L4: %w", err)
	}
	if len(l4Memories) == 0 {
		return 0, nil
	}

	// Find clusters via greedy cosine clustering.
	clusters := findClusters(l4Memories, c.cfg.Compaction.L4ClusterSize)
	if len(clusters) == 0 {
		return 0, nil
	}

	promoted := 0
	for _, cluster := range clusters {
		n, err := c.promoteCluster(ctx, cluster)
		if err != nil {
			c.logger.Warn("L4→L3 cluster promotion failed", "err", err)
			continue
		}
		promoted += n
	}

	return promoted, nil
}

// findClusters finds groups of memories where all pairwise cosine similarities are >= threshold
// and the group size is >= minSize.
func findClusters(memories []*memory.Memory, minSize int) [][]*memory.Memory {
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
			if allPairwiseSimilar(cluster, memories[j].Embedding) {
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

// allPairwiseSimilar returns true if candidate has cosine similarity >= cosineClusterThreshold
// with every member of the cluster. It delegates to the shared allPairwiseSimilarThreshold
// helper using the package-level L4 threshold constant.
func allPairwiseSimilar(members []*memory.Memory, candidate []float32) bool {
	return allPairwiseSimilarThreshold(members, candidate, cosineClusterThreshold)
}

// promoteCluster creates a single L3 memory from a cluster of L4 memories.
func (c *Compactor) promoteCluster(ctx context.Context, cluster []*memory.Memory) (int, error) {
	// Build centroid embedding (mean of member embeddings, re-normalized).
	centroid := computeCentroid(cluster)

	// Build prompt.
	var sb strings.Builder
	sb.WriteString("What skill or pattern do these situational memories share? Write a concise skill description.\nMemories:\n")
	for _, m := range cluster {
		sb.WriteString("- ")
		sb.WriteString(m.Content)
		sb.WriteByte('\n')
	}

	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.LLM.TimeoutS)*time.Second)
	defer cancel()

	skillDesc, err := c.llm.Complete(llmCtx, sb.String())
	if err != nil {
		return 0, fmt.Errorf("LLM skill description: %w", err)
	}

	// Store as L3.
	attrs := map[string]string{"source": "compactor"}
	l3, err := c.store.Store(ctx, 3, skillDesc, attrs)
	if err != nil {
		return 0, fmt.Errorf("store L3: %w", err)
	}

	// Add compacted_from edges.
	for _, m := range cluster {
		e := &graph.Edge{
			FromID:   l3.ID,
			ToID:     m.ID,
			EdgeType: "compacted_from",
			Weight:   1.0,
			Created:  time.Now().UTC(),
		}
		if err := c.graph.AddEdge(ctx, e); err != nil {
			c.logger.Warn("failed to add compacted_from edge", "err", err)
		}
	}

	// Set centroid embedding on the L3 memory (if available).
	_ = centroid // stored in memory.Store via embedding; we can't retroactively set embedding via Store interface

	// Soft-delete cluster members.
	ids := make([]string, len(cluster))
	for i, m := range cluster {
		ids[i] = m.ID
	}
	if err := c.store.SoftDelete(ctx, ids, "compacted to L3"); err != nil {
		return 0, fmt.Errorf("soft delete L4 cluster: %w", err)
	}

	return 1, nil
}

// computeCentroid computes the mean embedding of a cluster and normalizes it.
func computeCentroid(cluster []*memory.Memory) []float32 {
	if len(cluster) == 0 {
		return nil
	}

	dims := 0
	for _, m := range cluster {
		if len(m.Embedding) > 0 {
			dims = len(m.Embedding)
			break
		}
	}
	if dims == 0 {
		return nil
	}

	centroid := make([]float32, dims)
	count := 0
	for _, m := range cluster {
		if len(m.Embedding) != dims {
			continue
		}
		for i, v := range m.Embedding {
			centroid[i] += v
		}
		count++
	}

	if count == 0 {
		return nil
	}

	// Mean.
	for i := range centroid {
		centroid[i] /= float32(count)
	}

	// Normalize.
	var norm float64
	for _, v := range centroid {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		norm = 1.0 / math.Sqrt(norm) // L2 norm: sqrt of sum-of-squares
		for i := range centroid {
			centroid[i] = float32(float64(centroid[i]) * norm)
		}
	}

	return centroid
}
