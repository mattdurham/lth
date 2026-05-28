// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"
const anthropicMaxTokens = 8192

// AnthropicLLM calls the Anthropic Messages API.

// configurable for testing; defaults to anthropicAPIURL

// Compile-time interface check.
var _ LLM = (*AnthropicLLM)(nil)

// NewAnthropicLLM creates a new AnthropicLLM. If apiKey is empty, the
// ANTHROPIC_API_KEY environment variable is read at call time.
func NewAnthropicLLM(apiKey, model string, timeoutS int) *AnthropicLLM {
	return &AnthropicLLM{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: time.Duration(timeoutS) * time.Second},
		baseURL: anthropicAPIURL,
	}
}

// Complete sends a user prompt to the Anthropic Messages API and returns the text response.
func (a *AnthropicLLM) Complete(ctx context.Context, prompt string) (string, error) {
	apiKey := a.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	reqBody := anthropicRequest{
		Model:     a.model,
		MaxTokens: anthropicMaxTokens,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal anthropic request: %w", err)
	}

	//nolint:gosec // URL comes from constant or trusted config, not user input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := a.client.Do(req) //nolint:gosec // G704: URL is hardcoded constant or trusted config, not user input
	if err != nil {
		return "", fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("anthropic response status %d: %s", resp.StatusCode, body)
	}

	var ar anthropicResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&ar); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w", err)
	}

	for _, block := range ar.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("anthropic response: no text content block")
}
