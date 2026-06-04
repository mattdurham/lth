// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package db

// MemoryRow is a flat struct matching the columns of the memories table.

// raw IEEE 754 little-endian float32 bytes; may be nil

// outcome polarity: -1.0 (bad) to +1.0 (good), 0.0 neutral
// true once an LLM has set a real valence score
// model used to generate the embedding, e.g. "nomic-ai/nomic-embed-text-v1.5"
