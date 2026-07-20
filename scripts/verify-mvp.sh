#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

ADDR=${XDP_API_ADDR:-127.0.0.1:18080}
BASE=${BASE:-http://$ADDR}
GOCACHE=${GOCACHE:-"$ROOT/.cache/go-build"}
GOMODCACHE=${GOMODCACHE:-"$ROOT/.cache/go-mod"}
GOPATH=${GOPATH:-"$ROOT/.cache/go-path"}
LOG=${XDP_API_LOG:-"$ROOT/.cache/verify-mvp/api.log"}
BIN=${XDP_API_BIN:-"$ROOT/build/verify/xdp-api"}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$1" >&2
    exit 4
  }
}

require_cmd curl
require_cmd go

mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOPATH"
mkdir -p "$(dirname "$BIN")"
rm -f "$LOG"

if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
  printf 'target address already has a healthy API: %s\n' "$BASE" >&2
  printf 'stop the existing service or set XDP_API_ADDR to another host:port.\n' >&2
  exit 1
fi

printf '== test ==\n'
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" GOPATH="$GOPATH" go test ./...

printf '\n== build ==\n'
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" GOPATH="$GOPATH" go build ./cmd/...
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" GOPATH="$GOPATH" go build -o "$BIN" ./cmd/xdp-api

printf '\n== start api ==\n'
XDP_MYSQL_DISABLED=true XDP_API_ADDR="$ADDR" "$BIN" >"$LOG" 2>&1 &
pid=$!

cleanup() {
  kill "$pid" >/dev/null 2>&1 || true
  wait "$pid" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for i in $(seq 1 60); do
  if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
    printf 'api ready: %s\n' "$BASE"
    break
  fi
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    cat "$LOG"
    exit 1
  fi
  if [ "$i" = 60 ]; then
    cat "$LOG"
    exit 1
  fi
  sleep 1
done

printf '\n== acceptance ==\n'
BASE="$BASE" bash "$ROOT/scripts/acceptance.sh"

printf '\nPASS TC-MVP-VERIFY-001 MVP verification passed\n'
