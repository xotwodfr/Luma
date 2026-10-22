# Luma realtime protocol (v1)

`GET wss://chat.folk3s.com/v1/realtime`, upgraded to a WebSocket. Every frame is a UTF-8 JSON text
message of at most 128 KiB:

```json
{ "t": "<type>", "id": "<optional request id>", "d": { … } }
```

`id` is chosen by the client for request frames and echoed in the matching `ack` or `error`.

## Connecting

1. Open the socket. If the platform can set handshake headers, send `Authorization: Bearer <token>`.
   An invalid header token is rejected with HTTP 401 before the upgrade.
2. The first frame **must** be `hello` and arrive within 10 s:
   `{"t":"hello","d":{"token":"<token, if not in the header>","cursor":<last applied event seq>}}`.
   A fresh install with no local state sends `"cursor": null`. It gets `resync_required` with the current
   cursor (instead of replaying the whole account history), loads state over REST, and continues from there.
3. The server replays every stored event after `cursor` as `event` frames, in order, then sends
   `{"t":"ready","d":{"cursor":<n>,"user":{"id","username","display_name"}}}`.
4. Presence frames for current conversation partners follow, then live traffic.

If `cursor` is older than the server's retained history (30 days) or newer than anything the server
knows about, the server sends `{"t":"resync_required","d":{"cursor":<current>}}` instead of replaying.
The client must reload conversations and history over REST, then continue from that cursor.

Close codes: `4001` unauthorized or no hello in time · `4002` first frame was not hello ·
`4003` session revoked (sign out locally) · `4008` client fell more than 1024 frames behind
(reconnect and replay) · `1001` server restarting (reconnect and replay).

## Server → client

| t | d | Notes |
|---|---|---|
| `ready` | `{cursor, user}` | Replay finished. |
| `event` | `{seq, type, data}` | Durable. Apply in order, then persist `seq` as your cursor. |
| `ack` | `send`: `{message}` · `read`: `{last_read_seq}` · `delivered`: `{last_delivered_seq}` | Sent only after the database commit. |
| `error` | `{code, message}` | Same codes as REST (`blocked`, `invalid_message`, `not_found`, `rate_limited`, …). |
| `typing` | `{conversation_id, user_id}` | Ephemeral. Show for 5 s unless refreshed. |
| `presence` | `{user_id, online, last_seen}` | Only for people you share a conversation with and haven't blocked (either way). |
| `pong` | — | Reply to `ping`. |
| `resync_required` | `{cursor}` | See above. |

### Event types

| type | data |
|---|---|
| `conversation.created` | `{conversation_id}`. Fetch it with `GET /v1/conversations/{id}`. |
| `message.created` | `{conversation_id, message}`. Also sent to the sender's own devices. |
| `read.updated` | `{conversation_id, user_id, last_read_seq, last_delivered_seq}`. Your read position moved on another device. |
| `receipt.updated` | Same shape: another member's read or delivered watermark moved. |

## Client → server

| t | d | Reply |
|---|---|---|
| `send` | `{conversation_id, client_id, kind: "text", body}` | `ack {message}` or `error` |
| `read` | `{conversation_id, seq}` | `ack {last_read_seq}` |
| `delivered` | `{conversation_id, seq}` | `ack {last_delivered_seq}` |
| `typing` | `{conversation_id}` | none. Send at most once every 3 s while typing. |
| `ping` | — | `pong` |

## Delivery guarantees

- **Durable before acknowledged.** `ack` (and the REST 201) is sent only after the transaction that
  stores the message, its seq and every recipient's event has committed.
- **Ordering.** Messages are totally ordered per conversation by `seq` (1, 2, 3… with no gaps). There
  is no ordering across conversations.
- **At least once, effectively once.** Each device receives every event at least once. Dedupe events
  by `seq` (keep your cursor) and messages by `(conversation_id, seq)` or `id`.
- **Idempotent sends.** Generate `client_id` (a UUID) once per message, store it in your outbox, and
  reuse it on every retry. A retry returns the original message, even across reconnects and server
  restarts. Reusing a `client_id` for a different conversation is rejected with `client_id_conflict`.
- **Watermarks are monotonic.** `read`/`delivered` never move backwards and are clamped to what you
  can see, so they are safe to resend.

## Client obligations

- Keep a persistent outbox keyed by `client_id`. On reconnect, after `ready`, resend every pending item.
- Persist the cursor only after the event has been applied to local storage.
- Reconnect with exponential backoff from 0.5 s, doubling to a 30 s cap, with full jitter. Reconnect
  immediately when the OS reports that the network came back.
- Send `ping` every 25 s when idle. Cloudflare closes idle sockets at 100 s, and the server closes
  sockets that don't answer its pings within about 60 s.

Golden examples live in `protocol/vectors/`. Clients should run them against their parsers.
