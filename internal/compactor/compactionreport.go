// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package compactor

type CompactionReport struct {
	SeedL2 int
	SeedL3 int
	L5toL4 int
	L4toL3 int
	L3toL2 int
	Errors []error
}
