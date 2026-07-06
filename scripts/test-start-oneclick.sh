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
if printf '%s\n' "$output" | grep -F 'up -d --build xdp-api xdp-agent xdp-worker xdp-writer' >/dev/null; then
  printf 'unexpected Docker Agent service in backend startup\n' >&2
  exit 1
fi
printf '%s\n' "$output" | grep -F '== start host agent =='
printf '%s\n' "$output" | grep -F 'build/host-bin/xdp-agent'
printf '%s\n' "$output" | grep -F 'XDP_AUTH_ENABLED=true'
printf '%s\n' "$output" | grep -F 'XDP_AGENT_BASE_URL'
printf '%s\n' "$output" | grep -F '== start frontend console =='
printf '%s\n' "$output" | grep -F 'http://127.0.0.1:5173'
printf '%s\n' "$output" | grep -F 'admin / xdp'
