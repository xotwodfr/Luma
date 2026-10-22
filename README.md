# Luma

A small, invite-only messenger: Go server (`server/`), native clients (`linux/` first), shared design
tokens (`design/`) and protocol contract (`protocol/`). Deployment and operations: `deploy/RUNBOOK.md`.
Security: `docs/security/threat-model.md`.

## Privacy, plainly

- **Messages are not end-to-end encrypted.** They are encrypted in transit (TLS to Cloudflare, then the
  tunnel to this server), but the server operator can read messages and files.
- Passwords are stored only as Argon2id hashes; session tokens and invite codes only as SHA-256 hashes.
- The server's disk is not encrypted at rest. Backups are encrypted with `age` to a key kept off the server.
- Former group members keep what they saw while they were members and nothing newer.
