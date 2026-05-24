package llm

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
