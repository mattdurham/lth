// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

type VectorResult struct {
	*MemoryRow
	Distance float32
}
