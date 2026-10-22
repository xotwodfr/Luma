# Luma runbook

Production: `https://chat.folk3s.com` → Cloudflare Tunnel (`cloudflared-folk3s`) → host Caddy
(`/etc/caddy/Caddyfile`, block `http://chat.folk3s.com`) → `127.0.0.1:8790` → container `luma-server`.

| Thing | Value |
|---|---|
| Compose project | `luma` (file `deploy/compose.yaml`, env `deploy/.env`, mode 600, gitignored) |
| Containers | `luma-luma-server-1` (distroless, nonroot, read-only rootfs, no capabilities), `luma-luma-postgres-1` (postgres:17-alpine, no host port) |
| Network | `luma_default`, subnet `172.16.79.0/24`; the server trusts proxy headers only from the gateway `172.16.79.1` (where Caddy's connections arrive) |
| Volumes | `luma_pgdata`, `luma_uploads`. **Never** `docker volume rm` them and never `docker compose down -v`. |
| Image tags | `luma-server:<git short SHA>`; `luma-backup` (age + tar, `deploy/backup.Dockerfile`) |
| Backups | `/var/backups/luma` (root, mode 700), nightly 03:30 ± 15 min, 14 days, age-encrypted |
| Scratch projects | `luma_staging` (:8791, 172.16.80.0/24), `luma_restore_test` (:8792, 172.16.81.0/24) |

All commands run from the repository root. `C` below is shorthand for
`docker compose -p luma --env-file deploy/.env -f deploy/compose.yaml`.

## Security facts to keep in mind

- **No end-to-end encryption.** The operator (anyone with root or docker access on this host) can read
  messages and files. Credentials, session tokens and invite codes are stored only as hashes.
- **The host disk is not encrypted** (LVM + btrfs, no LUKS). Docker volumes are root-only; backups are
  age-encrypted to a key whose private half must not be on this machine. Full-disk encryption is
  recommended at the host level; Luma does not claim it.
- **Backups are local only.** They do not survive losing this disk. Copy `/var/backups/luma` off-host
  if that matters (the files are encrypted; copying them leaks nothing but sizes and times).
- **Caddy listens on `*:80` and this host has a public IP.** A client that reaches port 80 directly
  bypasses Cloudflare and can forge `CF-Connecting-IP`. Luma limits logins per username as well as per
  IP, so this cannot speed up password guessing on one account, but per-IP limits can be evaded.
  Recommended: firewall port 80 to loopback (cloudflared is local), or bind that Caddy site to 127.0.0.1.

## First deploy (needs the owner's explicit approval)

Root steps are marked **[root]**; the owner runs them.

1. Build images at a clean commit:
   ```sh
   git status --short                       # must be empty
   export LUMA_TAG=$(git rev-parse --short HEAD)
   docker build -t luma-server:$LUMA_TAG server
   docker build -t luma-backup -f deploy/backup.Dockerfile deploy
   ```
2. Create the env file (only if it doesn't exist; nothing ever overwrites it):
   ```sh
   test -e deploy/.env || { cp deploy/.env.example deploy/.env; chmod 600 deploy/.env; }
   openssl rand -hex 32            # → POSTGRES_PASSWORD
   ```
   On **another machine**: `age-keygen -o luma-backup.key` and put the printed `age1…` public key in
   `LUMA_BACKUP_RECIPIENT`. Keep `luma-backup.key` off this server (password manager / offline).
3. **[root]** backup directory: `sudo install -d -m 700 -o root -g root /var/backups/luma`
4. Preflight (read-only): `LUMA_TAG=$LUMA_TAG deploy/preflight.sh`. It must print `preflight passed`.
5. Start: `C up -d --wait`, then check `curl -s http://127.0.0.1:8790/v1/health/ready`
   → `{"migration":4,"status":"ok"}` and `curl -s https://chat.folk3s.com/v1/health/ready`.
6. **[root]** Caddy comment only (the block already routes correctly). Show the diff, keep a backup:
   ```sh
   sudo cp -a /etc/caddy/Caddyfile /etc/caddy/Caddyfile.bak-$(date +%Y%m%d%H%M%S)
   # edit the comment above `http://chat.folk3s.com {` to read `# Luma`
   sudo caddy validate --config /etc/caddy/Caddyfile && sudo systemctl reload caddy
   ```
7. **[root]** Backup timer:
   ```sh
   sudo cp deploy/systemd/luma-backup.{service,timer} /etc/systemd/system/
   sudo systemctl daemon-reload && sudo systemctl enable --now luma-backup.timer
   sudo systemctl start luma-backup.service && sudo ls -l /var/backups/luma   # first backup now
   ```
8. First admin invite: `docker exec luma-luma-server-1 /luma-server admin invite`

## Upgrade

```sh
git pull && git status --short          # clean
export LUMA_TAG=$(git rev-parse --short HEAD)
docker build -t luma-server:$LUMA_TAG server
sudo systemctl start luma-backup.service      # backup before every upgrade
LUMA_TAG=$LUMA_TAG deploy/preflight.sh
C up -d --wait luma-server                    # migrations run at startup under an advisory lock
curl -s http://127.0.0.1:8790/v1/health/ready
```
Clients reconnect and replay from their cursor; nothing is lost across the restart.

## Rollback

Migrations are forward-only (each file's header documents manual rollback SQL, for emergencies only).
The supported rollback is:
1. Note the previous tag: `docker images luma-server`.
2. If the failed version ran a migration: restore the backup taken before the upgrade (below,
   "Restore into production"), then
3. `LUMA_TAG=<previous> C up -d --wait luma-server`.

## Backups

- Manual: `sudo systemctl start luma-backup.service`; status: `systemctl list-timers luma-backup.timer`,
  `journalctl -u luma-backup.service`.
- Each backup is `luma-<UTC time>.dump.age` (pg_dump -Fc), `.uploads.tar.age` and `.sha256`.
  The dump is taken before the uploads tar; every file row in a dump has its file in the tar.
- Integrity: `.sha256` catches corruption; age's authenticated encryption also refuses modified files.

### Test a restore (safe, scratch only)

```sh
LUMA_TAG=$(git rev-parse --short HEAD) deploy/test-restore.sh
```
Seeds `luma_staging` through the API, backs it up, restores into `luma_restore_test`, compares counts,
file hashes and API behaviour, checks a tampered backup is refused, then removes both scratch projects
and only their four volumes, by name. The recorded run is at the end of this file.

To restore a real backup for inspection: `LUMA_TAG=… deploy/restore.sh /var/backups/luma/luma-<time> /path/to/luma-backup.key`
(brings up `luma_restore_test` on 127.0.0.1:8792). Remove it afterwards with
`docker compose -p luma_restore_test -f deploy/compose.yaml down` and
`docker volume rm luma_restore_test_pgdata luma_restore_test_uploads` (check the names twice).

### Restore into production (destructive: owner decision)

1. Take a backup of the current state first, even if it's broken.
2. Verify the backup you'll restore with the scratch restore above.
3. `C stop luma-server`
4. Database: `docker run --rm -i -v /path/key:/key:ro luma-backup age -d -i /key < X.dump.age | docker exec -i luma-luma-postgres-1 pg_restore -U luma -d luma --clean --if-exists --no-owner --exit-on-error`
5. Files: `docker run --rm -i -v /path/key:/key:ro -v luma_uploads:/u luma-backup sh -c 'age -d -i /key | tar -C /u -xpf -' < X.uploads.tar.age`
   (files uploaded after the backup remain on disk as unreferenced files; they are inert).
6. `C start luma-server`; check health. Clients whose cursor is ahead of the restored event log get `resync_required` and do a full resync.

## Rotate secrets

- **Database password:** `docker exec -it luma-luma-postgres-1 psql -U luma` then `\password luma`
  (interactive, so the password stays out of shell history and process lists); set `POSTGRES_PASSWORD`
  in `deploy/.env`; `C up -d --wait luma-server`.
- **Backup key:** make a new key pair off-host, change `LUMA_BACKUP_RECIPIENT`. Keep the old private key until
  the last backup encrypted to it has aged out (14 days).
- **All sessions of one user:** `docker exec luma-luma-server-1 /luma-server admin disable-user <name>` (also
  disables the account; re-enable with `UPDATE users SET disabled_at = NULL WHERE username = '<name>'`).
  Users can revoke their own other sessions in the app.

## Revoke a user

`docker exec luma-luma-server-1 /luma-server admin disable-user <username>`. Revokes every session,
closes live sockets within a second, and makes the account invisible to lookups and adds.

## Logs

- `C logs --since 1h luma-server` (JSON, one line per request: id, route pattern, status, latency,
  user id; never bodies, tokens, query strings or passwords).
- `C logs luma-postgres`; `journalctl -u luma-backup.service`.
- Health: `curl -s http://127.0.0.1:8790/v1/health/ready` (database, migration version, upload dir writable).

## Recorded restore test

Run 2026-10-22 ~12:56 UTC, `LUMA_TAG=02549b5 deploy/test-restore.sh`, image built from commit `02549b5`
(includes all security-review fixes). Compose progress lines omitted; `$WORK` is a throwaway directory.

```
== throwaway age key and staging env ($WORK)

== preflight
preflight for project luma_staging
  ok    env file is mode 600
  ok    POSTGRES_PASSWORD is set and URL-safe
  ok    LUMA_BACKUP_RECIPIENT is an age public key
  ok    image luma-server:02549b5 exists
  ok    image luma-backup exists
  ok    port 127.0.0.1:8791 is free
  ok    volume luma_staging_pgdata does not exist yet and will be created
  ok    volume luma_staging_uploads does not exist yet and will be created
  ok    subnet 172.16.80.0/24 is not used by another network
preflight passed

== up
{"migration":4,"status":"ok"}


== seed through the API
seeded: group 24 messages, dm 5 messages, 3 files; window check ok
trusted proxy: sessions record the forwarded client IP (203.0.113.7)
source:   users=3 conversations=2 members=5 messages=29 files=3 user_events=87 max_seq=24 migration=4
source files on disk: 3

== backup
backup $WORK/backups/luma-20261022T125442Z: 52K
total 52
-rw------- 1 folkes folkes 34398 Oct 22 12:54 luma-20261022T125442Z.dump.age
-rw------- 1 folkes folkes   201 Oct 22 12:54 luma-20261022T125442Z.sha256
-rw------- 1 folkes folkes  9416 Oct 22 12:54 luma-20261022T125442Z.uploads.tar.age
dump is encrypted (no plaintext message bodies)

== restore into luma_restore_test
restored $WORK/backups/luma-20261022T125442Z into luma_restore_test (127.0.0.1:8792)

== compare
restored: users=3 conversations=2 members=5 messages=29 files=3 user_events=87 max_seq=24 migration=4
database counts match
upload files match byte for byte (3 files, sha256)
verified via API: history {'86a00ad6-d22b-42eb-8d6b-e988ab9d83be': 24, 'd28c6bdd-239f-41f2-ade7-26198b988f3b': 5}, 3 files byte-identical, window enforced, search ok

== tampered backup is refused
checksum mismatch detected, nothing restored

RESTORE TEST PASSED

== teardown (scratch projects only)
removed volume luma_staging_pgdata
removed volume luma_staging_uploads
removed volume luma_restore_test_pgdata
removed volume luma_restore_test_uploads
```
