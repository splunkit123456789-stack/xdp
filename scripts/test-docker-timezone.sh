#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CACHE_DIR="${XDP_TEST_CACHE_DIR:-.cache/xdp-tests}"
GO_CACHE_DIR="${GOCACHE:-$ROOT_DIR/$CACHE_DIR/go-build}"
GO_PATH_DIR="${XDP_TEST_GOPATH:-$ROOT_DIR/$CACHE_DIR/go-path}"
GO_MOD_CACHE_DIR="${GOMODCACHE:-$ROOT_DIR/$CACHE_DIR/go-mod}"
ARCH="${XDP_DOCKER_GOARCH:-$(docker info --format '{{.Architecture}}' 2>/dev/null || go env GOARCH)}"

case "$ARCH" in
  amd64|x86_64)
    ARCH=amd64
    ;;
  arm64|aarch64)
    ARCH=arm64
    ;;
  *)
    ARCH=arm64
    ;;
esac

mkdir -p build/docker-bin "$GO_CACHE_DIR" "$GO_PATH_DIR" "$GO_MOD_CACHE_DIR"
env GOCACHE="$GO_CACHE_DIR" GOPATH="$GO_PATH_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -o build/docker-bin/xdp-api ./cmd/xdp-api

container_id="$(
  docker run -d \
    -p 18080:8080 \
    -e XDP_AUTH_ENABLED=false \
    -e XDP_MYSQL_DISABLED=true \
    -v "$ROOT_DIR/build/docker-bin/xdp-api:/usr/local/bin/xdp-api:ro" \
    alpine:3.17 \
    /usr/local/bin/xdp-api
)"

cleanup() {
  docker rm -f "$container_id" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:18080/healthz >/dev/null 2>&1; then
    printf 'docker timezone smoke test passed\n'
    exit 0
  fi
  if ! docker ps -q --no-trunc | grep -F "$container_id" >/dev/null; then
    docker logs "$container_id" >&2 || true
    exit 1
  fi
  sleep 1
done

docker logs "$container_id" >&2 || true
printf 'xdp-api did not become healthy in alpine\n' >&2
exit 1
