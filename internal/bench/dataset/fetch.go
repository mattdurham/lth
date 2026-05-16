// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package dataset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const hfBaseURL = "https://datasets-server.huggingface.co/rows"

// HFClient fetches rows from the HuggingFace datasets API.
type HFClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewHFClient returns a new HFClient with default HTTP client.
func NewHFClient() *HFClient {
	return &HFClient{
		httpClient: &http.Client{},
		baseURL:    hfBaseURL,
	}
}

type hfResponse struct {
	Rows []hfRow `json:"rows"`
}

type hfRow struct {
	Row Problem `json:"row"`
}

// FetchProblems retrieves up to limit problems starting at offset, filtered by language.
// language="" returns all languages.
func (c *HFClient) FetchProblems(ctx context.Context, offset, limit int, language string) ([]Problem, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("dataset", "SWE-bench/SWE-bench_Multilingual")
	q.Set("config", "default")
	q.Set("split", "test")
	q.Set("offset", strconv.Itoa(offset))
	q.Set("length", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch problems: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch problems: unexpected status %d", resp.StatusCode)
	}

	var hfResp hfResponse
	if err := json.NewDecoder(resp.Body).Decode(&hfResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var problems []Problem
	for _, row := range hfResp.Rows {
		p := row.Row
		if language != "" && p.Language() != language {
			continue
		}
		problems = append(problems, p)
	}
	return problems, nil
}
