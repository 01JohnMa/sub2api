#!/usr/bin/env bash
set -Eeuo pipefail

bytes=$(find /opt/cpa/logs -maxdepth 1 -type f -printf '%s\n' 2>/dev/null | awk '{s+=$1} END {print s+0}')
files=$(find /opt/cpa/logs -maxdepth 1 -type f 2>/dev/null | wc -l | tr -d ' ')
lines=$(wc -l < /var/log/nginx/access.log)
printf '%s %s %s\n' "$bytes" "$files" "$lines"
