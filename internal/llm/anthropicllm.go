// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package llm

import (
	"context"
	"net/http"
)

// TokenSource yields a valid Anthropic OAuth access token. When set on an
// AnthropicLLM, the client uses Authorization: Bearer + Claude Code identity
// headers instead of x-api-key. See internal/llm/anthropicauth.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

type AnthropicLLM struct {
	apiKey  string
	model   string
	client  *http.Client
	baseURL string
	// tokenSource, if non-nil, supplies OAuth access tokens for Bearer auth.
	// When set, apiKey is ignored.
	tokenSource TokenSource
}

// SetTokenSource enables OAuth (Bearer) authentication for this client.
// The apiKey field is ignored when a token source is set.
func (a *AnthropicLLM) SetTokenSource(ts TokenSource) {
	a.tokenSource = ts
}
