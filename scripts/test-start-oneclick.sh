#!/usr/bin/env bash
set -euo pipefail

output="$(
  bash scripts/start-oneclick.sh --dry-run 2>&1
)"

printf '%s\n' "$output" | grep -F '== start dependencies =='
printf '%s\n' "$output" | grep -F 'docker compose -f deployments/docker-compose/docker-compose.yaml up -d --build mysql clickhouse kafka minio redis'
printf '%s\n' "$output" | grep -F 'xdp.raw.syslog xdp.output.default xdp.deadletter.writer'
printf '%s\n' "$output" | grep -F '== start backend services =='
printf '%s\n' "$output" | grep -F 'xdp-api xdp-worker xdp-writer'
grep -F 'XDP_WRITER_ADDR: :8082' deployments/docker-compose/docker-compose.yaml >/dev/null
if printf '%s\n' "$output" | grep -F 'up -d --build xdp-api xdp-agent xdp-worker xdp-writer' >/dev/null; then
  printf 'unexpected Docker Agent service in backend startup\n' >&2
  exit 1
fi
printf '%s\n' "$output" | grep -F '== start host agent =='
printf '%s\n' "$output" | grep -F 'build/host-bin/xdp-agent'
printf '%s\n' "$output" | grep -F 'XDP_AUTH_ENABLED=true'
printf '%s\n' "$output" | grep -F 'XDP_AGENT_BASE_URL'
grep -F 'wait_process_http agent "$HOST_AGENT_HEALTH_URL" "$HOST_AGENT_PID" "$LOG_DIR/agent.log" 60' scripts/start-oneclick.sh >/dev/null
grep -F 'host agent port in use; switched to' scripts/start-oneclick.sh >/dev/null
printf '%s\n' "$output" | grep -F '== start frontend console =='
printf '%s\n' "$output" | grep -F 'http://127.0.0.1:5173'
printf '%s\n' "$output" | grep -F 'admin / xdp'
grep -F 'saved_searches' scripts/reset-test-env.sh >/dev/null
if grep -E '"s-1"|"s-2"|App stats|Firewall deny' services/api/internal/mvp/product.go >/dev/null; then
  printf 'unexpected demo saved-search seed in product defaults\n' >&2
  exit 1
fi
