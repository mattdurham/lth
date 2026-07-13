// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}
