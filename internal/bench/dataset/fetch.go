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

// NewHFClient returns a new HFClient with default HTTP client.
func NewHFClient() *HFClient {
	return &HFClient{
		httpClient: &http.Client{},
		baseURL:    hfBaseURL,
	}
}

const hfPageSize = 100 // HuggingFace API maximum rows per request

// FetchProblems retrieves up to limit problems filtered by language, paginating as needed.
// offset is a logical offset into the filtered result set (not the raw dataset offset).
// language="" returns all languages.
func (c *HFClient) FetchProblems(ctx context.Context, offset, limit int, language string) ([]Problem, error) {
	var problems []Problem
	rawOffset := 0
	skipped := 0

	for len(problems) < limit {
		page, total, err := c.fetchPage(ctx, rawOffset, hfPageSize)
		if err != nil {
			return nil, err
		}
		for _, p := range page {
			if language != "" && p.Language() != language {
				continue
			}
			if skipped < offset {
				skipped++
				continue
			}
			problems = append(problems, p)
			if len(problems) == limit {
				return problems, nil
			}
		}
		rawOffset += len(page)
		if rawOffset >= total || len(page) == 0 {
			break
		}
	}
	return problems, nil
}

// fetchPage retrieves one page of raw rows. Returns rows, total dataset size, error.
func (c *HFClient) fetchPage(ctx context.Context, offset, length int) ([]Problem, int, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, 0, fmt.Errorf("parse base URL: %w", err)
	}
	q := u.Query()
	q.Set("dataset", "SWE-bench/SWE-bench_Multilingual")
	q.Set("config", "default")
	q.Set("split", "test")
	q.Set("offset", strconv.Itoa(offset))
	q.Set("length", strconv.Itoa(length))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fetch problems: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("fetch problems: unexpected status %d", resp.StatusCode)
	}

	var hfResp struct {
		Rows         []hfRow `json:"rows"`
		NumRowsTotal int     `json:"num_rows_total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hfResp); err != nil {
		return nil, 0, fmt.Errorf("decode response: %w", err)
	}

	problems := make([]Problem, 0, len(hfResp.Rows))
	for _, row := range hfResp.Rows {
		problems = append(problems, row.Row)
	}
	return problems, hfResp.NumRowsTotal, nil
}
