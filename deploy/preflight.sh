#!/usr/bin/env bash
# Read-only checks before `docker compose up` for a Luma project. Changes nothing.
#   LUMA_PROJECT (luma)  LUMA_ENV_FILE (deploy/.env)  LUMA_TAG (image tag to deploy)
set -euo pipefail
. "$(dirname "$0")/lib.sh"
fail=0
ok() { printf '  ok    %s\n' "$*"; }
bad() { printf '  FAIL  %s\n' "$*"; fail=1; }
echo "preflight for project $project"

[ -f "$envfile" ] || die "env file $envfile not found"
mode=$(stat -c %a "$envfile")
[ "$mode" = 600 ] && ok "env file is mode 600" || bad "env file $envfile is mode $mode, want 600"
load_env
[[ ${POSTGRES_PASSWORD:-} =~ ^[A-Za-z0-9_-]{24,}$ ]] && ok "POSTGRES_PASSWORD is set and URL-safe" || bad "POSTGRES_PASSWORD must be 24+ URL-safe characters (openssl rand -hex 32)"
[[ ${LUMA_BACKUP_RECIPIENT:-} == age1* ]] && ok "LUMA_BACKUP_RECIPIENT is an age public key" || bad "LUMA_BACKUP_RECIPIENT must be an age1… public key"
port=${LUMA_PORT:-8790}
subnet=${LUMA_SUBNET:-172.16.79.0/24}

if [ -n "${LUMA_TAG:-}" ] && docker image inspect "luma-server:$LUMA_TAG" >/dev/null 2>&1; then
	ok "image luma-server:$LUMA_TAG exists"
else
	bad "image luma-server:${LUMA_TAG:-<LUMA_TAG unset>} not found"
fi
docker image inspect luma-backup >/dev/null 2>&1 && ok "image luma-backup exists" || bad "image luma-backup not found (see backup.Dockerfile)"

if ss -ltnH "sport = :$port" | grep -q .; then
	[ -n "$(container luma-server)" ] && ok "port $port is held by $project's own server (upgrade)" || bad "port $port is in use by something else"
else
	ok "port 127.0.0.1:$port is free"
fi

for v in pgdata uploads; do
	name=${project}_$v
	if docker volume inspect "$name" >/dev/null 2>&1; then
		owner=$(docker volume inspect -f '{{index .Labels "com.docker.compose.project"}}' "$name")
		[ "$owner" = "$project" ] && ok "volume $name exists, belongs to $project, will be reused (never recreated)" \
			|| bad "volume $name exists but was not created by compose project $project"
	else
		ok "volume $name does not exist yet and will be created"
	fi
done

clash=$(docker network ls -q | xargs docker network inspect -f '{{.Name}} {{range .IPAM.Config}}{{.Subnet}} {{end}}' |
	awk -v s="$subnet" -v own="${project}_default" '$1 != own { for (i = 2; i <= NF; i++) if ($i == s) print $1 }')
[ -z "$clash" ] && ok "subnet $subnet is not used by another network" || bad "subnet $subnet is already used by: $clash"

if [ "$project" = luma ]; then
	if grep -A3 '^http://chat.folk3s.com' /etc/caddy/Caddyfile | grep -q "reverse_proxy 127.0.0.1:$port"; then
		ok "Caddy routes chat.folk3s.com to 127.0.0.1:$port"
	else
		bad "Caddy block for chat.folk3s.com → 127.0.0.1:$port not found"
	fi
	d=${LUMA_BACKUP_DIR:-/var/backups/luma}
	[ -d "$d" ] && [ "$(stat -c %a "$d")" = 700 ] && ok "backup dir $d exists, mode 700" \
		|| bad "backup dir $d missing or not mode 700 (sudo install -d -m 700 $d)"
fi
[ $fail = 0 ] && echo "preflight passed" || echo "preflight FAILED"
exit $fail
