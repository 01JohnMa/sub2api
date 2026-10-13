#!/usr/bin/env bash
set -euo pipefail

config=/Users/lawrence/.codex/config.toml
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source "$script_dir/lib-local.sh"
tmpdir="$(mktemp -d /private/tmp/cpa-v104-log-rate.XXXXXX)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT
emit() { printf '%s=%s\n' "$1" "$2"; }

remote_metrics() {
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s' \
    < "$script_dir/log-metrics-remote.sh"
}

read -r bytes_before files_before lines_before <<< "$(remote_metrics)"

token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
cat > "$tmpdir/request.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 180
header = "Authorization: Bearer $token"
header = "Content-Type: application/json"
EOF
chmod 0600 "$tmpdir/request.curl"
unset token

for n in 1 2 3; do
  jq -n \
    --arg n "$n" \
    --arg body_canary "$CPA_LOG_BODY_CANARY" \
    --arg session_canary "$CPA_LOG_SESSION_CANARY" \
    '{model:"gpt-5.6-sol",input:("Reply with exactly LOG-RATE-"+$n+"-"+$body_canary+"."),stream:false,prompt_cache_key:$session_canary}' \
    > "$tmpdir/request-$n.json"
  status="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/response-$n.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request-$n.json" https://coococode.com/v1/responses)"
  emit "request_${n}_http" "$status"
done

read -r bytes_after files_after lines_after <<< "$(remote_metrics)"
bytes_delta=$((bytes_after - bytes_before))
lines_delta=$((lines_after - lines_before))
if [ "$lines_delta" -gt 0 ]; then
  bytes_per_line=$((bytes_delta / lines_delta))
else
  bytes_per_line=0
fi

emit log_bytes_before "$bytes_before"
emit log_bytes_after "$bytes_after"
emit log_bytes_delta "$bytes_delta"
emit log_file_count_before "$files_before"
emit log_file_count_after "$files_after"
emit nginx_lines_delta "$lines_delta"
emit approximate_log_bytes_per_access_line "$bytes_per_line"
