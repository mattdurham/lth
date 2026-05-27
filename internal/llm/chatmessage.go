package llm

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
