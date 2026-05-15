// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaLLM implements the LLM interface via the OpenAI-compatible /v1/chat/completions endpoint.
type OllamaLLM struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaLLM creates a new OllamaLLM with the given base URL, model, and timeout.
func NewOllamaLLM(baseURL, model string, timeoutS int) *OllamaLLM {
	// Compile-time interface check.
	var _ LLM = (*OllamaLLM)(nil)

	return &OllamaLLM{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: time.Duration(timeoutS) * time.Second,
		},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends a user prompt to the LLM and returns the text response.
func (o *OllamaLLM) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody := chatRequest{
		Model: o.model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal llm request: %w", err)
	}

	//nolint:gosec // URL comes from trusted config file, not user input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	//nolint:gosec // G704: URL is from trusted config, not user input
	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("llm response status %d: %s", resp.StatusCode, body)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decode llm response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm response: no choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}
