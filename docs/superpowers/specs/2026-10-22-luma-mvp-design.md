# Luma — MVP Design

Status: draft for approval · 2026-10-22

## 1. Intent

Luma is a messaging app whose point is not a new paradigm but **everyday communication that feels
exceptionally well made**: calm, fast, personal, intentional; premium and slightly futuristic.
It is part of the Auralis ecosystem but fully independent (own backend, accounts, release cycle).

Production endpoint: `https://chat.folk3s.com`.

**One shared design foundation, not one universal UI.** Every platform shares identity, tokens,
terminology, protocol and behavioural rules; every platform gets its own native interface.

### Goals (MVP)
- Invite-based sign-up, login, secure revocable sessions.
- Profiles: display name, unique username, avatar; find people by username.
- 1:1 and group conversations, real-time, persistent history.
- Delivery/read state, typing indicators, online presence, unread counts.
- Image and file sharing.
- Conversation management: list ordered by activity, mute, leave group, archive.
- Message search.
- Notifications (desktop via live connection; mobile push is post-MVP, see §8).
- Designed empty / loading / offline / reconnecting / error states on every client.

### Non-goals (MVP)
End-to-end encryption, voice/video calls, voice messages, reactions, replies, edits, deletes-for-all,
stickers, bots, federation, web client, multi-region scaling. The data model must allow
reactions, replies, edits and voice messages to be added without rewriting the schema (§5.4).

### Anti-goals (failure even if it works)
- Looks like a Telegram/Discord/WhatsApp clone or a generic AI dashboard.
- Electron, webview shells, or one cross-platform UI toolkit forced onto every OS.
- Gradients, glassmorphism, glow, emoji-as-icons, decorative cards, motion without meaning.
- A message the user sent silently disappears, duplicates, or reorders.
- Claiming a build, security property or visual result that was not verified.

## 2. Decisions taken with the owner
| Decision | Choice |
|---|---|
| Encryption | TLS in transit + encrypted-at-rest disk; server can read messages. Envelope shape is E2EE-ready (§5.4). |
| Shared code | Share contracts, not runtime code: design tokens, OpenAPI, realtime schemas, golden test vectors, generated constants. Each client is fully native. |
| Registration | Single-use invite codes. Config flag `LUMA_REGISTRATION=invite|open|closed`, default `invite`. |
| Platforms | iOS + macOS: Swift/SwiftUI. Android: Kotlin/Compose. Windows: C#/WinUI 3. Linux: Qt 6 Quick + C++. |
| No Electron/webview | Hard constraint. |

## 3. Environment facts (verified 2026-10-22)
- This machine **is** the production host. Ingress: Cloudflare Tunnel (`cloudflared-folk3s`,
  `/etc/cloudflared/config.yml` already maps `chat.folk3s.com → localhost:80`) → host Caddy
  (`/etc/caddy/Caddyfile` already has `http://chat.folk3s.com { reverse_proxy 127.0.0.1:8790 }`) →
  nothing listening (HTTP 502). The old chat backend is gone.
- Many unrelated services and databases run here (Auralis, Authentik, Immich, Nextcloud, …).
  Luma must not touch their containers, volumes, networks, ports or secrets.
- Toolchains: Go 1.27, Rust 1.98, Node 22, .NET 10, JDK 17, Docker 29, Caddy 2.11, Android SDK 36 +
  NDK (no emulator image installed; `/dev/kvm` present), Qt 6 base+declarative (missing cmake,
  ninja, qt6-websockets, qt6-svg), no display server, no Swift, no Windows SDK.

### Verification matrix
| Target | Build here | Automated tests here | Visual review here |
|---|---|---|---|
| Server | yes | yes (real Postgres in Docker) | n/a |
| Linux (Qt) | after `pacman -S cmake ninja qt6-websockets qt6-svg qt6-shadertools` | yes (QtTest) | yes, `QT_QPA_PLATFORM=offscreen` screenshots |
| Android | yes (Gradle wrapper) | JVM unit tests | yes, Roborazzi JVM screenshots; emulator optional (KVM present) |
| iOS / macOS | no | no | no — needs macOS (GitHub Actions `macos-*` runner provided) |
| Windows | no | no | no — needs Windows (GitHub Actions `windows-*` runner provided) |

Every milestone report states per target whether it was built, tested, and screenshotted.

## 4. Repository layout
```
design/     tokens.json (source of truth), gen/ (generator → Swift, Kotlin, XAML, QML), DESIGN.md, preview/
protocol/   openapi.yaml, realtime.md, schemas/*.json, vectors/*.json
server/     Go module (cmd/luma-server, internal/…, migrations/*.sql embedded)
deploy/     compose.yaml, .env.example, backup script + systemd timer, RUNBOOK.md
linux/      Qt 6 Quick app (CMake)
android/    Gradle project (Compose)
apple/      Xcode project: LumaKit Swift package (networking, models, sync) + LumaiOS + LumaMac targets
windows/    WinUI 3 solution (Luma.Core class library + Luma.App)
.github/workflows/  server, linux, android, apple, windows builds
```
Generated token files are committed so each platform builds without running the generator.

## 5. Backend

### 5.1 Stack
Go, stdlib `net/http` routing, `pgx/v5` (pool), `coder/websocket`, `golang.org/x/crypto/argon2`,
SQL migrations embedded in the binary and applied at startup inside a transaction with an advisory
lock (forward-only, numbered `NNNN_name.sql`, tracked in `schema_migrations`). Structured logs via
`log/slog` (JSON). Single binary, distroless image.

### 5.2 Configuration (env)
`LUMA_DATABASE_URL`, `LUMA_LISTEN` (default `127.0.0.1:8790` on host; `:8790` in container),
`LUMA_PUBLIC_URL`, `LUMA_UPLOAD_DIR`, `LUMA_MAX_UPLOAD_BYTES` (default 25 MiB),
`LUMA_REGISTRATION`, `LUMA_TRUSTED_PROXY` (to read `CF-Connecting-IP` / `X-Forwarded-For` only from Caddy).
Secrets live in `deploy/.env` (mode 600, gitignored); `.env.example` committed. The deploy step
**never overwrites an existing `.env`**.

### 5.3 Auth & security
- Passwords: Argon2id (m=64 MiB, t=3, p=2, 16-byte salt), PHC-encoded string; min length 10, max 256.
- Sessions: 32 random bytes, base64url, returned once; DB stores SHA-256 hash, device name, platform,
  created/last-seen, IP. Sent as `Authorization: Bearer`. Sliding expiry 90 days of inactivity.
  Endpoints to list and revoke sessions (single, all-others). Revocation closes that session's sockets immediately.
- Invite codes: random, single-use, optional expiry, created by any user (limit 5 outstanding) or admin CLI
  (`luma-server admin invite`, `admin create-user`).
- Authorization: every conversation-scoped query checks membership in SQL, not after fetching.
- Rate limits (token bucket, in-memory, per IP and per user): auth 10/min/IP, messages 30/10s/user,
  uploads 20/min/user, search 30/min/user. 429 with `Retry-After`.
- Validation at the boundary: JSON body cap 64 KiB, message text ≤ 8 000 chars, usernames
  `^[a-z0-9_]{3,24}$` (case-folded), display names 1–64 chars, group titles 1–80, groups ≤ 256 members.
- Uploads: streamed to disk under random IDs, size-capped, MIME sniffed (not trusted from client),
  images get width/height recorded; served only to conversation members via `/v1/files/{id}`
  with `Content-Disposition` and `X-Content-Type-Options: nosniff`. No SVG/HTML rendering inline.
- Errors: uniform `{ "error": { "code": "snake_case", "message": "…" } }`; no stack traces or SQL leaks.
- Logging: request id, route, status, latency, user id; never tokens, passwords or message bodies.

### 5.4 Data model (initial)
`users(id uuid, username citext unique, display_name, avatar_file_id, password_hash, created_at)`
`sessions(id, user_id, token_hash unique, device_name, platform, created_at, last_seen_at, revoked_at)`
`invites(code_hash, created_by, used_by, expires_at, used_at)`
`conversations(id, kind 'direct'|'group', title, avatar_file_id, created_by, created_at, last_seq bigint)`
`direct_pairs(user_a, user_b, conversation_id)` — unique ordered pair → one DM per pair.
`members(conversation_id, user_id, role 'owner'|'member', joined_at, left_at, last_read_seq, last_delivered_seq, muted_until, archived)`
`messages(id uuid, conversation_id, seq bigint, sender_id, client_id uuid, kind, body text, meta jsonb, created_at, edited_at, deleted_at)`
  - unique `(conversation_id, seq)` and `(sender_id, client_id)` (idempotent send).
  - `kind`: `text`, `file`, `system` now; `voice`, … later. `meta` carries attachment refs now,
    `reply_to`, `reactions` etc. later; `edited_at`/`deleted_at` reserved.
  - `body` + `meta` are treated as an opaque *envelope* by delivery code, so a later E2EE mode can
    replace them with ciphertext without touching sequencing, receipts or fan-out.
  - Full-text: generated `tsvector` column (`simple` config) + GIN index.
`files(id, owner_id, conversation_id, name, mime, size, width, height, sha256, created_at)`
`user_events(user_id, seq bigint, type, payload jsonb, created_at)` — per-user durable event log
  (new message, membership change, read-state change, profile change) used for gap-free replay;
  pruned after 30 days (clients older than that do a full resync).

Sequence assignment: `UPDATE conversations SET last_seq = last_seq + 1 … RETURNING last_seq` inside
the insert transaction (row lock serialises per conversation, not globally).

### 5.5 API
REST under `/v1`, documented in `protocol/openapi.yaml`:
auth (`register`, `login`, `logout`, `sessions`, `sessions/{id}` DELETE), `me` (GET/PATCH, avatar),
`invites`, `users/{username}`, `users?query=` (prefix search on username/display name),
`conversations` (list with unread + last message, create direct/group), `conversations/{id}`
(GET/PATCH title, members add/remove, leave, mute, archive), `conversations/{id}/messages`
(GET with `before_seq`/`after_seq` paging, POST send), `conversations/{id}/read`,
`files` (POST multipart), `files/{id}`, `search?q=`, `health/live`, `health/ready` (DB ping).

### 5.6 Realtime protocol
`GET /v1/realtime` (WebSocket, token in `Authorization` header or first `hello` frame for platforms
that cannot set headers). JSON frames `{ "t": type, "id"?: string, "d": payload }`.
- Client → server: `hello {token?, cursor}`, `send {conversation_id, client_id, kind, body, meta}`,
  `read {conversation_id, seq}`, `delivered {conversation_id, seq}`, `typing {conversation_id}`, `ping`.
- Server → client: `ready {cursor, user}`, `event {seq, type, data}` (durable, from `user_events`),
  `ack {id, message}` / `error {id, code}`, `typing {conversation_id, user_id}`,
  `presence {user_id, online, last_seen}`, `pong`, `resync_required`.
- Delivery guarantee: client keeps an outbox keyed by `client_id`; resends on reconnect; server
  dedups on `(sender_id, client_id)` and returns the existing message. On `hello` with a cursor the
  server replays every `user_events` row after it, then streams live. Ordering within a conversation is by `seq`.
- Keepalive: client ping every 25 s (Cloudflare idles at 100 s); server drops after 60 s silence.
  Reconnect: exponential backoff 0.5 s → 30 s with full jitter; immediate retry on network-change signals.
- Typing is throttled client-side (one per 3 s) and expires after 5 s. Presence = any live socket;
  `last_seen` updated on disconnect. Both ephemeral (not in DB except `last_seen`).
- Fan-out: in-process hub. `# ponytail:` single instance; switch to Postgres LISTEN/NOTIFY to scale out.
Golden vectors in `protocol/vectors/` (frame examples + expected client state transitions) are
consumed by each client's tests where the toolchain allows.

### 5.7 Deployment
`deploy/compose.yaml`, project name `luma`: `luma-server` (port `127.0.0.1:8790:8790`) and
`luma-postgres` (postgres:17-alpine, no host port, named volumes `luma_pgdata`, `luma_uploads`,
dedicated network). Caddy block already correct; only its comment changes (`# Luma`). Caddy and
Cloudflare Tunnel both pass WebSocket upgrades unchanged.
Backups: nightly `pg_dump -Fc` + uploads tarball to `/var/backups/luma/` via systemd timer,
14-day retention; restore procedure tested once and documented in `deploy/RUNBOOK.md`.
Before any host change: show diff, keep a timestamped backup of the file, never `docker volume rm`.

## 6. Clients

### 6.1 Shared behaviour (all platforms)
Same screens conceptually — Welcome/Sign-in, Conversation list, Conversation, New conversation,
Profile & settings, Sessions — but laid out natively. Each client contains: API client, realtime
connection with backoff + outbox, a local cache (SQLite where native: GRDB/SwiftData on Apple, Room
on Android, Microsoft.Data.Sqlite on Windows, QtSql on Linux) so the app opens instantly and works
read-only offline. Secure token storage: Keychain, Android Keystore-backed EncryptedSharedPreferences/
DataStore, Windows PasswordVault/Credential Locker, libsecret (QtKeychain) on Linux.

### 6.2 Platform adaptations
- **iOS:** NavigationStack, large titles, swipe actions, sheets, haptics on send; Dynamic Type.
- **macOS:** NavigationSplitView (sidebar/list/detail), toolbar, keyboard-first (⌘N, ⌘F, ⌘1…9), menu bar commands, multiple windows.
- **Android:** Material 3 structure (top app bar, FAB for new chat, predictive back, edge-to-edge) re-skinned with Luma tokens; dynamic color **off** (identity wins); adaptive two-pane on large screens.
- **Windows:** NavigationView/list-detail, Mica backdrop on the window only (system material, not decorative glass), Fluent focus visuals, Segoe Fluent icons, toast notifications.
- **Linux:** Qt Quick Controls with a custom Luma style (not Material/Fusion), two/three-pane desktop layout, keyboard shortcuts, system tray + freedesktop notifications, respects system dark preference.

## 7. Design foundation (Milestone 1 deliverable)
Direction — **"instrument calm"**: the restraint of a well-made tool. Cool graphite neutrals, generous
spacing, crisp hairlines instead of cards and shadows, one warm luminous accent ("Lumen", an amber-gold)
used sparingly: the user's own messages, the primary action, unread markers, focus. Sent bubbles are filled
Lumen with ink-dark text (high contrast, and not the blue/white or green of familiar apps); received
bubbles are a quiet tonal surface. Dark mode is designed first-class (near-black graphite, not pure black).
- **Typography:** platform system UI font for body (SF Pro, Roboto, Segoe UI Variable; Inter bundled on
  Linux). Small metadata — timestamps, receipts, @usernames, counts — in a bundled mono (Geist Mono, OFL)
  with tabular figures: the "slightly futuristic" signature, applied only to metadata.
- **Tokens:** color (light/dark, semantic names), type scale, 4-pt spacing scale, sizing (avatars, hit
  targets ≥ 44pt/48dp), radii tiers, elevation (hairline + at most one shadow level for floating
  surfaces), motion (durations, curves, spring params; every animation tied to a state change; reduced-motion respected).
- **Iconography:** platform-native symbol sets (SF Symbols, Material Symbols Rounded, Segoe Fluent,
  Lucide on Linux) with a shared semantic icon map; a custom Luma glyph for the app icon only.
- **Accessibility:** WCAG AA contrast for all text (verified by script), screen-reader labels per
  platform, full keyboard operation on desktop, text scaling to 200 %, reduced motion/transparency.
- **References:** Apple HIG, Material 3, Fluent 2, KDE HIG (structure); Things 3 (calm hierarchy and
  motion), Linear (typographic restraint), Teenage Engineering (instrument-like precision) — for
  qualities, not visuals to copy.
- Deliverable: `design/tokens.json`, generated platform files, `design/DESIGN.md`, and a rendered
  preview (light/dark, chat screen at phone and desktop width) for owner review before screens are built.

## 8. Deferred, with the hook already in place
Mobile push (needs APNs key + Firebase project from owner; server will get a `push_tokens` table and
a sender interface), E2EE (envelope + MLS later), reactions/replies/edits/voice (`kind` + `meta`),
horizontal scaling (LISTEN/NOTIFY), object storage (S3-compatible behind the files interface).

## 9. Milestones
1. Design foundation (tokens, generator, DESIGN.md, preview) → owner review.
2. Backend vertical slice: migrations, invites, register/login/sessions, users, DMs, messages REST +
   realtime with replay and idempotency; integration tests on real Postgres.
3. Linux Qt slice: sign-in, conversation list, live DM; screenshots.
4. Backend complete MVP: groups, receipts, typing, presence, files, search, rate limits, admin CLI;
   deploy to `chat.folk3s.com` with backups.
5. Android slice → MVP; Roborazzi screenshots.
6. Apple (iOS + macOS) and Windows clients to MVP; CI workflows.
7. Parity & polish pass on all states; final verification report.
Each milestone ends with: working code, test output, verification matrix, known limitations, a commit.

## 10. Rules settled after builder review
**Owner decisions**
- **Group history:** members see messages from `joined_at` onward only. After leaving, a member keeps
  read access up to their `left_at` seq and gets nothing newer. Re-joining starts a new visibility window.
  Messages, files, search and event replay all enforce this in SQL (store `joined_seq`/`left_seq` on `members`).
- **Backups:** local only (`/var/backups/luma`, 14 days). The runbook states plainly that this does not survive losing the disk.

**Builder's calls (owner may override)**
- **Stop points:** work runs continuously. Only two things wait for the owner: (a) package installs
  needing root (owner runs the command), (b) the first production deploy (`docker compose up` for the
  `luma` project + Caddy comment edit), which is confirmed explicitly. The design preview is shown to
  the owner, and screen work proceeds in parallel; feedback is folded in afterwards.
- **"MVP done" per client:** a development build that runs and works against production, plus a CI build.
  Signing, store submission and Linux packaging are post-MVP. Minimum versions: iOS 17 / macOS 14
  (SwiftData, Observation), Android API 26 (min) / 36 (target), Windows 10 1809+ (Mica on Windows 11,
  solid fallback on 10), Qt ≥ 6.5 (Linux).
- **Group management:** the owner and admins (`role` gains `admin`) can rename, remove members and promote others.
  Any member can add members. When the owner leaves, ownership passes to the longest-standing admin,
  or else to the longest-standing member. Each of these writes a `system` message plus a `user_events` entry.
- **Presence & reachability:** presence goes only to users who share a conversation. User search returns
  exact username matches plus prefix matches among people you already share a conversation with; anyone can open a DM by exact username.
  Blocking is in the MVP (`blocks` table): a blocked user cannot DM you, add you to groups or see your presence;
  in shared groups, their messages stay visible but are collapsed client-side.

## 11. Left to the builder
Internal package structure, exact library versions, token numeric values within the direction above,
exact copy text, test layout, CI details.
