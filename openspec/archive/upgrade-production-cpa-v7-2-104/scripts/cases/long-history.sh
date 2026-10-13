#!/usr/bin/env bash
set -Eeuo pipefail

config=${CPA_CODEX_CONFIG:-/Users/lawrence/.codex/config.toml}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"
tmpdir=$(mktemp -d /private/tmp/cpa-v104-long-history.XXXXXX)
cleanup() { rm -rf -- "$tmpdir"; }
trap cleanup EXIT

fail() {
  printf 'long_history_error=%s\n' "$1" >&2
  exit 1
}

token=$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")
test -n "$token" || fail bearer_token_missing
cat > "$tmpdir/request.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 240
header = "Authorization: Bearer $token"
header = "Content-Type: application/json"
EOF
chmod 0600 "$tmpdir/request.curl"
unset token

jq -n '
  def words: "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau";
  {
    model:"gpt-5.6-sol",
    input:([range(0;12) as $i |
      {role:(if ($i % 2)==0 then "user" else "assistant" end), content:("Synthetic history turn " + ($i|tostring) + ": " + words + " " + words + " " + words)}
    ] + [{role:"user",content:"Reply with exactly LONG-HISTORY-OK."}]),
    prompt_cache_key:"cpa-v104-post-switch-long-history",
    stream:true
  }
' > "$tmpdir/request.json"

curl --config "$tmpdir/request.curl" -N -o "$tmpdir/response.sse" \
  -w '%{http_code} %{time_starttransfer} %{time_total}\n' \
  -X POST --data-binary "@$tmpdir/request.json" \
  https://coococode.com/v1/responses > "$tmpdir/metrics"
read -r http ttft total < "$tmpdir/metrics"
awk '/^data: / {sub(/^data: /, ""); sub(/\r$/, ""); if ($0 != "[DONE]") print}' "$tmpdir/response.sse" > "$tmpdir/data.jsonl"
terminal=$(stream_terminal_summary "$tmpdir/data.jsonl")
terminal_count=$(printf '%s' "$terminal" | jq -r '.terminal_count')
failure_type=$(printf '%s' "$terminal" | jq -r '.failure_type')

printf 'long_history_http=%s\n' "$http"
printf 'long_history_ttft_seconds=%s\n' "$ttft"
printf 'long_history_total_seconds=%s\n' "$total"
printf 'long_history_completed=%s\n' "$(printf '%s' "$terminal" | jq -r '.completed')"
printf 'long_history_terminal_count=%s\n' "$terminal_count"
printf 'long_history_failure_type=%s\n' "$failure_type"
printf 'long_history_text_nonempty=%s\n' "$(jq -s '[.[] | select(.type=="response.output_text.delta") | .delta | select(length>0)] | length > 0' "$tmpdir/data.jsonl")"
printf 'long_history_input_tokens=%s\n' "$(jq -s -r '[.[] | select(.type=="response.completed") | .response.usage.input_tokens] | first // 0' "$tmpdir/data.jsonl")"
printf 'long_history_cached_tokens=%s\n' "$(jq -s -r '[.[] | select(.type=="response.completed") | .response.usage.input_tokens_details.cached_tokens] | first // 0' "$tmpdir/data.jsonl")"
printf 'long_history_output_tokens=%s\n' "$(jq -s -r '[.[] | select(.type=="response.completed") | .response.usage.output_tokens] | first // 0' "$tmpdir/data.jsonl")"
