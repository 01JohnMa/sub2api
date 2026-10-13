#!/usr/bin/env bash
set -Eeuo pipefail

snapshot=${1:?snapshot path required}
if [[ ! "$snapshot" =~ ^/run/cpa-v104-log-offsets-[A-Za-z0-9.-]+$ ]]; then
  printf 'log_snapshot_error=invalid_path\n' >&2
  exit 2
fi
test ! -e "$snapshot"
install -o root -g root -m 0600 /dev/null "$snapshot"

find /opt/cpa/logs -maxdepth 1 -type f -print0 \
  | while IFS= read -r -d '' path; do
      printf '%s\t%s\t%s\n' "$(stat -c '%a' "$path")" "$(stat -c '%s' "$path")" "$path"
    done > "$snapshot"

printf 'log_offset_snapshot=created\n'
