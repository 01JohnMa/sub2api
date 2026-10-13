#!/usr/bin/env bash
set -Eeuo pipefail

: "${CPA_REMOTE_HOST:?set approved production SSH target}"
config=${CPA_CODEX_CONFIG:-/Users/lawrence/.codex/config.toml}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source "$script_dir/lib-local.sh"
marker="cpa-v104-post-switch-$(date -u +%Y%m%dT%H%M%SZ)-$$"
tmpdir=$(mktemp -d /private/tmp/cpa-v104-billing.XXXXXX)
cleanup() { rm -rf -- "$tmpdir"; }
trap cleanup EXIT

fail() {
  printf 'billing_validation_error=%s\n' "$1" >&2
  exit 1
}

token=$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")
test -n "$token" || fail bearer_token_missing
cat > "$tmpdir/request.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 180
header = "Authorization: Bearer $token"
header = "Content-Type: application/json"
header = "User-Agent: $marker"
EOF
chmod 0600 "$tmpdir/request.curl"
unset token

jq -n '{model:"gpt-5.6-sol",input:"Reply with exactly BILLING-VALIDATION.",stream:false,prompt_cache_key:"cpa-v104-post-switch-billing"}' > "$tmpdir/request.json"
status=$(curl --config "$tmpdir/request.curl" -o "$tmpdir/response.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/request.json" https://coococode.com/v1/responses)
test "$status" = 200 || fail "http_$status"
test "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/response.json")" = true || fail empty_output

response_input=$(jq -r '.usage.input_tokens // 0' "$tmpdir/response.json")
response_cached=$(jq -r '.usage.input_tokens_details.cached_tokens // 0' "$tmpdir/response.json")
response_output=$(jq -r '.usage.output_tokens // 0' "$tmpdir/response.json")
test "$response_input" -gt 0 || fail response_input_missing
test "$response_output" -gt 0 || fail response_output_missing

query='{}'
for _ in $(seq 1 15); do
  query=$(run_with_timeout 45 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" "bash -s -- $marker" \
    < "$script_dir/billing-query-remote.sh")
  if test "$(printf '%s' "$query" | jq -r '.usage_rows')" = 1; then
    break
  fi
  sleep 2
done

test "$(printf '%s' "$query" | jq -r '.usage_rows')" = 1 || fail usage_row_count
test "$(printf '%s' "$query" | jq -r '.distinct_request_ids')" = 1 || fail request_id_count
test "$(printf '%s' "$query" | jq -r '.dedup_rows')" = 1 || fail dedup_row_count
test "$(printf '%s' "$query" | jq -r '.legacy_billing_rows')" = 0 || fail unexpected_legacy_billing_row
test "$(printf '%s' "$query" | jq -r '.negative_cost_rows')" = 0 || fail negative_cost
total_cost=$(printf '%s' "$query" | jq -r '.total_cost')
actual_cost=$(printf '%s' "$query" | jq -r '.actual_cost')
awk -v total="$total_cost" -v actual="$actual_cost" 'BEGIN {exit !(total > 0 && actual > 0 && total == actual)}' || fail abnormal_cost
db_input=$(printf '%s' "$query" | jq -r '.input_tokens')
db_cached=$(printf '%s' "$query" | jq -r '.cached_tokens')
db_output=$(printf '%s' "$query" | jq -r '.output_tokens')
test $((db_input + db_cached)) = "$response_input" || fail input_token_mismatch
test "$db_cached" = "$response_cached" || fail cached_token_mismatch
test "$db_output" = "$response_output" || fail output_token_mismatch

printf 'billing_check=passed\n'
printf 'billing_usage_rows=1\n'
printf 'billing_dedup_rows=1\n'
printf 'billing_input_tokens=%s\n' "$response_input"
printf 'billing_cached_tokens=%s\n' "$response_cached"
printf 'billing_output_tokens=%s\n' "$response_output"
