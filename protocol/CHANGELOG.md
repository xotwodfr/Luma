# Protocol changelog

Mismatches found while building native clients against the server, and how each was resolved.

## 2026-10-22 — Linux client (M3)

- **Revocation close code was not delivered.** `realtime.md` promises close code `4003` on revocation,
  but the server cancelled the connection context before writing the close frame, so clients saw an
  abnormal closure and kept reconnecting. Fixed in `server/internal/realtime/conn.go` (close frame
  first, then cancel); `TestRealtime_RevokeClosesSocket` now asserts the code.
- **A revoked token is indistinguishable from a network failure on reconnect.** The handshake is
  refused with HTTP 401, which WebSocket APIs surface as a generic failure. Documented client
  obligation: after a failed connect, check `GET /v1/me` (throttled); a 401 means sign out.
