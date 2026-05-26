package memory

type ScoredMemory struct {
	*Memory
	Score           float32
	TimeScore       float32
	ImportanceScore float32
	VectorScore     float32
	ValenceScore    float32
}
