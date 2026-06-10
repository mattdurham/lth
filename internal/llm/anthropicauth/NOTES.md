# internal/llm/anthropicauth — Design Notes

## 1. Why claude.ai OAuth Instead of API Keys

*Added: 2026-06-10*

**Decision:** Implement the same OAuth 2.0 + PKCE flow that Claude Code uses,
hitting `https://claude.ai/oauth/authorize` and `https://platform.claude.com/v1/oauth/token`.

**Rationale:** Lets users on a Claude Pro/Max subscription run lth without
paying for API credits. The flow, client ID, scopes, and redirect URI all
mirror Claude Code so the resulting tokens are accepted by Anthropic's
OAuth-gated Messages API path.

**Consequence:**
- API-key callers see no behaviour change; OAuth is opt-in via
  `llm.auth_mode: oauth` in the lth config.
- Using a third-party tool with claude.ai OAuth is in a gray area w.r.t.
  Anthropic's ToS — users opt in explicitly.

---

## 2. Loopback Callback Server

*Added: 2026-06-10*

**Decision:** Run a single-shot `net/http` server on `127.0.0.1:53692` to
receive the OAuth redirect, rather than asking users to paste the code.

**Rationale:** Standard PKCE pattern. The port is fixed (matches Claude Code
and pi) so the redirect URI registered with the IdP stays stable. The state
parameter is set to the PKCE verifier — the same trick pi uses — so the
callback server can validate the redirect with no extra storage.

**Consequence:** If port 53692 is already bound, `Login` fails immediately
with the bind error. Users on multi-machine setups (e.g. SSH) can fall back
to manual paste in a future iteration; for now `lth auth login` assumes the
browser is on the same host.

---

## 3. Token Storage on Disk, Not Keychain

*Added: 2026-06-10*

**Decision:** Persist credentials as plain JSON at `~/.lth/anthropic-oauth.json`
with file mode `0600`.

**Rationale:** Matches Claude Code's own storage model. Avoids pulling in a
keychain dependency (`zalando/go-keyring` etc.) that fails on headless Linux
boxes. The lth config dir is already where the SQLite DB and other secrets
live.

**Consequence:** A user with read access to the home directory can read the
tokens. This is acceptable for a developer tool with the same threat model
as `.aws/credentials` or `.npmrc`.

---

## 4. Lazy Refresh Under a Mutex

*Added: 2026-06-10*

**Decision:** `TokenSource.AccessToken` loads the file lazily on first call,
checks `ExpiresMs` against `time.Now()`, and refreshes inline (holding the
mutex) when expired.

**Rationale:** Simple and correct. The mutex prevents two goroutines from
each kicking off a refresh when the daemon's compaction and chat code paths
fire concurrently. Refreshed credentials are written to disk before the
mutex is released so a crash mid-refresh either keeps the old creds (if the
write hasn't happened) or has the new ones fully durable.

**Consequence:** A long refresh blocks other Anthropic calls on the same
`TokenSource`. The 30s token-endpoint timeout caps this in the worst case.
