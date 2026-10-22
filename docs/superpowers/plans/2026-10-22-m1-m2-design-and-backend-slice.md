# Luma M1 + M2: Design Foundation and Backend Vertical Slice — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Luma's shared design foundation with a rendered, critiqued preview, followed by a Go backend that
supports invite sign-up, login and sessions, blocking, direct conversations, and persistent real-time messaging
with gap-free replay. The backend is tested against real PostgreSQL.

**Architecture:**
- **Design:** `design/tokens.json` is the single source. A dependency-free Node generator emits per-platform
  constant files. A static preview page consumes the generated CSS, and Playwright screenshots it for critique.
- **Backend:** one Go binary (`server/`) using stdlib `net/http`, `pgx/v5` and `coder/websocket`, with embedded SQL
  migrations. Each message send is one database transaction (§11.2 of the spec). Fan-out goes through an in-process
  hub only after COMMIT.

**Tech Stack:**
- Node 22 (generator, contrast check, Playwright).
- Go 1.27.
- PostgreSQL 17 (Docker).
- `github.com/jackc/pgx/v5`, `github.com/coder/websocket`, `golang.org/x/crypto/argon2`.

**Spec:** `docs/superpowers/specs/2026-10-22-luma-mvp-design.md` (read §5, §7, §10, §11 before starting)

## Global Constraints
- Name "Luma" everywhere. Production URL `https://chat.folk3s.com`; server listens `127.0.0.1:8790` on the host.
- No Electron or webviews in clients. The preview page is a design-review surface only.
- Visual anti-goals: no gradients, glassmorphism, glow, emoji icons, decorative cards or unmotivated motion.
- All text/background token pairs must meet WCAG AA: 4.5:1 for body text, 3:1 for large text and UI glyphs.
- Usernames: `^[a-z0-9_]{3,24}$`, case-folded. Display names 1–64 characters. Message text ≤ 8000 characters. JSON body ≤ 64 KiB.
- Argon2id parameters: m=64 MiB, t=3, p=2, 16-byte salt, 32-byte key, PHC string. Password length 10–256.
- Session token: 32 random bytes, base64url. Only its SHA-256 is stored. Sliding expiry: 90 days of inactivity.
- Error shape: `{"error":{"code":"snake_case","message":"…"}}`.
- Never log tokens, passwords, invite codes or message bodies.
- Ack and fan-out happen only after COMMIT. Seq is gap-free per conversation, and retries never consume a seq.
- Luma is isolated: compose project `luma`, volumes `luma_*`, no touching other containers, volumes or Caddy blocks.
- Tests use a disposable Postgres container `luma-test-pg` on `127.0.0.1:55432`. It must never use a production volume.

## Review Focus
1. **Two devices of the same user** both connected: an ack goes to the sending device, and the other device gets the message via its own `user_events` stream. Test: `TestRealtime_SecondDeviceReceivesOwnMessage` (Task 9).
2. **Client retries a send after the server committed but before the ack arrived** (the connection drops): the retry must return the same message and seq. Test: `TestSend_RetryAfterLostAck` (Task 7).
3. **Blocked user tries a DM through WebSocket instead of REST:** it must be refused identically. Test: `TestRealtime_SendBlockedDenied` (Task 9).
4. **Revoked session with a live socket:** the socket closes within 1 s. Test: `TestRealtime_RevokeClosesSocket` (Task 9).
5. **Username lookup with different casing or surrounding whitespace** (`" Alice "`): it resolves to the same user. Neither registration nor lookup trims silently; leading/trailing whitespace is rejected at registration and trimmed at lookup. Test: `TestUsers_LookupNormalises` (Task 6).

---

## Milestone 1 — Design foundation

### Task 1: Token source and contrast gate

**Files:**
- Create: `design/tokens.json`, `design/tools/contrast.mjs`, `design/tools/contrast.test.mjs`, `design/package.json`

**Interfaces:**
- Produces: `tokens.json` schema:
  - `color.{light,dark}.<semanticName>` → hex string.
  - `type.<role>` → `{size, lineHeight, weight, tracking, family:"ui"|"mono"}`.
  - `space.<n>` → px.
  - `size.<name>` → px.
  - `radius.<tier>` → px.
  - `elevation.<level>` → `{y, blur, alpha}` (`dark` reuses `light` with different alpha).
  - `motion.duration.<name>` → ms. `motion.curve.<name>` → `[x1,y1,x2,y2]`. `motion.spring.<name>` → `{response, damping}`.
  - `contrastPairs` → `[[fgName,bgName,minRatio]]`.
- Produces: `contrast.mjs` exports `ratio(hexA, hexB): number` and `checkAll(tokens): Array<{theme,fg,bg,ratio,min,ok}>`.
  The CLI exits 1 if any pair fails.

- [ ] Step 1: Write `contrast.test.mjs` (node:test).
  - `ratio('#000000','#ffffff')` ≈ 21 (±0.01).
  - `ratio('#777777','#ffffff')` ≈ 4.48.
  - `checkAll` on the real tokens returns all `ok`.
- [ ] Step 2: `node --test design/tools/` → FAIL (module missing).
- [ ] Step 3: Implement `contrast.mjs` using WCAG 2.x relative luminance.
  Author `tokens.json` per spec §7 with these semantic color names (both themes):
  - Surfaces: `ground`, `surface`, `surfaceRaised`, `hairline`.
  - Text: `text`, `textSecondary`, `textTertiary`.
  - Accent: `accent` (Lumen fill), `onAccent`, `accentText` (accent used as text on `surface`).
  - Bubbles: `bubbleOut`, `onBubbleOut`, `bubbleIn`, `onBubbleIn`.
  - States: `danger`, `onDanger`, `success`, `focusRing`, `presence`, `unreadBadge`, `onUnreadBadge`.
  - Contrast pairs to list: every `on*`/text token against the surface(s) it sits on.
- [ ] Step 4: `node --test design/tools/ && node design/tools/contrast.mjs` → PASS, table printed.
- [ ] Step 5: Commit `feat(design): token source with WCAG contrast gate`.

### Task 2: Platform generator

**Files:**
- Create: `design/tools/generate.mjs`, `design/tools/generate.test.mjs`.
- Generated (committed):
  - `design/generated/luma-tokens.css`
  - `design/generated/LumaTokens.swift`
  - `design/generated/LumaTokens.kt`
  - `design/generated/LumaTokens.xaml`
  - `design/generated/LumaTokens.qml` (+ `qmldir`)

**Interfaces:**
- Consumes: `tokens.json` (Task 1).
- Produces, per platform:
  - CSS: custom properties `--luma-<group>-<name>` on `:root` (light), overridden under
    `@media (prefers-color-scheme: dark){:root:not([data-theme=light])}` and `:root[data-theme=dark]`.
  - Swift: `enum LumaTokens { enum Color { static let accent = LumaColor(light: 0x…, dark: 0x…) … } enum Space … enum Radius … enum Motion … }`.
    `LumaColor` is a tiny struct defined in the same file.
  - Kotlin: `object LumaTokens { object LightColors {…} object DarkColors {…} object Space {…} … }` with `Long` ARGB values. No Compose import, so it stays usable from pure Kotlin.
  - XAML: a `ResourceDictionary` with `ThemeDictionaries` `Light`/`Dark` holding `Color` + `SolidColorBrush` keys
    `Luma<Name>Color` / `Luma<Name>Brush`, and `x:Double` keys for space and radius.
  - QML: `pragma Singleton` `QtObject` with a `property bool dark` and color properties that switch on it.
- Every generated file starts with `// Generated from design/tokens.json — do not edit.` (XAML uses an XML comment).

- [ ] Step 1: Write the test. Run the generator into a temp dir and assert that each file exists and contains:
  - the light and dark `accent` hex,
  - `space` value 16,
  - the header line.
  Rerunning produces byte-identical output.
- [ ] Step 2: Run → FAIL.
- [ ] Step 3: Implement `generate.mjs [outDir]`. Use string templates only, no dependencies.
- [ ] Step 4: `node --test design/tools/` → PASS. Then `node design/tools/generate.mjs design/generated`.
- [ ] Step 5: Commit `feat(design): generate Swift/Kotlin/XAML/QML/CSS tokens`.

### Task 3: Design language document

**Files:**
- Create: `design/DESIGN.md`

Sections, each with concrete rules rather than adjectives:
1. Identity: "instrument calm" and the Lumen accent rule (the accent appears in at most 3 kinds of place per screen).
2. Typography: per-platform faces, mono-for-metadata rule, scale table.
3. Color: semantic roles, the light and dark philosophy, no pure black.
4. Space, sizing, radii, elevation.
5. Motion: every animation names the state change it explains. Durations and curves come from tokens. Reduced-motion fallbacks.
6. Iconography: platform icon sets plus the semantic icon map table (`send`, `attach`, `newChat`, `search`, `settings`,
   `back`, `more`, `read`, `delivered`, `pending`, `failed`, `block`, `mute`, `archive`, `group`) with the symbol names on each platform.
7. Components: conversation row, message bubble and grouping, composer, status line (connection state), avatar, unread badge, empty state.
   For each: anatomy, states, keyboard and screen-reader behavior.
8. Platform adaptations: iOS, macOS, Android, Windows, Linux, following spec §6.2. Each links its official guideline (HIG, M3, Fluent 2, KDE HIG).
9. Accessibility checklist.
10. References and what each one is used for.

- [ ] Step 1: Write it.
- [ ] Step 2: Verify every token name it mentions exists: `node -e` script greps the names against `tokens.json` → no misses.
- [ ] Step 3: Commit `docs(design): Luma design language`.

### Task 4: Rendered preview and critique loop

**Files:**
- Create: `design/preview/index.html` (links `../generated/luma-tokens.css`), `design/tools/shoot.mjs`
- Output: `design/preview/shots/*.png`

The preview shows, in both themes:
- iOS conversation list and chat at 390×844.
- Android list at 412×915.
- macOS three-pane at 1280×800.
- Windows list-detail at 1280×800.
- Linux three-pane at 1280×800.
- A states strip: empty, loading skeleton, reconnecting, offline, send failed.
- A component sheet.

Platform frames imitate each OS's structure (title bar and controls, navigation pattern) without copying screenshots.
Icons are inline SVG (Lucide-style strokes), never emoji. Fonts: Inter and Geist Mono from Google Fonts.

- [ ] Step 1: Write `shoot.mjs`. Playwright Chromium takes full-page screenshots in light and dark plus per-frame element
  screenshots (`[data-shot]` attributes) and writes them to `design/preview/shots/`.
- [ ] Step 2: Build `index.html`.
- [ ] Step 3: `node design/tools/shoot.mjs` → PNGs written. Open each PNG (Read tool) and inspect it.
- [ ] Step 4: Critique each shot against the anti-goals, `design/DESIGN.md` and the design-critique checklist.
  Record findings in `design/preview/CRITIQUE.md` as weakness → fix.
- [ ] Step 5: Apply the fixes. Re-shoot and re-inspect until no obvious weakness remains. Rerun the contrast gate if tokens changed.
- [ ] Step 6: Commit `feat(design): rendered platform preview + critique`. Publish the preview as a private Artifact for owner review.

---

## Milestone 2 — Backend vertical slice

Package layout (`server/`):

| Path | Responsibility |
|---|---|
| `cmd/luma-server/main.go` | Parses config, runs migrations, serves HTTP, `admin` subcommands. |
| `internal/config` | Reads env (spec §5.2). |
| `internal/db` | Pool, migrator, embedded `migrations/*.sql`. |
| `internal/auth` | Password hashing, tokens, sessions, invites, middleware. |
| `internal/users` | Profiles, lookup, blocks. |
| `internal/chat` | Conversations, membership, send transaction, history, watermarks, user_events. |
| `internal/realtime` | Hub, WS protocol. |
| `internal/httpx` | Error writer, JSON decode with limits, rate limiter, request logging, health. |
| `internal/testdb` | Per-test fresh database cloned from a migrated template. |

### Task 5: Module, config, migrator, schema, test DB harness

**Files:**
- Create:
  - `server/go.mod` (module `luma.folk3s.com/server`)
  - `server/internal/config/config.go`
  - `server/internal/db/{db.go,migrate.go,migrations/0001_init.sql}`
  - `server/internal/testdb/testdb.go`
  - `server/internal/db/migrate_test.go`
  - `server/Makefile` (targets: `testdb-up`, `test`)

**Interfaces:**
- Produces:
  - `config.Load() (Config, error)`
  - `db.Open(ctx, url) (*pgxpool.Pool, error)`
  - `db.Migrate(ctx, pool) (version int, err error)`
  - `testdb.New(t *testing.T) *pgxpool.Pool`: creates a database from the template `luma_template` (migrated once per `go test` process) and drops it on cleanup.
- The `0001_init.sql` schema covers every slice table from spec §5.4 and §10:
  - `users` (+ `event_seq bigint not null default 0`, `disabled_at`, `last_seen_at`)
  - `sessions`
  - `invites`
  - `blocks(blocker_id, blocked_id, created_at, PK both)`
  - `conversations`
  - `direct_pairs`
  - `members` (+ `joined_seq`, `left_seq`)
  - `messages` (+ generated `tsv`)
  - `user_events`
  - `files`
- `schema_migrations(version int primary key, applied_at)`. Migration runs inside one transaction holding `pg_advisory_xact_lock(7311)`.
  The header comment documents the rollback (`-- rollback: DROP SCHEMA public CASCADE; restore from backup`).

- [ ] Step 1: `make testdb-up` (runs `docker run -d --name luma-test-pg -p 127.0.0.1:55432:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine` if it's not already running).
- [ ] Step 2: Test `TestMigrate_Idempotent`: run `Migrate` twice. Version 1 both times, and the tables exist.
  Test `TestMigrate_ConcurrentSafe`: 5 goroutines migrate at once, with no error.
- [ ] Step 3: `go test ./internal/db/` → FAIL.
- [ ] Step 4: Implement.
- [ ] Step 5: Tests PASS. Commit `feat(server): config, migrations, schema`.

### Task 6: Auth, sessions, invites, users, blocks (REST)

**Files:**
- Create:
  - `server/internal/httpx/{errors.go,json.go,ratelimit.go,log.go}`
  - `server/internal/auth/{password.go,token.go,service.go,handlers.go,middleware.go}`
  - `server/internal/users/{service.go,handlers.go}`
  - `server/internal/api/router.go`
  - Tests next to each file, plus `server/internal/api/authz_test.go`

**Interfaces:**
- Produces:
  - `auth.HashPassword(pw string) (string, error)`
  - `auth.VerifyPassword(phc, pw string) (bool, error)`
  - `auth.DummyVerify()`
  - `auth.NewToken() (plain string, hash []byte)`
  - `auth.Service{Register, Login, Logout, ListSessions, RevokeSession, RevokeOthers, CreateInvite}`
  - `auth.Middleware(svc) func(http.Handler) http.Handler`, which puts `auth.Principal{UserID, SessionID uuid.UUID}` in the context
  - `auth.FromContext(ctx) (Principal, bool)`
  - `auth.Service.OnRevoke func(sessionID uuid.UUID)` (hook used by realtime in Task 9)
  - `users.Service{Me, UpdateMe, Lookup(ctx, viewer, username), Block, Unblock}`
  - SQL helper `users.BlockedEitherWaySQL` (`EXISTS(... blocker=$a AND blocked=$b OR blocker=$b AND blocked=$a)`), reused by chat
  - `httpx.Limiter.Allow(key string) (ok bool, retryAfter time.Duration)`
  - `httpx.WriteError(w, status, code, msg)`
- Routes:
  - `POST /v1/auth/register {invite, username, display_name, password, device_name, platform}` → 201 `{token, user}`
  - `POST /v1/auth/login` → 200 `{token, user}`
  - `POST /v1/auth/logout`
  - `GET /v1/auth/sessions`
  - `DELETE /v1/auth/sessions/{id}`
  - `POST /v1/auth/sessions/revoke-others`
  - `POST /v1/invites` → `{code, expires_at}`
  - `GET /v1/me`, `PATCH /v1/me`
  - `GET /v1/users/{username}`
  - `POST/DELETE /v1/users/{username}/block`
  - `GET /v1/health/live`, `GET /v1/health/ready`
- Admin CLI: `luma-server admin invite`, `admin disable-user <username>` (revokes all sessions).

- [ ] Step 1: Write the tests:
  - `TestPassword_RoundTrip`. PHC prefix `$argon2id$v=19$m=65536,t=3,p=2$`.
  - `TestRegister_RequiresValidUnusedInvite`: the second use → 400 `invalid_invite`.
  - `TestRegister_UsernameRules`: `"Al"`, `"alice!"`, `" alice"` → 400 `invalid_username`. `"Alice"` is stored as `alice`.
  - `TestLogin_EnumerationSafe`: an unknown user and a wrong password give the same status and code, and the mean durations of 5 runs each are within 30%.
  - `TestLogin_RateLimited`: the 11th attempt within a minute from one IP → 429 with `Retry-After`.
  - `TestSessions_RevokeAndPasswordChange`: a revoked token → 401. A password change revokes other sessions but keeps the current one.
  - `TestSession_SlidingExpiry`: `last_seen_at` older than 90 days → 401.
  - `TestUsers_LookupNormalises`: `" Alice "` (URL-escaped) finds `alice`.
  - `TestUsers_LookupHiddenFromBlocked`: if B blocks A, then A's lookup of B → 404, the same body as a nonexistent user.
  - `TestDisabledUser_CannotLogin`.
  - `TestLogs_NoSecrets`: capture slog output during register and login, and assert it contains neither the password, the token nor the invite.
- [ ] Step 2: `go test ./...` → FAIL.
- [ ] Step 3: Implement. Every handler decodes through `httpx.DecodeJSON(w, r, &v)`, which caps at 64 KiB and rejects unknown fields.
  The client IP comes from `CF-Connecting-IP` only when the remote address is loopback and `LUMA_TRUSTED_PROXY=1`.
- [ ] Step 4: PASS. Commit `feat(server): auth, sessions, invites, users, blocking`.

### Task 7: Conversations and the send transaction

**Files:**
- Create:
  - `server/internal/chat/{service.go,send.go,events.go,handlers.go}`
  - `server/internal/chat/send_test.go`, `server/internal/chat/service_test.go`

**Interfaces:**
- Consumes: `users.BlockedEitherWaySQL`, `auth.FromContext`.
- Produces:
  - `chat.Service.OpenDirect(ctx, me, otherUsername) (Conversation, error)`. It's idempotent via `direct_pairs`, and returns `ErrNotFound` when blocked either way.
  - `chat.Service.List(ctx, me) ([]ConversationSummary, error)`, where `ConversationSummary{Conversation, LastMessage *Message, Unread int64}` is ordered by last activity.
  - `chat.Service.Send(ctx, me, SendInput{ConversationID, ClientID uuid.UUID, Kind, Body string, Meta json.RawMessage}) (Message, created bool, err error)`
  - `chat.Service.History(ctx, me, convID, beforeSeq, afterSeq int64, limit int) ([]Message, error)`. It's clamped to the member's visibility window, with limit ≤ 100.
  - `chat.Service.MarkRead(ctx, me, convID, seq)` and `MarkDelivered(ctx, me, convID, seq)`
  - `chat.Service.EventsAfter(ctx, userID, cursor int64, limit int) ([]Event, error)`
  - `chat.Service.OldestCursor(ctx, userID) int64`
  - `chat.Notifier` interface `{ Committed(events []Event) }`, called after COMMIT. The realtime hub implements it.
  - `Event{UserID uuid.UUID; Seq int64; Type string; Payload json.RawMessage}`, with types `message.created`, `conversation.created`, `read.updated`, `receipt.updated`.
- Send algorithm: the exact order in spec §11.2.
  - Lock `users` rows for event_seq with `SELECT … FOR UPDATE` in the order `ORDER BY id`.
  - Look up conflicts first: `SELECT … WHERE sender_id=$1 AND client_id=$2`.
  - Then run the insert. A concurrent duplicate is caught by the unique violation (`23505`). In that case, roll back, re-select and return `created=false`.
- Routes:
  - `POST /v1/conversations {kind:"direct", username}`
  - `GET /v1/conversations`
  - `GET /v1/conversations/{id}/messages?before_seq=&after_seq=&limit=`
  - `POST /v1/conversations/{id}/messages {client_id, kind:"text", body}` → 201 (new) or 200 (dup)
  - `POST /v1/conversations/{id}/read {seq}`

- [ ] Step 1: Write the tests:
  - `TestSend_ConcurrentGapFree`: 50 goroutines × 4 sends across 2 users in one DM. Seqs are exactly 1..200, and every user's `user_events` seqs are contiguous.
  - `TestSend_DuplicateClientID`: the same `client_id` sent twice sequentially, and 10× concurrently. The result is one row, the same seq, and `created=false` for the rest. `conversations.last_seq` is unchanged.
  - `TestSend_RetryAfterLostAck`: send, discard the result, send again. The message is identical.
  - `TestSend_NonMemberDenied`: `ErrNotFound` (404, so existence doesn't leak).
  - `TestSend_BlockedDMDenied`: B blocks A after the DM exists. A's send → 403 `blocked`, and B's send → 403 `blocked`.
  - `TestOpenDirect_Idempotent`: A→B and B→A open the same conversation.
  - `TestOpenDirect_BlockedLooksMissing`: 404.
  - `TestSend_Validation`: an empty body, more than 8000 characters, or a bad kind → 400.
  - `TestMarkRead_Monotonic`: 5 then 3 → 5 is kept. 999 is clamped to `last_seq`.
  - `TestList_UnreadCounts`.
  - `TestHistory_Paging`: `before_seq` and `after_seq` boundaries are exclusive.
  - `TestSend_NoAckBeforeCommit`: a fake Notifier records calls. Inject a failure via the test hook `chat.failBeforeCommit`. The Notifier is not called, the error is returned, and no row exists.
- [ ] Step 2: FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: PASS with `go test -race ./internal/chat/`. Commit `feat(server): conversations and transactional send`.

### Task 8: Rate limits and authz matrix for the slice

**Files:**
- Modify: `server/internal/api/router.go`
- Create: `server/internal/api/authz_matrix.json`, extend `authz_test.go`

**Interfaces:**
- Matrix rows: `{route, method, as: "anon"|"member"|"nonmember"|"blocked"|"self", expect: status}`. The test harness builds a world with users `alice`, `bob`, `carol` and `mallory`, where mallory is blocked by alice. The world has a DM alice↔bob.

- [ ] Step 1: Write the matrix covering every Task 6–7 route. `TestAuthzMatrix` iterates the matrix and asserts the status for each row.
- [ ] Step 2: Make it pass. Apply limits: messages 30 per 10 s per user; new-account DM openings 20/day; auth 10/min/IP.
  Add `TestSend_RateLimited`.
- [ ] Step 3: Commit `test(server): authorization matrix + rate limits`.

### Task 9: Realtime

**Files:**
- Create: `server/internal/realtime/{hub.go,conn.go,frames.go}`, `server/internal/realtime/realtime_test.go`

**Interfaces:**
- Consumes:
  - `chat.Service` (`Send`, `MarkRead`, `MarkDelivered`, `EventsAfter`, `OldestCursor`).
  - `chat.Notifier`, `auth.Service.OnRevoke`, `users.BlockedEitherWaySQL`.
- Produces:
  - `realtime.Hub` (implements `chat.Notifier`), with methods `Register(conn)`, `Unregister(conn)`, `CloseSession(sessionID)` and `PresenceOf(userID) (online bool, lastSeen time.Time)`.
  - Route `GET /v1/realtime`.
- Frames: exactly as in spec §5.6.
  - The `hello` frame must arrive within 10 s. Auth comes from the header or from `hello.token`.
  - Replay pages through `EventsAfter` in batches of 500 while live events queue in a per-connection buffer (cap 1024; overflow closes with code 4008 so the client reconnects and replays).
  - Live events with `seq <= lastReplayed` are dropped.
  - Typing and presence go only to users who share a conversation and aren't blocked either way.
  - Server ping 25 s. A connection with no pong for 60 s is closed.

- [ ] Step 1: Write the tests (use `httptest.Server` + `coder/websocket` client):
  - `TestRealtime_SendAckAfterCommit`: the ack carries the message with its seq, and the peer receives `event message.created`.
  - `TestRealtime_SecondDeviceReceivesOwnMessage`.
  - `TestRealtime_ReplayMissedEvents`: disconnect B, send 5 messages, reconnect B with the old cursor. B receives exactly those 5 events in order, then live events.
  - `TestRealtime_ReplayLiveRace`: messages sent during replay are delivered exactly once.
  - `TestRealtime_RestartDurability`: start server 1, send 20 messages, close it abruptly. Start server 2 on the same DB, reconnect with cursor 0. All 20 events arrive, and the DB message count matches the acks received.
  - `TestRealtime_DuplicateSendAcksSameMessage`.
  - `TestRealtime_SendBlockedDenied`: `error{code:"blocked"}`.
  - `TestRealtime_RevokeClosesSocket`: closed within 1 s.
  - `TestRealtime_HelloTimeout`.
  - `TestRealtime_TypingNotSentToBlocked`.
  - `TestRealtime_PresenceOnlyToPartners`: carol, who shares no conversation, gets no presence for alice.
  - `TestRealtime_StaleCursorResync`: cursor below `OldestCursor` → `resync_required`.
- [ ] Step 2: FAIL.
- [ ] Step 3: Implement.
- [ ] Step 4: `go test -race ./...` PASS. Commit `feat(server): realtime protocol with replay`.

### Task 10: Contract docs and slice smoke test

**Files:**
- Create:
  - `protocol/openapi.yaml` (the slice routes, schemas and error codes)
  - `protocol/realtime.md`
  - `protocol/vectors/realtime-basic.json` (hello → ready → send → ack → event sequence)
  - `server/internal/api/contract_test.go`
  - `server/Dockerfile` (multi-stage, distroless, non-root)

- [ ] Step 1: `TestContract_RoutesDocumented` loads `openapi.yaml`, a small YAML subset parsed with `gopkg.in/yaml.v3`. Every route registered in the router appears in it, and every documented route is registered. `TestContract_Vectors` replays `realtime-basic.json` against a live test server, matching frame types and fields.
- [ ] Step 2: Make them pass.
- [ ] Step 3: `docker build -t luma-server:dev server/` succeeds. Run it against the test DB, then:
  `curl -s localhost:8790/v1/health/ready` → `{"status":"ok","migration":1}`.
- [ ] Step 4: Commit `docs(protocol): OpenAPI + realtime contract, contract tests`.

Deferred to the M3/M4 plans:
- Groups, files, search, presence `last_seen` REST, deploy/backup/runbook, threat-model doc. Threat-model work starts in M4 using the M2 code.
- The Linux client.
