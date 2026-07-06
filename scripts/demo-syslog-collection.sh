#!/usr/bin/env bash
set -euo pipefail
cat configs/pipelines/syslog-collection-pipeline.yaml
printf '\nSyslog collection sample raw log:\n'
printf 'src=1.1.1.1 dst=8.8.8.8 action=deny bytes=1024\n'
