package db

type VectorResult struct {
	*MemoryRow
	Distance float32
}
