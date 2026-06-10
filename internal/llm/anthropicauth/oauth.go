// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

// Package anthropicauth implements the claude.ai OAuth 2.0 PKCE flow used by
// Claude Code, allowing lth to authenticate against an Anthropic Pro/Max
// subscription instead of an API key.
package anthropicauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Endpoints and client metadata. These mirror Claude Code so that the
// resulting tokens are accepted by Anthropic's OAuth-gated API path.
//
// The client ID is base64-obfuscated to match upstream convention (it is a
// public identifier, not a secret).
var clientIDObfuscated = "OWQxYzI1MGEtZTYxYi00NGQ5LTg4ZWQtNTk0NGQxOTYyZjVl"

const (
	AuthorizeURL = "https://claude.ai/oauth/authorize"
	// TokenURL is the OAuth token endpoint.
	TokenURL = "https://platform.claude.com/v1/oauth/token" //nolint:gosec // G101: public URL constant, not a credential
	CallbackHost   = "127.0.0.1"
	CallbackPort   = 53692
	CallbackPath   = "/callback"
	Scopes         = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
	expirySkew     = 5 * time.Minute
	httpTimeoutSec = 30
)

// ClientID returns the OAuth client ID used by lth (same as Claude Code).
func ClientID() string {
	b, _ := base64.StdEncoding.DecodeString(clientIDObfuscated)
	return string(b)
}

// RedirectURI returns the loopback redirect URI registered for the flow.
func RedirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", CallbackPort, CallbackPath)
}

// AuthorizeURLWithParams builds the full authorize URL for a given PKCE
// challenge and state. The verifier should be used as state so the callback
// server can validate it.
func AuthorizeURLWithParams(challenge, state string) string {
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", ClientID())
	q.Set("response_type", "code")
	q.Set("redirect_uri", RedirectURI())
	q.Set("scope", Scopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	return AuthorizeURL + "?" + q.Encode()
}

// LoginResult is returned to the caller from a callback server.
type LoginResult struct {
	Code  string
	State string
}

// callbackServer accepts a single OAuth redirect and returns the code+state.
type callbackServer struct {
	srv      *http.Server
	listener net.Listener
	result   chan LoginResult
	errCh    chan error
}

// startCallbackServer binds CallbackHost:CallbackPort and listens for a
// redirect with ?code=&state=. It returns the first valid hit, or an error.
func startCallbackServer(expectedState string) (*callbackServer, error) {
	addr := fmt.Sprintf("%s:%d", CallbackHost, CallbackPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	cs := &callbackServer{
		listener: ln,
		result:   make(chan LoginResult, 1),
		errCh:    make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "<h1>Login failed</h1><p>"+htmlEscape(e)+"</p>") //nolint:gosec // G705: input is HTML-escaped
			cs.errCh <- fmt.Errorf("oauth error: %s", e)
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if code == "" || state == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "Missing code or state")
			return
		}
		if state != expectedState {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "State mismatch")
			cs.errCh <- errors.New("oauth state mismatch")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><body style="font-family:system-ui;padding:2rem">
<h1>lth — authentication complete</h1>
<p>You can close this window and return to your terminal.</p>
</body></html>`)
		cs.result <- LoginResult{Code: code, State: state}
	})

	cs.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := cs.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cs.errCh <- err
		}
	}()
	return cs, nil
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func (c *callbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.srv.Shutdown(ctx)
}

func (c *callbackServer) Wait(ctx context.Context) (LoginResult, error) {
	select {
	case r := <-c.result:
		return r, nil
	case err := <-c.errCh:
		return LoginResult{}, err
	case <-ctx.Done():
		return LoginResult{}, ctx.Err()
	}
}

// OpenBrowser opens the given URL in the user's default browser. It returns
// nil on success; callers should treat any error as non-fatal (the URL can
// always be opened manually).
func OpenBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

// LoginOptions configures the interactive OAuth flow.
type LoginOptions struct {
	// HTTPClient is used for the token exchange. Defaults to a 30s timeout client.
	HTTPClient *http.Client
	// OpenBrowser, if false, skips automatic browser launch.
	OpenBrowser bool
	// OnAuthURL, if set, receives the authorize URL before waiting for the callback.
	OnAuthURL func(url string)
	// TokenURL overrides the token exchange endpoint (used by tests).
	TokenURL string
}

// Login runs the full PKCE flow: starts a callback server, opens the browser,
// waits for the redirect, and exchanges the code for tokens.
func Login(ctx context.Context, opts LoginOptions) (*Credentials, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	cs, err := startCallbackServer(pkce.Verifier)
	if err != nil {
		return nil, err
	}
	defer cs.Close()

	authURL := AuthorizeURLWithParams(pkce.Challenge, pkce.Verifier)
	if opts.OnAuthURL != nil {
		opts.OnAuthURL(authURL)
	}
	if opts.OpenBrowser {
		_ = OpenBrowser(authURL) // non-fatal
	}

	res, err := cs.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for callback: %w", err)
	}

	return exchangeCode(ctx, opts.HTTPClient, opts.TokenURL, res.Code, pkce.Verifier)
}

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: httpTimeoutSec * time.Second}
}

func tokenURL(override string) string {
	if override != "" {
		return override
	}
	return TokenURL
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func postJSON(ctx context.Context, c *http.Client, url string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, rb)
	}
	return rb, nil
}

func exchangeCode(ctx context.Context, c *http.Client, urlOverride, code, verifier string) (*Credentials, error) {
	rb, err := postJSON(ctx, httpClient(c), tokenURL(urlOverride), map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     ClientID(),
		"code":          code,
		"state":         verifier,
		"redirect_uri":  RedirectURI(),
		"code_verifier": verifier,
	})
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(rb, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return credentialsFromResponse(&tr), nil
}

// Refresh exchanges a refresh token for a new credential pair.
func Refresh(ctx context.Context, c *http.Client, urlOverride, refreshToken string) (*Credentials, error) {
	rb, err := postJSON(ctx, httpClient(c), tokenURL(urlOverride), map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     ClientID(),
		"refresh_token": refreshToken,
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(rb, &tr); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	return credentialsFromResponse(&tr), nil
}

func credentialsFromResponse(tr *tokenResponse) *Credentials {
	expires := time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second - expirySkew).UnixMilli()
	return &Credentials{
		Access:    tr.AccessToken,
		Refresh:   tr.RefreshToken,
		ExpiresMs: expires,
	}
}

// TokenSource yields valid Anthropic access tokens, refreshing them on demand
// and persisting the new credentials to disk. It is safe for concurrent use.
type TokenSource struct {
	path         string
	httpClient   *http.Client
	tokenURLOver string

	mu    sync.Mutex
	creds *Credentials
}

// NewTokenSource constructs a token source backed by the credentials file at
// path. The file is loaded lazily on first AccessToken call.
func NewTokenSource(path string) *TokenSource {
	return &TokenSource{path: path}
}

// AccessToken returns a valid access token, refreshing if it has expired
// (with a 5-minute skew). The refreshed credentials are persisted.
func (t *TokenSource) AccessToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.creds == nil {
		c, err := Load(t.path)
		if err != nil {
			return "", err
		}
		t.creds = c
	}

	if time.Now().UnixMilli() < t.creds.ExpiresMs {
		return t.creds.Access, nil
	}

	newCreds, err := Refresh(ctx, t.httpClient, t.tokenURLOver, t.creds.Refresh)
	if err != nil {
		return "", fmt.Errorf("refresh anthropic token: %w", err)
	}
	if err := Save(t.path, newCreds); err != nil {
		return "", fmt.Errorf("save refreshed credentials: %w", err)
	}
	t.creds = newCreds
	return t.creds.Access, nil
}
