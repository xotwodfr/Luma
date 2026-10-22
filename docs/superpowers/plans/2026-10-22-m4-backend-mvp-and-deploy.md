# Luma M4: Backend MVP Completion + Production Deploy — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Groups, conversation management, file sharing and message search on the server, plus a written threat model. Then a deployable, isolated production stack with tested encrypted backups and a runbook. The first production deploy is gated on the owner's explicit approval.

**Architecture:** This extends `server/` from M2. The rules and patterns stay the same:
- Every conversation-scoped query enforces membership and the visibility window in SQL.
- Every state change writes `user_events` inside its transaction.
- Locks are taken in the order conversations → users → members, always with `FOR NO KEY UPDATE`.

**Tech Stack:** Go, PostgreSQL 17, Docker Compose, `age` (in the backup container), systemd timers.

**Spec:** `docs/superpowers/specs/2026-10-22-luma-mvp-design.md` (§5, §10, §11.1, §11.5); `protocol/openapi.yaml`; `protocol/realtime.md`

## Global Constraints
- **Group history:**
  - A member sees `joined_seq < seq <= coalesce(left_seq, ∞)`. On (re)join, `joined_seq = conversations.last_seq`.
  - After leaving, history up to `left_seq` stays readable and nothing newer is.
- **Group roles:**
  - Owner and admins can rename, remove members and promote. Any member can add.
  - When the owner leaves, ownership passes to the longest-standing admin, else the longest-standing member.
  - Groups hold at most 256 members. Titles are 1–80 characters.
- **Blocking:**
  - A user can't add someone who blocked them or whom they blocked.
  - In shared groups, messages are still delivered; collapsing them is up to the client.
  - Every new path goes through `users.BlockedEitherWay`.
- **Files:**
  - Size cap `LUMA_MAX_UPLOAD_BYTES` (25 MiB), enforced while streaming.
  - MIME is sniffed. Only png, jpeg, webp, gif and heic display inline; everything else is served as `application/octet-stream` with `attachment`.
  - Image dimensions are capped at 12 000 × 12 000 and 50 MP.
  - Files are served only to members whose window contains the message. Unattached uploads are swept after 24 h.
- **`meta` stays server-built:** clients send `file_id`, and the server fills name, mime, size and dimensions. Arbitrary client meta is still rejected.
- **Search:** `websearch_to_tsquery('simple', q)`, only over visible messages, rate-limited to 30 per minute per user.
- **Deploy:**
  - Compose project `luma`, network `luma`, volumes `luma_pgdata` and `luma_uploads`, port `127.0.0.1:8790` only.
  - Never `docker volume rm`. Never overwrite an existing `deploy/.env`.
  - Caddy needs only its comment changed (`# Luma`). Show the diff and back up the file first.
- **The first production deploy waits for the owner's explicit "yes".**

## Review Focus
1. **Someone removed from a group requests an attachment** posted after their removal: they must get 404. Test: `TestFiles_RemovedMemberCannotFetchLaterFile`.
2. **Two admins remove each other concurrently**, or the owner leaves while being demoted. The group must always end with exactly one owner while it has members. Test: `TestGroups_ConcurrentRemovalsKeepOneOwner`.
3. **Re-joining a group** must not reveal messages from the gap while away. Test: `TestGroups_RejoinWindowExcludesGap`.
4. **An upload that lies about its type** (an HTML file named `.png`, or a polyglot) must never be served inline. Test: `TestFiles_SniffedTypeWins`.
5. **The backup restored into a scratch project** must contain the same message count and files as the source. Test: `deploy/test-restore.sh` (run and recorded).

---

### Task 1: Groups
**Files:**
- `server/internal/db/migrations/0002_groups.sql` (index on `members(conversation_id) WHERE left_at IS NULL`, system message kinds)
- `server/internal/chat/groups.go`
- `server/internal/chat/groups_test.go`
- `api/router.go` routes, `authz_matrix.json` rows, `openapi.yaml`

**Interfaces:**
- `CreateGroup(ctx, me, title string, usernames []string) (Conversation, error)`
- `AddMembers(ctx, me, conv, usernames []string)`
- `RemoveMember(ctx, me, conv, userID)`
- `Leave(ctx, me, conv)`
- `Rename(ctx, me, conv, title)`
- `SetRole(ctx, me, conv, userID, role)`
- Each one writes a `system` message, using `meta.event` set to `member_added`, `member_removed`, `member_left`, `renamed`, `role_changed` or `owner_transferred`, through the same path as `send`. It also writes a `membership.updated` user event for the affected users, including the one removed.
- Routes:
  - `POST /v1/conversations {kind:"group", title, usernames}`
  - `POST /v1/conversations/{id}/members`
  - `DELETE /v1/conversations/{id}/members/{user_id}`
  - `POST /v1/conversations/{id}/leave`
  - `PATCH /v1/conversations/{id}` `{title}`
  - `PATCH /v1/conversations/{id}/members/{user_id}` `{role}`

- [ ] Tests:
  - Visibility windows: join, leave and rejoin.
  - Role permissions for every action.
  - Ownership transfer on leave.
  - Blocked adds refused either way.
  - The 256-member cap.
  - A send by a removed member is refused.
  - Concurrent removals.
  - The realtime fan-out reaches added members and stops for removed ones.
- [ ] Implement, then run `go test -race ./...`.
- [ ] Commit.

### Task 2: Conversation settings
**Interfaces:**
- `PATCH /v1/conversations/{id}/settings {muted_until?: RFC3339|null, archived?: bool}`. A `settings.updated` event goes to my other devices.

- [ ] Tests:
  - Mute expiry is reflected in the list.
  - Archived conversations stay in the list with `archived:true`.
  - An archived conversation comes back to the inbox when a new message arrives.
- [ ] Implement.
- [ ] Commit.

### Task 3: Files
**Files:**
- `0003_files.sql` (`files.attached_at`)
- `server/internal/files/{service,handlers}.go` + tests
- Send path: `kind:"file"` with `{file_id, body?}`

**Interfaces:**
- `POST /v1/files?conversation_id=` (multipart field `file`) → `{id, name, mime, size, width?, height?}`
- `GET /v1/files/{id}`, served with `Content-Disposition`, `X-Content-Type-Options: nosniff` and `Cache-Control: private`.
- Storage: `UPLOAD_DIR/ab/cd/<uuid>`, written with mode 0600 and fsynced before the row commits.

- [ ] Tests:
  - The size cap is enforced while streaming (bytes on disk never exceed the cap).
  - Sniffing overrides the declared type.
  - Decompression-bomb dimensions are rejected.
  - Fetch is denied to non-members and to members outside the window, including removed members.
  - The orphan sweep removes files older than 24 h that were never attached.
  - Attaching another conversation's file is refused.
- [ ] Implement.
- [ ] Commit.

### Task 4: Search
**Interfaces:**
- `GET /v1/search?q=&conversation_id=&limit=` returns `{results:[{conversation_id, message, headline}]}`, newest first.
- `headline` comes from `ts_headline` with the markers `\u0001` and `\u0002`. Clients style them; HTML is never interpreted.

- [ ] Tests: window-respecting results (left groups, rejoin gaps), blocked senders not filtered (a documented choice), and the rate limit.
- [ ] Implement.
- [ ] Commit.

### Task 5: Threat model
- [ ] Write `docs/security/threat-model.md`:
  - STRIDE per trust boundary, plus abuse cases.
  - Each mitigation links to the test that proves it.
  - Residual risks: no E2EE, unencrypted host disk, single instance, no push.
- [ ] Commit.

### Task 6: Deploy stack, backups, runbook
**Files:**
- `deploy/compose.yaml`, `deploy/.env.example`
- `deploy/preflight.sh`: port 8790 free, volumes `luma_*` not about to be recreated, Caddy block present, `.env` mode 600.
- `deploy/backup.sh`: `pg_dump -Fc` plus an uploads tar, `age`-encrypted to `LUMA_BACKUP_RECIPIENT`, with checksums, written to `/var/backups/luma` and kept for 14 days.
- `deploy/restore.sh`: restores into the scratch project `luma_restore_test`.
- `deploy/test-restore.sh`
- `deploy/systemd/luma-backup.{service,timer}`
- `deploy/RUNBOOK.md`

- [ ] Run against a scratch project `luma_staging` on port 8791: preflight → `up` → seed → backup → restore → compare counts → `down` (keeping volumes, then removing only the scratch volumes created by this test, by name).
- [ ] Record the output in RUNBOOK.md.
- [ ] Commit.
- [ ] **Stop and ask the owner** before the production `up` and the Caddy comment edit.
