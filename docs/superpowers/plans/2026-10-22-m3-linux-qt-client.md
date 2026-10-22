# Luma M3: Linux Qt Client — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The first fully working native Luma client: a Qt 6 Quick desktop app that signs in, lists conversations, opens direct conversations by username, and sends and receives messages in real time. It persists a local cache and an outbox, and renders every connection state. Its visuals are checked against the M1 preview.

**Architecture:**
- **C++ core** (no QML dependency, unit-testable):
  - `ApiClient` for REST.
  - `RealtimeClient` for WebSocket, backoff and ping.
  - `Store` for SQLite: cache, outbox, cursor.
  - `Sync`, which applies events and owns the outbox.
- **QML UI** on `QtQuick.Controls.Basic`, fully re-skinned from the generated `LumaTokens` singleton. Not Material or Fusion.
- **Icons:** Lucide SVGs rendered through a C++ image provider (`image://icon/<name>?color=…`). This works on the software backend, which is used headlessly.

**Tech Stack:** Qt 6.12 (Quick, QuickControls2, WebSockets, Sql/SQLite, Svg, Test), Qt6Keychain, CMake 4 + Ninja, C++20.

**Spec:** `docs/superpowers/specs/2026-10-22-luma-mvp-design.md` (§6, §7, §11.4), `design/DESIGN.md`, `protocol/realtime.md`, `protocol/openapi.yaml`

## Global Constraints
- **Native Qt Quick** only. No WebEngine or webview. The style is custom on Basic; no Material, Fusion or Universal.
- **Tokens** only from `design/generated/LumaTokens.qml`, copied into the QML module at build time. No literal colors in QML.
- **Fonts:**
  - The display face is Instrument Sans, bundled under OFL.
  - The UI font is the platform theme's. When it's a generic fallback (offscreen/minimal platforms), use the first installed of Noto Sans, Adwaita Sans, Cantarell.
- **Token storage:** the token lives in the system keyring via Qt6Keychain. With no keyring, the token is held in memory only, unless `LUMA_INSECURE_TOKEN_FILE` is set, which is for tests and screenshots only and prints a warning.
- **Fresh install** sends `cursor: null`. Persist the cursor only after an event is applied to SQLite.
- **Outbox:** keyed by `client_id`, persisted, and resent after every `ready`.
- **Backoff:** 0.5 s doubling to a 30 s cap, with full jitter. Client ping every 25 s.
- **Layout:** three panes (rail 56 / list 320 / chat) at window width ≥ 1100. Two panes (list / chat) below that, and one pane below 720.
- **Keyboard:** Ctrl+N new conversation, Ctrl+K quick switcher (focuses search), Enter sends, Shift+Enter adds a new line, Esc closes overlays.
- **Copy** matches DESIGN.md §7 exactly ("No conversations yet", "Reconnecting in 4 s", "Offline. Messages will send when you're back.", "Couldn't send. Click to retry.").
- **Server URL:** `LUMA_SERVER` env or `--server`, default `https://chat.folk3s.com`.

## Review Focus
1. **The app is quit with messages still in the outbox.** On the next launch they show as pending and send after `ready`, exactly once. Test: `tst_sync::outboxSurvivesRestart`.
2. **The server sends `resync_required`** (stale or fresh cursor). The client must reload conversations and must not keep stale unread counts. Test: `tst_sync::resyncReloads`.
3. **A message arrives for a conversation that isn't in the local list** (`message.created` before `conversation.created` has been fetched). The conversation must be fetched and shown, not dropped. Test: `tst_sync::unknownConversationFetched`.
4. **The same message arrives twice** (ack plus own event, or replay). There must be one bubble, and the pending bubble must be replaced in place rather than duplicated. Test: `tst_sync::ackAndEventDedupe`.
5. **A 401 or WebSocket close code 4003** (session revoked). Sign out locally, clear the keyring entry, and show the sign-in screen with "You were signed out." Test: `tst_sync::revokedSignsOut`.

---

### Task 1: Scaffold, tokens module, fonts, icon provider, window
**Files:**
- `linux/CMakeLists.txt`
- `linux/src/main.cpp`
- `linux/src/IconProvider.{h,cpp}`
- `linux/assets/icons/*.svg` (Lucide, ISC)
- `linux/assets/fonts/InstrumentSans.ttf` + `OFL.txt`
- `linux/qml/Main.qml`
- `linux/tests/tst_icons.cpp`

**Interfaces:**
- QML module `Luma` (URI `Luma`). The `LumaTokens` singleton is copied from `design/generated`.
- `IconProvider : QQuickImageProvider`, id `icon`. URL `image://icon/<name>?color=%23RRGGBB`. The requested size is honoured for HiDPI.
- CLI flags:
  - `--server URL`
  - `--profile DIR` (data dir, default `QStandardPaths::AppDataLocation`)
  - `--theme light|dark|system`
  - `--screenshot FILE --size WxH [--after-ms N]` (grab the window and exit)

- [ ] `tst_icons::rendersColored`: render `search` at 40×40 in `#0E1418`. The image is non-null, has at least one opaque pixel of that color, and an unknown name gives a null image.
- [ ] Implement. `cmake -S linux -B linux/build -G Ninja && cmake --build linux/build && ctest --test-dir linux/build` passes.
- [ ] `QT_QPA_PLATFORM=offscreen QT_QUICK_BACKEND=software linux/build/luma --screenshot /tmp/x.png --size 1280x800` writes a PNG of the empty window.
- [ ] Commit.

### Task 2: Core: ApiClient, Store, Backoff
**Files:**
- `linux/src/core/{ApiClient,Store,Backoff,Types}.{h,cpp}`
- `linux/tests/tst_store.cpp`, `linux/tests/tst_backoff.cpp`

**Interfaces:**
- `Types.h`: plain structs `Message`, `Member`, `Conversation`, each with `static fromJson(QJsonObject)` (field names exactly as in openapi.yaml), plus `OutboxItem{QUuid clientId; QString conversationId, body; QDateTime createdAt; enum State{Pending, Failed} state;}`.
- `Store(QString path)`:
  - Conversations: `upsertConversation`, `conversations()` ordered by `lastActivityAt` desc.
  - Messages: `upsertMessage` (key `(conversationId, seq)`), `messages(convId, limit)`.
  - Outbox: `addOutbox`, `removeOutbox(clientId)`, `setOutboxState`, `outbox()`.
  - Cursor and reset: `cursor() -> std::optional<qint64>`, `setCursor`, `clear()`.
- `Backoff`:
  - `next() -> std::chrono::milliseconds` (full jitter in [0, min(30000, 500·2^n)]), `reset()`.
  - An injectable RNG for tests.
- `ApiClient(QUrl base)`:
  - Async calls returning `QFuture<ApiResult>`: `login`, `registerAccount`, `me`, `conversations`, `conversation(id)`, `openDirect(username)`, `history(conv, beforeSeq, limit)`, `lookup(username)`.
  - `ApiResult{int status; QJsonDocument body; QString errorCode;}`.

- [ ] Tests:
  - `tst_store` covers upsert idempotence, ordering, outbox round-trip across reopening the DB file, cursor null versus 0, and `clear()`.
  - `tst_backoff` covers bounds over 1000 samples, the cap at 30 s, and reset.
- [ ] Implement. Tests pass.
- [ ] Commit.

### Task 3: RealtimeClient + Sync (the client sync engine)
**Files:**
- `linux/src/core/{RealtimeClient,Sync}.{h,cpp}`
- `linux/tests/tst_sync.cpp` (unit, using a fake transport)
- `linux/tests/tst_e2e.cpp` and `linux/scripts/e2e.sh` (against a real server)

**Interfaces:**
- `RealtimeClient`:
  - Owns `QWebSocket`.
  - States: `Connecting, Online, Reconnecting{int secondsUntilRetry}, Offline`.
  - `start(url, token, std::optional<qint64> cursor)`, `stop()`, `sendFrame(QJsonObject)`.
  - Signals: `frame(QJsonObject)`, `stateChanged()`, `closedWithCode(int)`.
  - Reconnects with `Backoff`. Treats `QNetworkInformation` reachability changes as "retry now".
- `Sync(Store&, ApiClient&, Transport&)`:
  - Applies the frames in `realtime.md`.
  - `send(convId, text) -> QUuid` creates an outbox item and sends if online.
  - `retry(clientId)`, `markRead(convId, seq)`, `typing(convId)` (throttled to 3 s).
  - Signals: `conversationsChanged()`, `messagesChanged(convId)`, `typingChanged(convId, userId)`, `presenceChanged(userId)`, `signedOut(reason)`.
- `Transport` is an abstract interface over `RealtimeClient` so tests can inject frames.

- [ ] `tst_sync` (fake transport):
  - Every Review Focus case.
  - Plus: frames from `protocol/vectors/realtime-basic.json` parse and apply. A receipt updates the member watermark.
  - Unread is computed as `lastSeq - lastReadSeq`.
  - Typing expires after 5 s.
- [ ] `tst_e2e` (`e2e.sh` starts `server/luma-server` on `127.0.0.1:8798` against a fresh `luma_e2e_*` DB on luma-test-pg and exports invites):
  - Two `Sync` instances (alice and bob).
  - Alice opens a DM and sends. Bob receives. Bob reads. Alice sees the receipt.
  - Kill alice's socket, queue 2 messages offline, reconnect. Bob gets exactly 2 messages, in order.
  - A fresh profile with `cursor null` gets a resync, then sees the history.
- [ ] Implement. All tests pass.
- [ ] Commit.

### Task 4: QML interface
**Files:**
- `linux/src/app/{AppController,ConversationListModel,MessageListModel}.{h,cpp}`
- `linux/qml/{Main,SignIn,ChatsPane,ChatView,NewConversation,EmptyState}.qml`
- `linux/qml/components/{Avatar,ConversationRow,Bubble,Receipt,Composer,LumaLine,StatusNote,Badge,IconButton,LumaTextField,LumaButton,Rail}.qml`
- `linux/tests/tst_models.cpp`

**Interfaces:**
- `AppController`, exposed as `App`:
  - Properties: `phase` (`signedOut | signingIn | ready`), `connection` (`online | connecting | reconnecting | offline`), `retryIn`, `me`, `conversations` (model), `currentConversation`, `messages` (model), `errorText`.
  - Invokables: `signIn`, `signUp`, `signOut`, `openConversation`, `startDirect(username)`, `send(text)`, `retry(clientId)`, `markVisibleRead()`.
- `MessageListModel` roles: `id, seq, body, mine, time, receipt (pending|sent|delivered|read|failed), groupPos (single|first|middle|last), showDateSeparator, dateLabel, kind, fileName, fileSize`. Grouping is by the same sender within 3 minutes (DESIGN.md §4).
- `ConversationListModel` roles: `id, title, initials, preview, time, unread, muted, online, typing, selected`.

- [ ] `tst_models`:
  - The grouping positions for a 5-message run with a 4-minute gap.
  - Receipt derivation from the other member's watermarks.
  - Time labels: `HH:mm` today, "Yesterday", weekday within 7 days, `d MMM` otherwise.
- [ ] Implement the screens per DESIGN.md §7–8:
  - Linux three-pane layout, luma line, empty, loading, offline, reconnecting and failed states.
  - Focus ring on every control, plus accessible names (`Accessible.name`) on rows, bubbles and buttons.
- [ ] Commit.

### Task 5: Visual verification against the preview
**Files:**
- `linux/scripts/seed-demo.sh`: registers the preview's people and recreates the preview's conversations through the REST API on a local server.
- `linux/scripts/shoot.sh`
- `linux/shots/*.png`

- [ ] Seed a local server. Shoot light and dark at 1280×800 (three-pane), 960×720 (two-pane), and the sign-in screen.
- [ ] Inspect each PNG next to `design/preview/shots/linux-*.png`. Record differences and fixes in `linux/shots/CRITIQUE.md`. Fix, then reshoot.
- [ ] Commit.

### Task 6: Contract validation report
- [ ] Record every mismatch between the server, `openapi.yaml` and `realtime.md` found while building the client in `protocol/CHANGELOG.md`, and fix it at the source. Rerun the server contract tests and the client e2e.
- [ ] Commit.
