#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT_DIR/deployments/docker-compose/docker-compose.yaml}"
CLICKHOUSE_URL="${CLICKHOUSE_URL:-http://127.0.0.1:8123}"
CLICKHOUSE_USER="${CLICKHOUSE_USER:-xdp}"
CLICKHOUSE_PASSWORD="${CLICKHOUSE_PASSWORD:-xdp}"
MYSQL_USER="${MYSQL_USER:-xdp}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-xdp}"
MYSQL_DATABASE="${MYSQL_DATABASE:-xdp}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$1" >&2
    exit 4
  }
}

wait_mysql() {
  printf '== wait mysql ==\n'
  local i
  for i in $(seq 1 90); do
    if docker compose -f "$COMPOSE_FILE" exec -T mysql mysqladmin ping -h 127.0.0.1 -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" >/dev/null 2>&1; then
      printf 'mysql ready\n'
      return 0
    fi
    sleep 1
  done
  printf 'mysql not ready\n' >&2
  docker compose -f "$COMPOSE_FILE" logs --tail=80 mysql >&2 || true
  return 1
}

reset_clickhouse() {
  printf '== reset clickhouse event tables ==\n'
  python3 - <<'PY'
import base64
import os
import urllib.request

url = os.environ.get("CLICKHOUSE_URL", "http://127.0.0.1:8123") + "/"
user = os.environ.get("CLICKHOUSE_USER", "")
password = os.environ.get("CLICKHOUSE_PASSWORD", "")

def execute(sql):
    req = urllib.request.Request(url, data=sql.encode(), method="POST")
    if user or password:
        req.add_header("Authorization", "Basic " + base64.b64encode(f"{user}:{password}".encode()).decode())
    return urllib.request.urlopen(req, timeout=10).read().decode()

execute("CREATE DATABASE IF NOT EXISTS xdp")
tables = execute(
    "SELECT name FROM system.tables "
    "WHERE database = 'xdp' AND startsWith(name, 'events_') FORMAT TSV"
).splitlines()
for table in tables:
    if table and all(ch.isalnum() or ch == "_" for ch in table):
        execute(f"DROP TABLE IF EXISTS xdp.{table}")
print("clickhouse reset ok", len(tables))
PY
}

reset_mysql() {
  printf '== reset mysql business tables ==\n'
  local tables=(
    data_source_runtime_states
    data_sources
    parse_rules
    saved_searches
    search_command_execution_audits
    indexes
    parser_plugins
    pipeline_versions
    pipelines
    plugin_versions
    plugins
    deadletter_records
    auth_audit_logs
  )
  local table_names
  table_names="$(printf "'%s'," "${tables[@]}")"
  table_names="${table_names%,}"
  local existing_tables
  existing_tables="$(docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -N -B -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" \
    -e "SELECT table_name FROM information_schema.tables WHERE table_schema = '$MYSQL_DATABASE' AND table_name IN ($table_names)")"

  local reset_sql="SET FOREIGN_KEY_CHECKS=0;" table
  for table in "${tables[@]}"; do
    if printf '%s\n' "$existing_tables" | grep -Fxq "$table"; then
      reset_sql="${reset_sql}
TRUNCATE TABLE \`$table\`;"
    fi
  done
  reset_sql="${reset_sql}
SET FOREIGN_KEY_CHECKS=1;"
  docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" <<<"$reset_sql"

  local auth_tables=(
    auth_users
    auth_tokens
    auth_roles
    auth_role_permissions
    auth_user_roles
    auth_role_index_scopes
    auth_role_plugin_scopes
  )
  local auth_table_names
  auth_table_names="$(printf "'%s'," "${auth_tables[@]}")"
  auth_table_names="${auth_table_names%,}"
  local existing_auth_tables
  existing_auth_tables="$(docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -N -B -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" \
    -e "SELECT table_name FROM information_schema.tables WHERE table_schema = '$MYSQL_DATABASE' AND table_name IN ($auth_table_names)")"
  local auth_table
  local auth_tables_ready=1
  for auth_table in "${auth_tables[@]}"; do
    if ! printf '%s\n' "$existing_auth_tables" | grep -Fxq "$auth_table"; then
      auth_tables_ready=0
      break
    fi
  done
  if [[ "$auth_tables_ready" -eq 1 ]]; then
    local rbac_acceptance_cleanup_sql
    rbac_acceptance_cleanup_sql="
SET FOREIGN_KEY_CHECKS=0;
DELETE ur FROM auth_user_roles ur JOIN auth_users u ON u.id = ur.user_id WHERE u.username LIKE 'accept_p2_rbac_%';
DELETE ur FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id WHERE r.builtin = 0 AND (r.role_code LIKE 'accept_p2_rbac_%' OR r.role_name IN ('P2 RBAC 受限搜索', 'P2 RBAC 采集只读'));
DELETE rp FROM auth_role_permissions rp JOIN auth_roles r ON r.id = rp.role_id WHERE r.builtin = 0 AND (r.role_code LIKE 'accept_p2_rbac_%' OR r.role_name IN ('P2 RBAC 受限搜索', 'P2 RBAC 采集只读'));
DELETE ris FROM auth_role_index_scopes ris JOIN auth_roles r ON r.id = ris.role_id WHERE r.builtin = 0 AND (r.role_code LIKE 'accept_p2_rbac_%' OR r.role_name IN ('P2 RBAC 受限搜索', 'P2 RBAC 采集只读'));
DELETE rps FROM auth_role_plugin_scopes rps JOIN auth_roles r ON r.id = rps.role_id WHERE r.builtin = 0 AND (r.role_code LIKE 'accept_p2_rbac_%' OR r.role_name IN ('P2 RBAC 受限搜索', 'P2 RBAC 采集只读'));
UPDATE auth_tokens tok JOIN auth_users u ON u.id = tok.user_id SET tok.status = 'revoked', tok.revoked_at = CURRENT_TIMESTAMP(3), tok.updated_at = CURRENT_TIMESTAMP(3) WHERE u.username LIKE 'accept_p2_rbac_%' AND tok.status <> 'revoked';
UPDATE auth_users SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE username LIKE 'accept_p2_rbac_%' AND deleted_at IS NULL;
UPDATE auth_roles SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE builtin = 0 AND (role_code LIKE 'accept_p2_rbac_%' OR role_name IN ('P2 RBAC 受限搜索', 'P2 RBAC 采集只读')) AND deleted_at IS NULL;
SET FOREIGN_KEY_CHECKS=1;"
    docker compose -f "$COMPOSE_FILE" exec -T mysql mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE" <<<"$rbac_acceptance_cleanup_sql"
  fi
  printf 'mysql reset ok\n'
}

wait_topic_deleted() {
  local topic="$1"
  local i
  for i in $(seq 1 30); do
    if ! docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --list 2>/dev/null | grep -Fxq "$topic"; then
      return 0
    fi
    sleep 1
  done
  printf 'warn: kafka topic still exists after delete timeout: %s\n' "$topic" >&2
}

reset_kafka() {
  printf '== reset kafka topics ==\n'
  local topic
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer raw.ds_e2e_syslog_source; do
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --delete --if-exists --topic "$topic" >/dev/null 2>&1 || true
  done
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer raw.ds_e2e_syslog_source; do
    wait_topic_deleted "$topic"
  done
  for topic in xdp.raw.syslog xdp.output.default xdp.deadletter.writer; do
    docker compose -f "$COMPOSE_FILE" exec -T kafka /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
  done
}

require_cmd docker
require_cmd python3

export CLICKHOUSE_URL CLICKHOUSE_USER CLICKHOUSE_PASSWORD

reset_clickhouse
wait_mysql
reset_mysql
reset_kafka
printf 'test environment reset complete.\n'
