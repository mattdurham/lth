// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

// SearchRequest holds parameters for a multi-modal memory search.
type SearchRequest struct {
	Query      string
	Layers     []int
	TopK       int
	Seeds      []string // memory IDs for PPR graph traversal seeding
	Alpha      float32  // weight for time decay component
	Beta       float32  // weight for importance component
	Gamma      float32  // weight for cosine similarity component
	MinValence *float32 // if set, only return memories with Valence >= MinValence
	MaxValence *float32 // if set, only return memories with Valence <= MaxValence
}
