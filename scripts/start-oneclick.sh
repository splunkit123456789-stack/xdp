#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE_FILE="${COMPOSE_FILE:-deployments/docker-compose/docker-compose.yaml}"
CACHE_DIR="${XDP_START_CACHE_DIR:-.cache/xdp-oneclick}"
LOG_DIR="${XDP_START_LOG_DIR:-$CACHE_DIR/logs}"
OVERRIDE_FILE="$CACHE_DIR/docker-compose.auth.yaml"
GO_CACHE_DIR="${XDP_START_GOCACHE:-${GOCACHE:-$ROOT_DIR/$CACHE_DIR/go-build}}"
GO_PATH_DIR="${XDP_START_GOPATH:-$ROOT_DIR/$CACHE_DIR/go-path}"
GO_MOD_CACHE_DIR="${XDP_START_GOMODCACHE:-${GOMODCACHE:-$ROOT_DIR/$CACHE_DIR/go-mod}}"
export BUILDX_CONFIG="${XDP_START_BUILDX_CONFIG:-$ROOT_DIR/$CACHE_DIR/docker-buildx}"
FRONTEND_HOST="${FRONTEND_HOST:-127.0.0.1}"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
API_BASE="${API_BASE:-http://127.0.0.1:8080}"
AUTH_USERNAME="${XDP_AUTH_USERNAME:-admin}"
AUTH_PASSWORD="${XDP_AUTH_PASSWORD:-xdp}"
AUTH_TOKEN="${XDP_API_TOKEN:-xdp-dev-token}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://127.0.0.1:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-xdp}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-xdp}"
HOST_AGENT_BIN="${XDP_HOST_AGENT_BIN:-$ROOT_DIR/build/host-bin/xdp-agent}"
HOST_AGENT_ADDR="${XDP_AGENT_ADDR:-127.0.0.1:8081}"
HOST_AGENT_KAFKA_BROKERS="${XDP_KAFKA_BROKERS:-127.0.0.1:9092}"
HOST_AGENT_CONFIG_API="${XDP_CONFIG_API:-$API_BASE}"
HOST_AGENT_RELOAD_INTERVAL="${XDP_CONFIG_RELOAD_INTERVAL:-2s}"
HOST_AGENT_PORT="${HOST_AGENT_ADDR##*:}"
HOST_AGENT_HEALTH_URL="${XDP_AGENT_HEALTH_URL:-http://127.0.0.1:$HOST_AGENT_PORT/healthz}"
API_AGENT_BASE_URL="${XDP_AGENT_BASE_URL:-http://host.docker.internal:$HOST_AGENT_PORT}"

DRY_RUN=0
STOP_ONLY=0
CLEAN_TEST_ENV=0

usage() {
  cat <<'EOF'
Usage:
  bash scripts/start-oneclick.sh [--dry-run] [--clean] [--stop]

Options:
  --dry-run  Print startup steps without executing commands.
  --clean    Reset ClickHouse event tables, MySQL business config, and Kafka test topics before startup.
  --stop     Stop frontend dev server, host Agent, and Docker Compose services started by this script.

Environment overrides:
  XDP_AUTH_USERNAME=admin
  XDP_AUTH_PASSWORD=xdp
  XDP_API_TOKEN=xdp-dev-token
  FRONTEND_PORT=5173
  XDP_AGENT_ADDR=127.0.0.1:8081
  XDP_KAFKA_BROKERS=127.0.0.1:9092
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      ;;
    --clean)
      CLEAN_TEST_ENV=1
      ;;
    --stop)
      STOP_ONLY=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

print_cmd() {
  printf '%q' "$1"
  shift
  for arg in "$@"; do
    printf ' %q' "$arg"
  done
  printf '\n'
}

run() {
  if [ "$DRY_RUN" = "1" ]; then
    print_cmd "$@"
    return 0
  fi
  "$@"
}

run_env() {
  if [ "$DRY_RUN" = "1" ]; then
    print_cmd env "$@"
    return 0
  fi
  env "$@"
}

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "$command_name" >&2
    exit 1
  fi
}

docker_goarch() {
  local arch
  arch="${XDP_DOCKER_GOARCH:-}"
  if [ -z "$arch" ] && [ "$DRY_RUN" != "1" ] && command -v docker >/dev/null 2>&1; then
    arch="$(docker info --format '{{.Architecture}}' 2>/dev/null || true)"
  fi
  if [ -z "$arch" ] && command -v go >/dev/null 2>&1; then
    arch="$(go env GOARCH 2>/dev/null || true)"
  fi
  case "$arch" in
    amd64|x86_64)
      printf 'amd64'
      ;;
    arm64|aarch64)
      printf 'arm64'
      ;;
    *)
      printf 'arm64'
      ;;
  esac
}

write_compose_override() {
  printf '== write auth compose override ==\n'
  printf 'XDP_AUTH_ENABLED=true\n'
  printf 'XDP_AUTH_USERNAME=%s\n' "$AUTH_USERNAME"
  printf 'XDP_AUTH_PASSWORD=%s\n' "$AUTH_PASSWORD"
  printf 'XDP_API_TOKEN=%s\n' "$AUTH_TOKEN"
  printf 'XDP_AGENT_BASE_URL=%s\n' "$API_AGENT_BASE_URL"
  if [ "$DRY_RUN" = "1" ]; then
    printf 'write %s\n' "$OVERRIDE_FILE"
    return 0
  fi
  mkdir -p "$CACHE_DIR"
  cat >"$OVERRIDE_FILE" <<EOF
services:
  xdp-api:
    environment:
      XDP_AUTH_ENABLED: "true"
      XDP_AUTH_USERNAME: "$AUTH_USERNAME"
      XDP_AUTH_PASSWORD: "$AUTH_PASSWORD"
      XDP_API_TOKEN: "$AUTH_TOKEN"
      XDP_AGENT_BASE_URL: "$API_AGENT_BASE_URL"
  xdp-worker:
    environment:
      XDP_API_TOKEN: "$AUTH_TOKEN"
EOF
}

compose() {
  run docker compose -f "$COMPOSE_FILE" "$@"
}

compose_with_auth() {
  run docker compose -f "$COMPOSE_FILE" -f "$OVERRIDE_FILE" "$@"
}

build_backend_binaries() {
  local arch
  arch="$(docker_goarch)"
  printf '== build backend binaries ==\n'
  run mkdir -p build/docker-bin build/host-bin "$GO_CACHE_DIR" "$GO_PATH_DIR" "$GO_MOD_CACHE_DIR" "$BUILDX_CONFIG"
  run rm -f build/docker-bin/xdp-agent
  run_env GOCACHE="$GO_CACHE_DIR" GOPATH="$GO_PATH_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o build/docker-bin/xdp-api ./cmd/xdp-api
  run_env GOCACHE="$GO_CACHE_DIR" GOPATH="$GO_PATH_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o build/docker-bin/xdp-worker ./cmd/xdp-worker
  run_env GOCACHE="$GO_CACHE_DIR" GOPATH="$GO_PATH_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o build/docker-bin/xdp-writer ./cmd/xdp-writer
  run_env GOCACHE="$GO_CACHE_DIR" GOPATH="$GO_PATH_DIR" GOMODCACHE="$GO_MOD_CACHE_DIR" CGO_ENABLED=0 go build -o "$HOST_AGENT_BIN" ./cmd/xdp-agent
}

wait_http() {
  local name="$1"
  local url="$2"
  local max="${3:-90}"
  local i
  for i in $(seq 1 "$max"); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      printf '%s ready: %s\n' "$name" "$url"
      return 0
    fi
    sleep 1
  done
  printf '%s not ready: %s\n' "$name" "$url" >&2
  return 1
}

wait_kafka() {
  local i
  for i in $(seq 1 90); do
    if docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list >/dev/null 2>&1; then
      printf 'kafka ready\n'
      return 0
    fi
    sleep 1
  done
  printf 'kafka not ready\n' >&2
  docker compose -f "$COMPOSE_FILE" logs --tail=80 kafka >&2 || true
  return 1
}

prepare_kafka_topics() {
  local topic
  printf '== prepare kafka topics ==\n'
  if [ "$DRY_RUN" = "1" ]; then
    printf 'docker compose -f %s exec -T kafka kafka-topics --create xdp.raw.syslog xdp.output.default xdp.deadletter.writer\n' "$COMPOSE_FILE"
    return 0
  fi
  wait_kafka
  for topic in "xdp.raw.syslog" "xdp.output.default" "xdp.deadletter.writer"; do
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
  done
}

reset_test_environment() {
  if [ "$CLEAN_TEST_ENV" != "1" ]; then
    return 0
  fi
  printf '== reset test environment ==\n'
  run_env \
    COMPOSE_FILE="$COMPOSE_FILE" \
    CLICKHOUSE_URL="$CLICKHOUSE_URL" \
    CLICKHOUSE_USER="$CLICKHOUSE_USER" \
    CLICKHOUSE_PASSWORD="$CLICKHOUSE_PASSWORD" \
    bash scripts/reset-test-env.sh
}

start_dependencies() {
  printf '== start dependencies ==\n'
  stop_host_agent
  stop_legacy_agent_containers
  compose stop xdp-api xdp-worker xdp-writer
  compose up -d --build mysql clickhouse kafka minio redis
  if [ "$DRY_RUN" != "1" ]; then
    wait_http clickhouse "$CLICKHOUSE_URL/ping" 90
  fi
}

run_clickhouse_migrations() {
  printf '== migrate clickhouse ==\n'
  run_env CLICKHOUSE_URL="$CLICKHOUSE_URL" CLICKHOUSE_USER="$CLICKHOUSE_USER" CLICKHOUSE_PASSWORD="$CLICKHOUSE_PASSWORD" bash scripts/migrate-clickhouse.sh
}

start_backend_services() {
  printf '== start backend services ==\n'
  compose_with_auth up -d --build xdp-api xdp-worker xdp-writer
  if [ "$DRY_RUN" != "1" ]; then
    wait_http api "$API_BASE/healthz" 90
    curl -fsS "$API_BASE/api/v1/auth" | grep -F '"enabled":true' >/dev/null
  fi
}

stop_host_agent() {
  if [ -f "$CACHE_DIR/agent.pid" ]; then
    local pid
    pid="$(cat "$CACHE_DIR/agent.pid" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" >/dev/null 2>&1; then
      run kill "$pid"
    fi
    run rm -f "$CACHE_DIR/agent.pid"
  fi
}

stop_stale_host_agent_ports() {
  if [ "$DRY_RUN" = "1" ]; then
    printf 'kill stale xdp-agent on %s if present\n' "$HOST_AGENT_ADDR"
    return 0
  fi
  local host_agent_tcp_port pids pid
  host_agent_tcp_port="${HOST_AGENT_ADDR##*:}"
  pids="$(lsof -tiTCP:"$host_agent_tcp_port" -sTCP:LISTEN 2>/dev/null || true)"
  for pid in $pids; do
    if ps -p "$pid" -o command= 2>/dev/null | grep -q 'xdp-agent'; then
      kill "$pid" >/dev/null 2>&1 || true
    fi
  done
}

stop_legacy_agent_containers() {
  if [ "$DRY_RUN" = "1" ]; then
    printf 'docker ps -aq --filter label=com.docker.compose.service=xdp-agent | xargs -r docker rm -f\n'
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1; then
    return 0
  fi
  local containers
  containers="$(docker ps -aq --filter label=com.docker.compose.service=xdp-agent 2>/dev/null || true)"
  if [ -n "$containers" ]; then
    # shellcheck disable=SC2086
    docker rm -f $containers >/dev/null
  fi
}

start_host_agent() {
  printf '== start host agent ==\n'
  stop_host_agent
  stop_stale_host_agent_ports
  stop_legacy_agent_containers
  if [ "$DRY_RUN" = "1" ]; then
    print_cmd env \
      "XDP_AGENT_ADDR=$HOST_AGENT_ADDR" \
      "XDP_KAFKA_BROKERS=$HOST_AGENT_KAFKA_BROKERS" \
      "XDP_CONFIG_API=$HOST_AGENT_CONFIG_API" \
      "XDP_CONFIG_API_TOKEN=$AUTH_TOKEN" \
      "XDP_API_TOKEN=$AUTH_TOKEN" \
      "XDP_CONFIG_RELOAD_INTERVAL=$HOST_AGENT_RELOAD_INTERVAL" \
      "$HOST_AGENT_BIN"
    printf 'write %s\n' "$CACHE_DIR/agent.pid"
    return 0
  fi

  mkdir -p "$LOG_DIR"
  env \
    XDP_AGENT_ADDR="$HOST_AGENT_ADDR" \
    XDP_KAFKA_BROKERS="$HOST_AGENT_KAFKA_BROKERS" \
    XDP_CONFIG_API="$HOST_AGENT_CONFIG_API" \
    XDP_CONFIG_API_TOKEN="$AUTH_TOKEN" \
    XDP_API_TOKEN="$AUTH_TOKEN" \
    XDP_CONFIG_RELOAD_INTERVAL="$HOST_AGENT_RELOAD_INTERVAL" \
    "$HOST_AGENT_BIN" >"$LOG_DIR/agent.log" 2>&1 &
  HOST_AGENT_PID="$!"
  printf '%s\n' "$HOST_AGENT_PID" >"$CACHE_DIR/agent.pid"
  wait_http agent "$HOST_AGENT_HEALTH_URL" 60
}

start_frontend_console() {
  printf '== start frontend console ==\n'
  if [ "$DRY_RUN" = "1" ]; then
    printf 'cd web/console && npm run dev -- --host %s --port %s\n' "$FRONTEND_HOST" "$FRONTEND_PORT"
    printf 'http://127.0.0.1:%s\n' "$FRONTEND_PORT"
    printf 'admin / xdp\n'
    return 0
  fi

  mkdir -p "$LOG_DIR"
  if [ ! -d web/console/node_modules ]; then
    npm --prefix web/console install
  fi

  if [ -f "$CACHE_DIR/frontend.pid" ]; then
    local old_pid
    old_pid="$(cat "$CACHE_DIR/frontend.pid" 2>/dev/null || true)"
    if [ -n "$old_pid" ] && kill -0 "$old_pid" >/dev/null 2>&1; then
      kill "$old_pid" >/dev/null 2>&1 || true
    fi
  fi

  npm --prefix web/console run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT" >"$LOG_DIR/frontend.log" 2>&1 &
  FRONTEND_PID="$!"
  printf '%s\n' "$FRONTEND_PID" >"$CACHE_DIR/frontend.pid"
  wait_http frontend "http://127.0.0.1:$FRONTEND_PORT" 60
}

print_summary() {
  cat <<EOF

XDP one-click stack is running.

Frontend: http://127.0.0.1:$FRONTEND_PORT
API:      $API_BASE
Login:    $AUTH_USERNAME / $AUTH_PASSWORD
Token:    $AUTH_TOKEN

Logs:
  Agent:    $LOG_DIR/agent.log
  Frontend: $LOG_DIR/frontend.log
  Backend:  docker compose -f $COMPOSE_FILE -f $OVERRIDE_FILE logs -f xdp-api xdp-worker xdp-writer

Press Ctrl+C to stop the frontend dev server and host Agent. Docker services keep running.
Run "bash scripts/start-oneclick.sh --stop" to stop the whole local stack.
EOF
}

stop_stack() {
  printf '== stop frontend console ==\n'
  if [ -f "$CACHE_DIR/frontend.pid" ]; then
    local pid
    pid="$(cat "$CACHE_DIR/frontend.pid" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" >/dev/null 2>&1; then
      run kill "$pid"
    fi
    run rm -f "$CACHE_DIR/frontend.pid"
  fi
  printf '== stop host agent ==\n'
  stop_host_agent
  stop_legacy_agent_containers
  printf '== stop docker services ==\n'
  compose stop xdp-api xdp-worker xdp-writer mysql clickhouse kafka minio redis
  run rm -f "$OVERRIDE_FILE"
}

if [ "$STOP_ONLY" = "1" ]; then
  stop_stack
  exit 0
fi

if [ "$DRY_RUN" != "1" ]; then
  require_command docker
  require_command go
  require_command npm
  require_command curl
  docker compose version >/dev/null
  if ! docker info >/dev/null 2>&1; then
    printf 'docker daemon is not running. Start Docker Desktop first, then rerun: bash scripts/start-oneclick.sh\n' >&2
    exit 1
  fi
fi

write_compose_override
build_backend_binaries
start_dependencies
prepare_kafka_topics
reset_test_environment
run_clickhouse_migrations
start_backend_services
start_host_agent
start_frontend_console
print_summary

if [ "$DRY_RUN" != "1" ]; then
  trap 'if [ -n "${FRONTEND_PID:-}" ]; then kill "$FRONTEND_PID" >/dev/null 2>&1 || true; fi; if [ -n "${HOST_AGENT_PID:-}" ]; then kill "$HOST_AGENT_PID" >/dev/null 2>&1 || true; fi' INT TERM EXIT
  wait "$FRONTEND_PID"
fi
