// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

// StatsRow contains aggregate statistics about the memory store.
type StatsRow struct {
	TotalMemories int
	ByLayer       map[int]int
	TotalEdges    int
}
