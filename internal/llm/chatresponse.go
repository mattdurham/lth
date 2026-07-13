// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
