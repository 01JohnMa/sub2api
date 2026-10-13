#!/usr/bin/env bash
set -Eeuo pipefail

: "${CPA_REMOTE_HOST:?set approved Linux SSH test target}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
switch_script=$script_dir/../switch-process.sh
driver=$script_dir/test-oauth-snapshot-driver.sh
tmpdir=$(mktemp -d /private/tmp/cpa-v104-snapshot-linux-test.XXXXXX)
combined=$tmpdir/combined.sh
trap 'rm -rf -- "$tmpdir"' EXIT

sed -n '/^# BEGIN OAUTH SNAPSHOT FUNCTION$/,/^# END OAUTH SNAPSHOT FUNCTION$/p' "$switch_script" \
  | sed '/^# BEGIN OAUTH SNAPSHOT FUNCTION$/d; /^# END OAUTH SNAPSHOT FUNCTION$/d' > "$combined"
grep -Fq 'prepare_oauth_snapshot()' "$combined"
printf '\n' >> "$combined"
sed -n '3,$p' "$driver" >> "$combined"
chmod 0700 "$combined"

ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s' < "$combined"
