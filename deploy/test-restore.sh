#!/usr/bin/env bash
# End-to-end proof that backups restore: runs scratch project luma_staging (127.0.0.1:8791),
# seeds it through the API, backs it up, restores into luma_restore_test (127.0.0.1:8792),
# compares the two, then removes both scratch projects and only their volumes, by name.
# Touches nothing belonging to project luma. Output is recorded in RUNBOOK.md.
set -euo pipefail
umask 077
here=$(cd "$(dirname "$0")" && pwd)
work=${LUMA_TEST_WORKDIR:-$(mktemp -d)}
mkdir -p "$work/backups"
export LUMA_TAG=${LUMA_TAG:?set LUMA_TAG to the server image tag}
step() { printf '\n== %s\n' "$*"; }

teardown() {
	step "teardown (scratch projects only)"
	for p in luma_staging luma_restore_test; do
		[ "$p" != luma ] || exit 99
		LUMA_PROJECT=$p LUMA_ENV_FILE=$work/staging.env bash -c '. '"$here"'/lib.sh; compose down --remove-orphans' 2>&1 | tail -1
		for v in pgdata uploads; do
			docker volume rm "${p}_$v" >/dev/null 2>&1 && echo "removed volume ${p}_$v" || true
		done
	done
}
trap teardown EXIT

step "throwaway age key and staging env ($work)"
docker run --rm luma-backup age-keygen >"$work/backup.key" 2>/dev/null
cat >"$work/staging.env" <<ENV
POSTGRES_PASSWORD=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
LUMA_BACKUP_RECIPIENT=$(docker run --rm -i luma-backup age-keygen -y <"$work/backup.key")
LUMA_PORT=8791
LUMA_SUBNET=172.16.80.0/24
LUMA_GATEWAY=172.16.80.1
LUMA_PUBLIC_URL=http://127.0.0.1:8791
ENV
export LUMA_PROJECT=luma_staging LUMA_ENV_FILE=$work/staging.env LUMA_BACKUP_DIR=$work/backups
. "$here/lib.sh"

step "preflight"
"$here/preflight.sh"

step "up"
compose up -d --wait 2>&1 | tail -3
curl -fsS http://127.0.0.1:8791/v1/health/ready; echo

step "seed through the API"
srv=$(container luma-server)
invites=$(for _ in 1 2 3; do docker exec "$srv" /luma-server admin invite 2>/dev/null | tail -1; done)
# shellcheck disable=SC2086
python3 "$here/smoke.py" seed http://127.0.0.1:8791 "$work/state.json" $invites
pg=$(container luma-postgres)
ip=$(docker exec "$pg" psql -U luma -d luma -tAc "SELECT DISTINCT host(ip) FROM sessions WHERE device_name = 'smoke'")
[ "$ip" = 203.0.113.7 ] && echo "trusted proxy: sessions record the forwarded client IP ($ip)" || { echo "trusted proxy FAILED: $ip"; exit 1; }

counts() {
	docker exec "$1" psql -U luma -d luma -tAc "SELECT 'users='||(SELECT count(*) FROM users)||' conversations='||(SELECT count(*) FROM conversations)
		||' members='||(SELECT count(*) FROM members)||' messages='||(SELECT count(*) FROM messages)||' files='||(SELECT count(*) FROM files)
		||' user_events='||(SELECT count(*) FROM user_events)||' max_seq='||(SELECT max(last_seq) FROM conversations)
		||' migration='||(SELECT max(version) FROM schema_migrations)"
}
files_on_disk() {
	docker run --rm -v "$1_uploads:/u:ro" luma-backup sh -c 'cd /u && find . -path ./tmp -prune -o -type f -print | sort | xargs -r sha256sum'
}
before=$(counts "$pg")
echo "source:   $before"
files_on_disk luma_staging >"$work/files.before"
echo "source files on disk: $(wc -l <"$work/files.before")"

step "backup"
"$here/backup.sh"
base=$(ls "$work/backups"/luma-*.sha256 | sed 's/\.sha256$//')
ls -l "$work/backups"
if docker run --rm -i luma-backup sh -c 'cat >/tmp/f; ! grep -q "group message" /tmp/f' <"$base.dump.age"; then
	echo "dump is encrypted (no plaintext message bodies)"
else
	echo "dump contains plaintext!"; exit 1
fi

step "restore into luma_restore_test"
LUMA_ENV_FILE=$work/staging.env "$here/restore.sh" "$base" "$work/backup.key"

step "compare"
LUMA_PROJECT=luma_restore_test
after=$(counts "$(container luma-postgres)")
echo "restored: $after"
files_on_disk luma_restore_test >"$work/files.after"
[ "$before" = "$after" ] && echo "database counts match" || { echo "COUNTS DIFFER"; exit 1; }
cmp -s "$work/files.before" "$work/files.after" && echo "upload files match byte for byte ($(wc -l <"$work/files.after") files, sha256)" \
	|| { diff "$work/files.before" "$work/files.after"; exit 1; }
python3 "$here/smoke.py" verify http://127.0.0.1:8792 "$work/state.json"

step "tampered backup is refused"
cp "$base.dump.age" "$work/orig" && printf 'x' >>"$base.dump.age"
out=$(LUMA_RESTORE_PROJECT=luma_restore_never "$here/restore.sh" "$base" "$work/backup.key" 2>&1) && { echo "tampered restore succeeded!"; exit 1; }
if [[ $out == *"checksum mismatch"* ]] && ! docker volume inspect luma_restore_never_pgdata >/dev/null 2>&1; then
	echo "checksum mismatch detected, nothing restored"
else
	echo "tampering NOT detected"; exit 1
fi
mv "$work/orig" "$base.dump.age"
echo; echo "RESTORE TEST PASSED"
