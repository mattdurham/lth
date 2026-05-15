// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

// ScoredMemory is a Memory with its composite search score and score breakdown.
type ScoredMemory struct {
	*Memory
	Score           float32
	TimeScore       float32
	ImportanceScore float32
	VectorScore     float32
	ValenceScore    float32 // contribution of valence to the composite score
}
