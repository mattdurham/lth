// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}
