// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// MaxEmbedInputBytes is the hard upper bound on text length sent to the embedding
// endpoint. nomic-embed-text-v1.5 (the default) advertises an 8192-token context;
// 30 KB at typical text density (~4 bytes/token) is ~7500 tokens, leaving headroom
// for tokenizer overhead and CJK expansion. Inputs longer than this are truncated
// at a UTF-8 boundary and a debug log is emitted. Without this cap, a single
// 60 KB memory could repeatedly fail at the server (HTTP 413 or token-limit error)
// and starve the BackfillEmbeddings goroutine on infinite retries.
const MaxEmbedInputBytes = 30 * 1024

// OllamaEmbedder calls an OpenAI-compatible /v1/embeddings endpoint.

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
//
// Inputs longer than MaxEmbedInputBytes are truncated at the nearest valid UTF-8
// boundary before being sent. This prevents pathological memories (50+ KB watcher
// rows, big tool-result dumps) from infinitely failing against the embedder's
// token limit and blocking the backfill loop.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if len(text) > MaxEmbedInputBytes {
		original := len(text)
		text = truncateUTF8(text, MaxEmbedInputBytes)
		slog.Debug("embed input truncated", "original_bytes", original, "truncated_bytes", len(text), "cap", MaxEmbedInputBytes)
	}
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

// truncateUTF8 returns the longest prefix of s that is <= max bytes AND ends on
// a UTF-8 character boundary. Splitting mid-rune would produce invalid UTF-8 that
// some embedding servers reject.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Walk backwards from max until we hit the start of a UTF-8 sequence.
	// A continuation byte has the high bits 10xxxxxx (0x80..0xBF).
	i := max
	for i > 0 && (s[i]&0xC0) == 0x80 {
		i--
	}
	return s[:i]
}
