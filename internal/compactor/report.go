// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

// CompactionReport records how many memories were promoted in each path during RunOnce.
type CompactionReport struct {
	SeedL2 int // L2 behavioral rules seeded from L5 history
	SeedL3 int // L3 skills seeded from L5 history
	L5toL4 int
	L4toL3 int
	L3toL2 int
	Errors []error
}
