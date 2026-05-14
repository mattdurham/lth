// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// OllamaEmbedder calls an OpenAI-compatible /v1/embeddings endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
	dims    atomic.Int64
}

// NewOllamaEmbedder creates an OllamaEmbedder for the given endpoint and model.
func NewOllamaEmbedder(baseURL, model string, timeoutS int) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: time.Duration(timeoutS) * time.Second,
		},
	}
}

// Embed calls the /v1/embeddings endpoint and returns the float32 embedding vector.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{
		Model: o.model,
		Input: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	//nolint:gosec // URL comes from trusted config file, not user input
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	//nolint:gosec // G704: URL is from trusted config, not user input
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed request returned status %d", resp.StatusCode)
	}

	var respBody struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	if len(respBody.Data) == 0 || len(respBody.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed response contained no data")
	}

	raw := respBody.Data[0].Embedding
	result := make([]float32, len(raw))
	for i, v := range raw {
		result[i] = float32(v)
	}

	o.dims.Store(int64(len(result)))
	return result, nil
}

// Dims returns the dimension of the last successful Embed call, or 0 if never called.
func (o *OllamaEmbedder) Dims() int {
	return int(o.dims.Load())
}
