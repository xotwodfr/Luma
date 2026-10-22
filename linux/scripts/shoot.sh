#!/usr/bin/env bash
# Seeds a throwaway server with the preview's conversations and screenshots the Linux client
# headlessly (light/dark, three-pane and two-pane, sign-in). Output: linux/shots/*.png
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${LUMA_DEMO_PORT:-8797}
DB=luma_demo_$(date +%s)_$RANDOM
TMP=$(mktemp -d)
OUT="$ROOT/linux/shots"
PIDS=()

cleanup() {
    for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
    wait 2>/dev/null || true
    docker exec luma-test-pg psql -U postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
    rm -rf "$TMP"
}
trap cleanup EXIT

make -s -C "$ROOT/server" testdb-up
(cd "$ROOT/server" && go build -o "$TMP/luma-server" ./cmd/luma-server)
cmake --build "$ROOT/linux/build" --target luma >/dev/null
docker exec luma-test-pg psql -U postgres -qc "CREATE DATABASE $DB"
export LUMA_DATABASE_URL="postgres://postgres:test@127.0.0.1:55432/$DB?sslmode=disable"
LUMA_LISTEN="127.0.0.1:$PORT" LUMA_UPLOAD_DIR="$TMP/uploads" "$TMP/luma-server" >"$TMP/server.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 50); do curl -sf "http://127.0.0.1:$PORT/v1/health/ready" >/dev/null && break; sleep 0.1; done

for _ in $(seq 7); do "$TMP/luma-server" admin invite; done >"$TMP/invites"
python3 "$ROOT/linux/scripts/seed-demo.py" "http://127.0.0.1:$PORT" "$TMP/invites" "$TMP"
docker exec -i luma-test-pg psql -U postgres -d "$DB" -q <"$TMP/backdate.sql"

export QT_QPA_PLATFORM=offscreen QT_QUICK_BACKEND=software LUMA_SERVER="http://127.0.0.1:$PORT"
# Mira and Elif are online: real headless clients holding sockets, so presence is genuine.
for who in mira elif; do
    LUMA_INSECURE_TOKEN_FILE="$TMP/session-$who.json" "$ROOT/linux/build/luma" --profile "$TMP/p-$who" >/dev/null 2>&1 &
    PIDS+=($!)
done
sleep 1.5

mkdir -p "$OUT"
shot() { # name theme size [open-with]
    local args=(--profile "$TMP/p-$1" --theme "$2" --size "$3" --screenshot "$OUT/$1.png" --after-ms 2500)
    [[ -n ${4:-} ]] && args+=(--open-with "$4")
    LUMA_INSECURE_TOKEN_FILE="$TMP/session-folkes.json" "$ROOT/linux/build/luma" "${args[@]}" 2>/dev/null
    echo "$OUT/$1.png"
}
cp "$TMP/session-folkes.json" "$TMP/keep.json"
shot linux-three-pane-light light 1280x800 elif
shot linux-three-pane-dark dark 1280x800 mira
shot linux-two-pane-light light 960x720 mira
shot linux-two-pane-dark dark 960x720 elif
shot linux-one-pane-light light 480x760 ""
shot linux-theo-receipts-light light 960x560 theo
LUMA_INSECURE_TOKEN_FILE="$TMP/none.json" "$ROOT/linux/build/luma" --profile "$TMP/p-signin" --theme dark --size 1100x720 \
    --screenshot "$OUT/linux-sign-in-dark.png" --after-ms 800 2>/dev/null && echo "$OUT/linux-sign-in-dark.png"
