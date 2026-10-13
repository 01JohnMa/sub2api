#!/usr/bin/env bash
set -Eeuo pipefail

snapshot=${1:?snapshot path required}
if [[ ! "$snapshot" =~ ^/run/cpa-v104-log-offsets-[A-Za-z0-9.-]+$ ]]; then
  printf 'log_snapshot_cleanup_error=invalid_path\n' >&2
  exit 2
fi
rm -f -- "$snapshot"
printf 'log_offset_snapshot=removed\n'
