// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}
