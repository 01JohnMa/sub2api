#!/usr/bin/env bash
set -Eeuo pipefail

: "${CPA_REMOTE_HOST:?set approved production SSH target}"
: "${CPA_BASELINE_FILE:?set root-only local baseline output path}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/lib-local.sh"
umask 077
tmp=$(mktemp "${CPA_BASELINE_FILE}.XXXXXX")
trap 'rm -f -- "$tmp"' EXIT

for window in 15 30; do
  snapshot=$(mktemp "${CPA_BASELINE_FILE}.${window}.XXXXXX")
  run_with_timeout 45 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" "bash -s -- $window" \
    < "$script_dir/observe-remote.sh" > "$snapshot"
  printf 'nginx_%s=' "$window" >> "$tmp"
  sed -n '1p' "$snapshot" >> "$tmp"
  printf 'usage_%s=' "$window" >> "$tmp"
  sed -n '2p' "$snapshot" >> "$tmp"
  printf 'errors_%s=' "$window" >> "$tmp"
  sed -n '3p' "$snapshot" >> "$tmp"
  rm -f -- "$snapshot"
done

test "$(wc -l < "$tmp" | tr -d ' ')" = 6
chmod 0600 "$tmp"
mv -f "$tmp" "$CPA_BASELINE_FILE"
trap - EXIT
printf 'pre_switch_baseline=collected\n'
