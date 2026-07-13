// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

type SearchRequest struct {
	Query      string
	Layers     []int
	TopK       int
	Seeds      []string
	Alpha      float32
	Beta       float32
	Gamma      float32
	MinValence *float32
	MaxValence *float32
	Expand     bool
	// FilterAttrs boosts memories whose attributes contain all given key=value pairs.
	// Memories matching all pairs receive an AttrBoost score multiplier (default 1.5×).
	// Does not hard-filter — non-matching results still appear, just ranked lower.
	FilterAttrs map[string]string
}
