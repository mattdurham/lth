# internal/llm/anthropicauth — Invariants

1. `GeneratePKCE` returns a verifier that is exactly the RFC 7636-compliant base64url(no-pad) encoding of 32 random bytes, and a challenge that is `base64url(no-pad)(SHA256(verifier))`.
2. `ClientID()` returns the same public OAuth client ID Claude Code ships (`9d1c250a-e61b-44d9-88ed-5944d1962f5e`). It is not a secret; the base64 obfuscation only prevents trivial code search hits.
3. `Login` blocks until either: (a) the loopback callback at `127.0.0.1:53692/callback` receives a `code` whose `state` equals the PKCE verifier, (b) the context is cancelled, or (c) the user closes the OAuth page with an `error` query param.
4. `Login` returns credentials with `ExpiresMs = now + expires_in*1000 - 5*60*1000` (5-minute skew).
5. `Refresh` POSTs `grant_type=refresh_token` to the token URL with the same client ID; on success, the server-returned refresh token replaces the old one.
6. `Save` writes the credentials JSON with file mode `0600`. `Load` returns `ErrNoCredentials` (sentinel) if the file does not exist, and a non-sentinel error if it exists but is unparseable or missing required fields.
7. `TokenSource.AccessToken` is safe for concurrent use; only one refresh is in flight at a time per `TokenSource`. The refreshed credentials are persisted to disk before being returned.
8. The package never logs the access or refresh tokens.
