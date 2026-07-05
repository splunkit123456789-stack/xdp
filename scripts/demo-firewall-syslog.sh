#!/usr/bin/env bash
set -euo pipefail
cat configs/pipelines/firewall-syslog-pipeline.yaml
printf '\nFirewall sample raw log:\n'
printf 'src=1.1.1.1 dst=8.8.8.8 action=deny bytes=1024\n'
