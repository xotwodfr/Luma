# Shared helpers for the deploy scripts. Sourced, not run.
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
project=${LUMA_PROJECT:-luma}
envfile=${LUMA_ENV_FILE:-$here/.env}

die() { echo "error: $*" >&2; exit 1; }

load_env() {
	[ -f "$envfile" ] || die "env file $envfile not found (copy deploy/.env.example)"
	set -a; . "$envfile"; set +a
}

# container ID of a running service in $project, found by compose labels (no compose file needed)
container() {
	docker ps -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=$1"
}

compose() { docker compose -p "$project" --env-file "$envfile" -f "$here/compose.yaml" "$@"; }
