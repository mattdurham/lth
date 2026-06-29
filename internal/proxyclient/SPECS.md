# internal/proxyclient — Invariants

## Overview

`proxyclient.Client` is an HTTP client that forwards lth operations to a running
daemon's `/api/v1/` REST API. It satisfies the same `MemClient` interface as
`*lth.Client` so CLI commands work transparently in proxy mode.

## Invariants

1. **JSON always used on the wire.** All requests send `Accept: application/json` and
   `Content-Type: application/json`. The daemon's Markdown-first default is bypassed.

2. **30-second timeout.** The underlying `http.Client` has a 30-second timeout.
   Individual callers may use a shorter context deadline.

3. **No local DB.** `proxyclient.Client` never opens a SQLite connection. All data
   flows through HTTP to the remote daemon.

4. **Stateless.** `Close()` is a no-op. No connection pooling beyond the standard
   `http.Client` transport.

5. **Error propagation.** HTTP 4xx/5xx responses are converted to Go errors. The error
   message includes the HTTP status code and the `error` field from the JSON body if
   present.

6. **`GraphPPR` top=50.** The proxy always requests the top 50 PPR nodes and returns
   the full map; callers can slice further. This matches the server's default.
