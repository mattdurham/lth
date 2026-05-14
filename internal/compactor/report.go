// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

// CompactionReport records how many memories were promoted in each path during RunOnce.
type CompactionReport struct {
	L5toL4 int
	L4toL3 int
	L3toL2 int
	Errors []error
}
