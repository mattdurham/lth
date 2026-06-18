// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const anthropicAPIURL = "https://api.anthropic.com"
const anthropicVersion = "2023-06-01"
// anthropicMaxTokens caps the output tokens per request. Set to 32k -- well
// below Haiku 4.5's 64k ceiling but 4x the previous 8k value, which was
// truncating fact-extraction responses on large libsonnet/yaml inputs and
// causing the resulting JSON to fail to unmarshal. Output is pay-per-use:
// raising the cap only costs more when the model actually generates more,
// not on every call.
const anthropicMaxTokens = 32768

// AnthropicLLM calls the Anthropic Messages API.

// configurable for testing; defaults to anthropicAPIURL

// Compile-time interface check.
var _ LLM = (*AnthropicLLM)(nil)

// NewAnthropicLLM creates a new AnthropicLLM. If apiKey is empty, the
// ANTHROPIC_API_KEY environment variable is read at call time.
//
// timeoutS is ignored: the http.Client uses no built-in timeout and relies
// entirely on the context.Context passed to Complete for cancellation. This
// lets the surrounding Chain hot-reload per-backend timeouts via the
// timeoutLookup without the client baking the construction-time value in.
func NewAnthropicLLM(apiKey, model string, timeoutS int) *AnthropicLLM {
	_ = timeoutS // intentionally unused; see ctx-based timeout above
	return &AnthropicLLM{
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{},
		baseURL: anthropicAPIURL,
	}
}

// Complete sends a user prompt to the Anthropic Messages API and returns the text response.
func (a *AnthropicLLM) Complete(ctx context.Context, prompt string) (string, error) {
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

	req, err := a.newAnthropicRequest(ctx, data)
	if err != nil {
		return "", fmt.Errorf("create anthropic request: %w", err)
	}

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
