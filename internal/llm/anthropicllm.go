package llm

import "net/http"

type AnthropicLLM struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
}
