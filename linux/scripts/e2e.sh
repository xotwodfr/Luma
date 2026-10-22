#!/usr/bin/env bash
# Runs the Linux client's end-to-end test against a real luma-server on a throwaway database
# in the disposable luma-test-pg container. Never touches production.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${LUMA_E2E_PORT:-8798}
DB=luma_e2e_$(date +%s)_$RANDOM
TMP=$(mktemp -d)

cleanup() {
    [[ -n ${PID:-} ]] && kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null || true
    docker exec luma-test-pg psql -U postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
    rm -rf "$TMP"
}
trap cleanup EXIT

make -s -C "$ROOT/server" testdb-up
(cd "$ROOT/server" && go build -o "$TMP/luma-server" ./cmd/luma-server)
docker exec luma-test-pg psql -U postgres -qc "CREATE DATABASE $DB"

export LUMA_DATABASE_URL="postgres://postgres:test@127.0.0.1:55432/$DB?sslmode=disable"
export LUMA_LISTEN="127.0.0.1:$PORT" LUMA_UPLOAD_DIR="$TMP/uploads"
"$TMP/luma-server" >"$TMP/server.log" 2>&1 &
PID=$!
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$PORT/v1/health/ready" >/dev/null && break; sleep 0.1; done

INVITES=$(for _ in 1 2 3; do "$TMP/luma-server" admin invite; done | paste -sd,)
cmake --build "$ROOT/linux/build" --target tst_e2e >/dev/null
LUMA_E2E_URL="http://127.0.0.1:$PORT" LUMA_E2E_INVITES="$INVITES" QT_QPA_PLATFORM=offscreen \
    "$ROOT/linux/build/tst_e2e" "$@" || { echo "--- server log ---"; tail -20 "$TMP/server.log"; exit 1; }
