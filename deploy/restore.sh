#!/usr/bin/env bash
# Restore a backup into a scratch project (default luma_restore_test), never into production.
#   restore.sh <backup-dir>/luma-<timestamp> <age identity file>
# The scratch project gets a throwaway env file (port 8792, subnet 172.16.81.0/24).
# Bring it into service only by following RUNBOOK.md "Restore into production".
set -euo pipefail
umask 077
. "$(dirname "$0")/lib.sh"
base=${1:?usage: restore.sh <backup base path> <age identity file>}
identity=$(realpath "${2:?usage: restore.sh <backup base path> <age identity file>}")
project=${LUMA_RESTORE_PROJECT:-luma_restore_test}
[ "$project" != luma ] || die "refusing to restore over the production project"
[ -n "${LUMA_TAG:-}" ] || die "set LUMA_TAG to the server image tag to start"

(cd "$(dirname "$base")" && sha256sum -c --quiet "$(basename "$base").sha256") || die "checksum mismatch"
envfile=$(mktemp)
trap 'rm -f "$envfile"' EXIT
cat >"$envfile" <<ENV
POSTGRES_PASSWORD=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
LUMA_PORT=${LUMA_RESTORE_PORT:-8792}
LUMA_SUBNET=172.16.81.0/24
LUMA_GATEWAY=172.16.81.1
ENV

for v in pgdata uploads; do
	docker volume inspect "${project}_$v" >/dev/null 2>&1 && die "volume ${project}_$v already exists; restore needs an empty scratch project"
done
compose create --quiet-pull
compose start luma-postgres
# Over TCP: the image's first-run init server listens only on the socket, then restarts.
until docker exec "$(container luma-postgres)" pg_isready -h 127.0.0.1 -U luma -d luma -q; do sleep 1; done

docker run --rm -i -v "$identity:/key:ro" luma-backup age -d -i /key <"$base.dump.age" |
	docker exec -i "$(container luma-postgres)" pg_restore -U luma -d luma --no-owner --exit-on-error
docker run --rm -i -v "$identity:/key:ro" -v "${project}_uploads:/u" luma-backup \
	sh -c 'age -d -i /key | tar -C /u -xpf -' <"$base.uploads.tar.age"
compose up -d --wait luma-server
echo "restored $base into $project (127.0.0.1:${LUMA_RESTORE_PORT:-8792})"
