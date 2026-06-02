// Package layers defines the canonical names for lth memory layers.
// The underlying storage uses integers 1-5; these names are display-only.
package layers

// Names maps layer number to its human-readable name.
var Names = map[int]string{
	1: "core",
	2: "principles",
	3: "knowledge",
	4: "workspace",
	5: "observations",
}

// Name returns the display name for a layer number, or "unknown" if out of range.
func Name(layer int) string {
	if n, ok := Names[layer]; ok {
		return n
	}
	return "unknown"
}
