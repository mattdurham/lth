// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

import (
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
