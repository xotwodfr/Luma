# Luma threat model (v1, 2026-10-22)

Scope: the Go server (`server/`), its deployment (`deploy/`) and the protocol clients rely on
(`protocol/`). STRIDE per trust boundary, then abuse and harassment cases, then accepted residual
risks. Every mitigation names the test that proves it. Test packages: **[api]** =
`server/internal/api`, **[chat]** = `server/internal/chat`, **[httpx]**, **[auth]**, **[db]**; **[deploy]** =
`deploy/test-restore.sh` (run output in `deploy/RUNBOOK.md`).

## Assets
Message bodies and files; who talks to whom (membership, presence, typing); credentials and
session tokens; account existence and block relationships; availability of the service *and of
the other services on the same host*; backups.

## Trust boundaries

```
 client app ──TLS── Cloudflare edge ──tunnel── cloudflared ── Caddy (host, :80) ── 127.0.0.1:8790
                                                                    │ docker-proxy (bridge gateway 172.16.79.1)
                                                                    ▼
                                               luma-server (nonroot, read-only) ── luma-postgres (no host port)
                                                       │                                  │
                                                  luma_uploads                       luma_pgdata ──► backup.sh ──► /var/backups/luma (age)
```

### 1. Client ↔ edge (internet)

| | Threat | Mitigation | Proof |
|---|---|---|---|
| S | Guessing passwords | Argon2id; per-IP 10/min **and** per-username 30/h (per-username can't be bypassed by rotating or forging IPs); dummy verify for unknown users | `TestLogin_RateLimited`, `TestLogin_PerAccountLimit`, `TestLogin_EnumerationSafe` [api] |
| S | Stolen/forged token | 256-bit random, stored as SHA-256 only, 90-day idle expiry, revocable; revocation closes sockets within 1 s, also across processes | `TestAuth_MalformedTokens`, `TestSession_SlidingExpiry`, `TestSessions_RevokeAndPasswordChange`, `TestRealtime_RevokeClosesSocket`, `TestRealtime_RevokeFromOtherProcessClosesSocket`, `TestRealtime_NoSendsAcceptedAfterRevoke` [api] |
| T | Malformed / oversized input | JSON ≤ 128 KiB, unknown fields rejected; text ≤ 8000 chars; client `meta` rejected except `{file_id}` on file messages | `TestBodyLimits`, `TestSend_Validation` [api/chat], `TestFiles_AttachRules` [api] |
| R | User denies an action | Group changes are recorded as system messages (`actor` in meta) in the conversation itself; access log has request id, route, status and user id | `TestGroups_FanOutFollowsMembership` [chat], `TestLogs_NoSecrets` [api] |
| I | Account enumeration | Same error and timing for unknown user / wrong password; username-taken only revealed with a valid invite; lookups 404 for blocked; group adds 404 identically for unknown, disabled and blocked | `TestLogin_EnumerationSafe`, `TestRegister_UsernameTakenOnlyRevealedWithValidInvite`, `TestUsers_LookupHiddenFromBlocked` [api], `TestGroups_BlockedAddsRefused` [chat] |
| I | Secrets in logs | Logs never include bodies, query strings, headers, tokens or passwords | `TestLogs_NoSecrets` [api] |
| D | Request floods | Per-user buckets (sends 30/10 s shared REST+WS, uploads 20/min, search 30/min, new DMs 20/day, groups 10/day, group adds 300/day); Argon2 concurrency capped at 4 (memory bound) | `TestSend_RateLimited`, `TestSearch_RateLimited`, `TestOpenDirect_LimitOnlyCountsNew` [api] |
| D | Slow or huge uploads | Size cap while streaming (nothing over the cap reaches disk); 10-minute read deadline per upload; 1 GiB/day/user quota | `TestFiles_SizeCapEnforcedWhileStreaming`, `TestFiles_DailyQuota` [api] |
| D | Idle unauthenticated sockets | 10 s hello timeout | `TestRealtime_HelloTimeout` [api] |

### 2. Edge ↔ server (host)

| | Threat | Mitigation | Proof |
|---|---|---|---|
| S | Forged `CF-Connecting-IP` / `X-Forwarded-For` | Honoured only when the TCP peer is the configured proxy (container: bridge gateway only; other containers and direct peers ignored) | `TestClientIP_OnlyTrustedPeersMayForward` [httpx], `TestLogin_ProxyIPOnlyTrustedWhenConfigured` [api], trusted-proxy step in [deploy] |
| S | Bypassing Cloudflare via Caddy `*:80` on the public IP | **Not mitigated in Luma** beyond the per-username login limit; see residual risk R5 | `TestLogin_PerAccountLimit` [api] |
| T/I | Traffic between Cloudflare and Caddy | Encrypted inside the tunnel; Caddy → server is loopback / host bridge only | (deployment; `127.0.0.1:8790` binding checked by `deploy/preflight.sh`) |
| E | Container escape / lateral movement | Server: distroless, nonroot uid 65532, read-only rootfs, all capabilities dropped, no-new-privileges; Postgres has no host port and its own network | `deploy/compose.yaml`; staging run in [deploy] |

### 3. Server ↔ database and disk

| | Threat | Mitigation | Proof |
|---|---|---|---|
| T | Lost or reordered messages | Seq allocated under the conversation row lock in the inserting transaction; idempotent on `(sender, client_id)`; ack only after commit; event written in the same transaction | `TestSend_ConcurrentGapFree`, `TestSend_DuplicateClientID`, `TestSend_NoAckBeforeCommit` [chat], `TestRealtime_RestartDurability`, `TestRealtime_FailedCommitNotAcked`, `TestRealtime_ReplayLiveRace` [api] |
| T | Membership races (stale reads under READ COMMITTED) | Lock the conversation row in one statement, read membership in the next; all membership changes serialise on that lock; at most one owner is a unique index | `TestSend_RacingRemovalStaysInWindow`, `TestGroups_ConcurrentRemovalsKeepOneOwner` [chat] |
| T | Deadlocks | Lock order conversations → files → users → members, `FOR NO KEY UPDATE` only | `TestSend_NoDeadlockWithReceipts` [chat] |
| T | Path traversal via filenames | Storage path is `UPLOAD_DIR/ab/cd/<server uuid>`; the client name is display-only and reduced to its last element | `TestFiles_SniffedTypeWins` (`../../etc/passwd` case) [api] |
| I | Reading uploads off disk | Files mode 0600 in a nonroot-owned volume; Docker volumes root-only on the host | `TestFiles_SizeCapEnforcedWhileStreaming` (mode check) [api] |
| I | Backups leak data | age-encrypted to an off-host key; checksums; the backup dir is root mode 700 | "dump is encrypted", "tampered backup is refused" in [deploy] |
| D | Losing data | Nightly encrypted dump + uploads; restore proven into a scratch project with identical counts and file hashes | [deploy] |
| D | Concurrent migrations | Advisory lock, one transaction per migration | `TestMigrate_ConcurrentSafe`, `TestMigrate_Idempotent` [db] |

### 4. User ↔ user

| | Threat | Mitigation | Proof |
|---|---|---|---|
| I | Reading a conversation you're not in | Every query checks membership in SQL; non-members get 404 everywhere (REST, WS, files, search); a matrix covers every route × principal | `TestAuthzMatrix`, `TestAuthzMatrix_CoversAllRoutes` [api], `TestSend_NonMemberDenied` [chat] |
| I | Former / re-joined members seeing what was said while they were out | One window `(joined_seq, left_seq]` per member, enforced in history, list, events, files and search; re-joining opens a new window | `TestGroups_VisibilityWindows`, `TestGroups_RejoinWindowExcludesGap`, `TestSearch_RespectsVisibilityWindows` [chat], `TestFiles_RemovedMemberCannotFetchLaterFile`, `TestRealtime_GroupFanOutFollowsMembership` [api] |
| I | Former members learning the current roster | Members list is empty for former members | `TestGroups_VisibilityWindows` [chat] |
| E | Members acting as admins | Role checked under the conversation lock; owner can't be removed or demoted; only the owner transfers ownership | `TestGroups_RolePermissions` [chat], matrix rows for `{g}` [api] |
| E | Posting after removal | Send requires active membership, read after the lock | `TestGroups_RemovedMemberCannotSend`, `TestSend_RacingRemovalStaysInWindow` [chat] |
| T | Spoofed attachment metadata | Clients send only `file_id`; name, type, size, dimensions come from the server; a file attaches once, by its uploader, to its own conversation | `TestFiles_AttachRules` [api] |
| T | Script via uploaded content (stored XSS) | Type sniffed from bytes; only png/jpeg/webp/gif/heic inline, everything else `application/octet-stream` + `attachment`; `nosniff`, `CSP: default-src 'none'; sandbox`, `Cache-Control: private` | `TestFiles_SniffedTypeWins` [api] |
| D | Decompression bombs against clients | Inline images must parse and stay ≤ 12 000 px a side and ≤ 50 MP | `TestFiles_DecompressionBombRejected` [api] |
| T | Fake highlight markers in search results | Server strips U+0001/U+0002 from bodies before marking; headlines are plain text | `TestSearch_HeadlineMarkersAreServers` [chat] |

## Abuse and harassment

| Case | Mitigation | Proof |
|---|---|---|
| Stranger keeps messaging me | Block: they can't DM you (REST or WS), add you to groups, see your presence or typing, or look you up | `TestSend_BlockedDMDenied`, `TestOpenDirect_BlockedLooksMissing` [chat], `TestRealtime_SendBlockedDenied`, `TestRealtime_TypingNotSentToBlocked`, `TestRealtime_PresenceOnlyToPartners`, `TestUsers_BlockBack` [api] |
| Blocked person re-reaches me through a group | They can't add you, you can't add them; a *third* member can add both (documented: messages are still delivered and the client collapses them) | `TestGroups_BlockedAddsRefused` [chat] |
| Mass-adding strangers to groups / spam DMs | 300 adds/day, 10 groups/day, 20 new DMs/day; invite-only registration with a 5-invite quota | `TestOpenDirect_LimitOnlyCountsNew`, `TestInvites_UserQuota` [api] |
| Probing whether someone blocked me | Lookup, DM open and group add all return the same 404 as for nonexistent users | `TestUsers_LookupHiddenFromBlocked` [api], `TestGroups_BlockedAddsRefused` [chat] |
| Searching a blocked person's old messages | Allowed in shared groups (same as the timeline); a documented choice | `TestSearch_BlockedSendersNotFiltered` [chat] |
| Abusive account | `admin disable-user` revokes all sessions and closes sockets | `TestDisabledUser_CannotLogin` [api] |
| Filling the shared host's disk | 25 MiB/file, 20 uploads/min, 1 GiB/day/user, orphans swept after 24 h | `TestFiles_DailyQuota`, `TestFiles_OrphanSweep` [api] |

## Residual risks (accepted for v1, stated to users and the owner)

- **R1 No end-to-end encryption.** The operator can read everything. Stated in `README.md`, `protocol/openapi.yaml`
  and the runbook. **Gap:** the in-app "About & privacy" screen (spec §11.1) doesn't exist in any client yet.
- **R2 Host disk unencrypted.** Volumes are root-only; backups are encrypted; physical or root compromise exposes live data.
- **R3 Single instance.** In-memory rate limits and fan-out; a restart loses typing/presence (not messages).
  Backups are local only and don't survive the disk.
- **R4 No push notifications.** Clients only learn of messages while connected.
- **R5 Cloudflare bypass on port 80.** Caddy listens on all interfaces and the host has a public IP, so per-IP
  limits can be evaded by forging `CF-Connecting-IP`. Per-username login limits still hold. Fix at the host:
  firewall :80 to loopback or bind the Caddy site to 127.0.0.1 (owner decision; outside Luma's isolation boundary).
- **R6 HEIC dimensions come from the declared `ispe` property** (largest found). A crafted file whose coded
  image is larger than its declared extents isn't caught server-side; clients must keep their own decode limits.
- **R7 Re-joining hides the earlier window too** (one window per membership row). Safer than over-sharing; may
  surprise users.
- **R8 Former members see the current group title** (renames after they left). The roster is hidden.
- **R9 Files on disk without a row** can remain after a crash between the file rename and the row commit; they are
  unreachable through the API. Not swept automatically.
