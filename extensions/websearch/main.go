//go:build wasip1

// Package main implements the websearch extension for lth.
//
// Provides a `websearch` tool that searches DuckDuckGo Lite and returns
// results as a list of titles, URLs, and snippets.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const ExtensionVersion = "v1.0.0"

// SearchResult represents a single search result
type SearchResult struct {
	Title   string `json:"title"`
	Url     string `json:"url"`
	Snippet string `json:"snippet"`
}

// ─── Host SDK functions (defined in lth host) ────────────────────────────────

//go:noinline
func RegisterTool(name, description string, inputSchema json.RawMessage) {}

//go:noinline
func RegisterToolWithOutput(name, description string, inputSchema, outputSchema json.RawMessage) {}

//go:noinline
func OnToolCall(fn func(callID, toolName string, input json.RawMessage) (result string, isError bool)) {}

//go:noinline
func OnCommand(name string, fn func(args []string)) {}

//go:noinline
func OnSessionStart(fn func()) {}

//go:noinline
func OnBeforeAgentStart(fn func(prompt string)) {}

//go:noinline
func ToolResult(callID, result string, isError bool) {}

//go:noinline
func Modal(text string) {}

//go:noinline
func Notify(text string) {}

//go:noinline
func SetStatus(key, value string) {}

//go:noinline
func SetSystemPrompt(prompt string) {}

//go:noinline
func AppendSystemPrompt(text string) {}

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	// Register the search tool
	RegisterTool(
		"websearch",
		fmt.Sprintf("WebSearch extension %s - Search DuckDuckGo Lite", ExtensionVersion),
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"q": {"type": "string", "description": "Search query"}
			},
			"required": ["q"]
		}`),
	)

	// Handle search tool calls
	OnToolCall(func(callID, name string, input json.RawMessage) (string, bool) {
		if name != "websearch" {
			return "", false
		}

		result, isError := handleSearch(callID, name, input)
		return result, isError
	})
}

// ─── Tool Handler ─────────────────────────────────────────────────────────────

func handleSearch(callID, name string, input json.RawMessage) (string, bool) {
	var params struct {
		Query string `json:"q"`
	}

	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf(`{"error":%q}`, "invalid JSON"), true
	}

	if params.Query == "" {
		return fmt.Sprintf(`{"error":%q}`, "missing query parameter"), true
	}

	results, err := searchDuckDuckGo(params.Query)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error()), true
	}

	response := map[string][]SearchResult{
		"results": results,
	}

	respBytes, _ := json.Marshal(response)
	return string(respBytes), false
}

// ─── Search Implementation ────────────────────────────────────────────────────

func searchDuckDuckGo(query string) ([]SearchResult, error) {
	url := fmt.Sprintf("https://lite.duckduckgo.com/lite/?q=%s", urlEncode(query))

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	results := parseLiteResults(string(body))

	return results, nil
}

func urlEncode(query string) string {
	return strings.ReplaceAll(query, " ", "+")
}

// parseLiteResults extracts search results from DuckDuckGo Lite HTML
func parseLiteResults(html string) []SearchResult {
	var results []SearchResult

	links := strings.Split(html, `<a class="result-link"`)

	for i := 1; i < len(links); i++ {
		link := links[i]

		result := SearchResult{}

		start := strings.Index(link, `href="`)
		if start == -1 {
			continue
		}
		start += 6
		end := strings.Index(link[start:], `"`)
		if end == -1 {
			continue
		}
		result.Url = link[start : start+end]

		titleStart := strings.Index(link, `>`)
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(link[titleStart+1:], `</a>`)
		if titleEnd == -1 {
			continue
		}
		result.Title = strings.TrimSpace(link[titleStart+1 : titleStart+titleEnd])

		descStart := strings.Index(link, `<p class="result-snippet">`)
		if descStart != -1 {
			descEnd := strings.Index(link[descStart:], `</p>`)
			if descEnd != -1 {
				desc := link[descStart+len(`<p class="result-snippet">`) : descStart+descEnd]
				result.Snippet = strings.TrimSpace(strings.ReplaceAll(desc, "<br>", " "))
			}
		} else {
			result.Snippet = "No description available"
		}

		results = append(results, result)
	}

	return results
}

func main() {}
