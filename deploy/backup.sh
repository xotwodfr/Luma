#!/usr/bin/env bash
# Encrypted backup of a running Luma project: pg_dump -Fc and a tar of the uploads volume,
# each encrypted with age to LUMA_BACKUP_RECIPIENT, plus SHA-256 checksums. Keeps 14 days.
#   LUMA_PROJECT (luma)  LUMA_ENV_FILE (deploy/.env)  LUMA_BACKUP_DIR (/var/backups/luma)
set -euo pipefail
umask 077
. "$(dirname "$0")/lib.sh"
load_env
dir=${LUMA_BACKUP_DIR:-/var/backups/luma}
[[ ${LUMA_BACKUP_RECIPIENT:-} == age1* ]] || die "LUMA_BACKUP_RECIPIENT is not set"
[ -d "$dir" ] || die "backup dir $dir does not exist"
pg=$(container luma-postgres)
[ -n "$pg" ] || die "luma-postgres is not running in project $project"

base=luma-$(date -u +%Y%m%dT%H%M%SZ)
cd "$dir"
# Database first, then files. A file is fsynced before its row commits and attached files are
# never deleted, so every file row in this dump has its file in the tar taken afterwards.
docker exec "$pg" pg_dump -Fc -U luma luma |
	docker run --rm -i luma-backup age -r "$LUMA_BACKUP_RECIPIENT" >"$base.dump.age.part"
docker run --rm -v "${project}_uploads:/u:ro" luma-backup \
	sh -c 'tar -C /u --exclude=./tmp -cf - . | age -r "$0"' "$LUMA_BACKUP_RECIPIENT" >"$base.uploads.tar.age.part"
mv "$base.dump.age.part" "$base.dump.age"
mv "$base.uploads.tar.age.part" "$base.uploads.tar.age"
sha256sum "$base.dump.age" "$base.uploads.tar.age" >"$base.sha256"
find "$dir" -maxdepth 1 -name 'luma-*' -mtime +13 -delete
echo "backup $dir/$base: $(du -ch "$base".* | tail -1 | cut -f1)"
