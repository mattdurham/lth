package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const maxToolIterations = 10

// Tool defines a callable tool for agentic completions.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema object
}

// ToolExecutor executes a named tool with the given JSON input and returns a result string.
type ToolExecutor func(ctx context.Context, name string, input json.RawMessage) (string, error)

// CompleteWithTools runs an agentic loop: Haiku can call tools until it reaches end_turn.
func (a *AnthropicLLM) CompleteWithTools(ctx context.Context, system, userMsg string, tools []Tool, exec ToolExecutor) (string, error) {
	apiKey := a.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	apiTools := make([]toolDef, len(tools))
	for i, t := range tools {
		apiTools[i] = toolDef{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
	}

	// Initial user message
	userContent, _ := json.Marshal(userMsg)
	msgs := []toolMsg{{Role: "user", Content: userContent}}

	for iter := 0; iter < maxToolIterations; iter++ {
		reqBody := toolRequest{
			Model:     a.model,
			MaxTokens: anthropicMaxTokens,
			System:    system,
			Tools:     apiTools,
			Messages:  msgs,
		}
		data, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("marshal: %w", err)
		}

		//nolint:gosec
		req, err := newAnthropicHTTPRequest(ctx, a.baseURL, apiKey, data)
		if err != nil {
			return "", err
		}

		resp, err := a.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("anthropic request: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return "", fmt.Errorf("anthropic status %d: %s", resp.StatusCode, body)
		}

		var ar toolResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&ar); err != nil {
			return "", fmt.Errorf("decode: %w", err)
		}

		// Collect text from this response turn
		var text string
		for _, b := range ar.Content {
			if b.Type == "text" {
				text = b.Text
			}
		}

		if ar.StopReason == "end_turn" {
			return text, nil
		}

		if ar.StopReason != "tool_use" {
			return text, nil
		}

		// Append assistant message (contains both text and tool_use blocks)
		assistantBlocks, _ := json.Marshal(ar.Content)
		msgs = append(msgs, toolMsg{Role: "assistant", Content: assistantBlocks})

		// Execute each tool call and collect results
		var resultBlocks []toolResultBlock
		for _, b := range ar.Content {
			if b.Type != "tool_use" {
				continue
			}
			result, err := exec(ctx, b.Name, b.Input)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			resultBlocks = append(resultBlocks, toolResultBlock{
				Type:      "tool_result",
				ToolUseID: b.ID,
				Content:   result,
			})
		}

		if len(resultBlocks) == 0 {
			return text, nil
		}

		toolResultContent, _ := json.Marshal(resultBlocks)
		msgs = append(msgs, toolMsg{Role: "user", Content: toolResultContent})
	}

	return "", fmt.Errorf("exceeded max tool iterations (%d)", maxToolIterations)
}

func newAnthropicHTTPRequest(ctx context.Context, baseURL, apiKey string, data []byte) (*http.Request, error) {
	//nolint:gosec
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")
	return req, nil
}

// --- internal types for tool-use API ---

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []contentBlock
}

type toolRequest struct {
	Model     string   `json:"model"`
	MaxTokens int      `json:"max_tokens"`
	System    string   `json:"system,omitempty"`
	Tools     []toolDef `json:"tools,omitempty"`
	Messages  []toolMsg `json:"messages"`
}

type toolContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type toolResponse struct {
	StopReason string             `json:"stop_reason"`
	Content    []toolContentBlock `json:"content"`
}
