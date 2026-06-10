// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import "net/http"

// OllamaLLM is a generic OpenAI-compatible /v1/chat/completions client. The
// name is historical -- it works for Ollama, vLLM, LM Studio, llama.cpp
// server, LocalAI, OpenAI proper, OpenRouter, Together, Groq, DeepSeek,
// and any other endpoint that speaks the chat completions schema.
//
// If apiKey is non-empty, requests include `Authorization: Bearer <apiKey>`.
type OllamaLLM struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}
