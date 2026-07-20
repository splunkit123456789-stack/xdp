#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

printf '== syslog pipeline sample ==\n'
cat "$ROOT/configs/pipelines/syslog-collection-pipeline.yaml"
printf '\nSyslog collection sample raw log:\n'
printf 'src=1.1.1.1 dst=8.8.8.8 action=deny bytes=1024\n'
