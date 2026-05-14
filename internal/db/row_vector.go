// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

// VectorResult combines a MemoryRow with the L2 distance returned by vec0 KNN search.
type VectorResult struct {
	*MemoryRow
	Distance float32
}
