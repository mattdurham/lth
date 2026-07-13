// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package graph

type adjacency struct {
	neighborID string
	edgeType   string
	weight     float32
	outgoing   bool
}
