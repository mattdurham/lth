package llm

import "net/http"

type OllamaLLM struct {
	baseURL string
	model   string
	client  *http.Client
}
