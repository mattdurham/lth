// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package memory

// SearchRequest holds parameters for a multi-modal memory search.

// memory IDs for PPR graph traversal seeding
// weight for time decay component
// weight for importance component
// weight for cosine similarity component
// if set, only return memories with Valence >= MinValence
// if set, only return memories with Valence <= MaxValence
// if true, use LLM to generate related queries before searching
