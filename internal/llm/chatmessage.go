// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
