#!/usr/bin/env bash
set -euo pipefail

config=/Users/lawrence/.codex/config.toml
tmpdir="$(mktemp -d /private/tmp/cpa-v104-tools-followup.XXXXXX)"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT
emit() { printf '%s=%s\n' "$1" "$2"; }

token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
[ -n "$token" ]
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

tool_def='[{"type":"function","name":"get_followup_value","description":"Return a deterministic synthetic value.","parameters":{"type":"object","properties":{"key":{"type":"string"}},"required":["key"],"additionalProperties":false}}]'
explicit_marker=explicit-replay-ok
stream_marker=stream-explicit-replay-ok
replay_instruction='Reply with exactly the value field from the preceding function_call_output and nothing else.'
jq -n --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:"Call get_followup_value with key followup-v104.",tools:$tools,tool_choice:"required",parallel_tool_calls:false,prompt_cache_key:"cpa-v104-followup-tools",stream:false}' > "$tmpdir/tool-request.json"
tool_http="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/tool.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/tool-request.json" https://coococode.com/v1/responses)"
call_id="$(jq -r '[.output[]? | select(.type=="function_call") | .call_id] | first // empty' "$tmpdir/tool.json")"
call_item="$(jq -c '[.output[]? | select(.type=="function_call")] | first // {}' "$tmpdir/tool.json")"
tool_name="$(printf '%s' "$call_item" | jq -r '.name // empty')"
tool_arguments="$(printf '%s' "$call_item" | jq -r '.arguments // empty')"

explicit_continue_http=skipped
printf '{}' > "$tmpdir/explicit-continue.json"
if [ "$tool_http" = 200 ] && [ -n "$call_id" ]; then
  explicit_replay_input=$(build_explicit_tool_replay_input "$call_item" "$explicit_marker" "$replay_instruction")
  jq -n --argjson input "$explicit_replay_input" --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:$input,tools:$tools,prompt_cache_key:"cpa-v104-followup-tools",stream:false}' > "$tmpdir/explicit-continue-request.json"
  explicit_continue_http="$(curl --config "$tmpdir/request.curl" -o "$tmpdir/explicit-continue.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/explicit-continue-request.json" https://coococode.com/v1/responses)"
fi

jq -n --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:"Call get_followup_value with key stream-followup-v104.",tools:$tools,tool_choice:"required",parallel_tool_calls:false,prompt_cache_key:"cpa-v104-followup-stream",stream:true}' > "$tmpdir/stream-request.json"
curl --config "$tmpdir/request.curl" -N -D "$tmpdir/stream.headers" -o "$tmpdir/stream.sse" \
  -w '%{http_code} %{time_starttransfer} %{time_total}\n' \
  -X POST --data-binary "@$tmpdir/stream-request.json" \
  https://coococode.com/v1/responses > "$tmpdir/stream.metrics"
read -r stream_http stream_ttft stream_total < "$tmpdir/stream.metrics"
awk '/^event: / {sub(/^event: /, ""); sub(/\r$/, ""); print}' "$tmpdir/stream.sse" > "$tmpdir/events.txt"
awk '/^data: / {sub(/^data: /, ""); sub(/\r$/, ""); if ($0 != "[DONE]") print}' "$tmpdir/stream.sse" > "$tmpdir/data.jsonl"
stream_terminal=$(stream_terminal_summary "$tmpdir/data.jsonl")
stream_failure_count=$(printf '%s' "$stream_terminal" | jq -r '.failure_count')
stream_terminal_count=$(printf '%s' "$stream_terminal" | jq -r '.terminal_count')
stream_failure_type=$(printf '%s' "$stream_terminal" | jq -r '.failure_type')

stream_call="$(jq -s -c '[.[] |
  select(.type=="response.output_item.done") | .item |
  select(.type=="function_call")
] | first // {}' "$tmpdir/data.jsonl")"
stream_call_id="$(printf '%s' "$stream_call" | jq -r '.call_id // empty')"
stream_call_namespace="$(printf '%s' "$stream_call" | jq -r '.namespace // ""')"

stream_continue_http=skipped
stream_continue_completed=false
stream_continue_terminal_count=0
stream_continue_failure_type=""
stream_continue_text_nonempty=false
printf '' > "$tmpdir/stream-continue.jsonl"
if [ "$stream_http" = 200 ] && [ -n "$stream_call_id" ]; then
  stream_replay_input=$(build_explicit_tool_replay_input "$stream_call" "$stream_marker" "$replay_instruction")
  jq -n --argjson input "$stream_replay_input" --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:$input,tools:$tools,prompt_cache_key:"cpa-v104-followup-stream",stream:true}' > "$tmpdir/stream-continue-request.json"
  stream_continue_http="$(curl --config "$tmpdir/request.curl" -N -o "$tmpdir/stream-continue.sse" -w '%{http_code}' -X POST --data-binary "@$tmpdir/stream-continue-request.json" https://coococode.com/v1/responses)"
  awk '/^data: / {sub(/^data: /, ""); sub(/\r$/, ""); if ($0 != "[DONE]") print}' "$tmpdir/stream-continue.sse" > "$tmpdir/stream-continue.jsonl"
  stream_continue_terminal=$(stream_terminal_summary "$tmpdir/stream-continue.jsonl")
  stream_continue_terminal_count=$(printf '%s' "$stream_continue_terminal" | jq -r '.terminal_count')
  stream_continue_completed=$(printf '%s' "$stream_continue_terminal" | jq -r '.completed')
  stream_continue_failure_type=$(printf '%s' "$stream_continue_terminal" | jq -r '.failure_type')
  stream_continue_text_nonempty=$(jq -s '[.[] | select(.type=="response.output_text.delta") | .delta | select(length>0)] | length > 0' "$tmpdir/stream-continue.jsonl")
fi

redact_message() {
  sed -E 's/call_[A-Za-z0-9_-]+/<call_id>/g; s/resp_[A-Za-z0-9_-]+/<response_id>/g' | cut -c1-240
}

emit tool_http "$tool_http"
emit tool_status "$(jq -r '.status // ""' "$tmpdir/tool.json")"
emit tool_name "$tool_name"
emit tool_call_count "$(jq '[.output[]? | select(.type=="function_call")] | length' "$tmpdir/tool.json")"
emit tool_call_id_present "$(test -n "$call_id" && echo true || echo false)"
emit tool_namespace "$(printf '%s' "$call_item" | jq -r '.namespace // ""')"
emit tool_arguments_valid "$(test -n "$tool_arguments" && printf '%s' "$tool_arguments" | jq -e '.key == "followup-v104"' >/dev/null 2>&1 && echo true || echo false)"
emit tool_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/tool.json")"
emit explicit_continue_http "$explicit_continue_http"
emit explicit_continue_status "$(jq -r '.status // ""' "$tmpdir/explicit-continue.json")"
emit explicit_continue_completed "$(jq -r '(.status == "completed") and ((.error // null) == null)' "$tmpdir/explicit-continue.json")"
emit explicit_continue_text_nonempty "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/explicit-continue.json")"
emit explicit_continue_output_matches "$(response_output_text_matches "$tmpdir/explicit-continue.json" "$explicit_marker" && echo true || echo false)"
emit explicit_continue_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/explicit-continue.json")"
emit explicit_continue_error_message_redacted "$(jq -r '.error.message // ""' "$tmpdir/explicit-continue.json" | redact_message)"

emit stream_http "$stream_http"
emit stream_content_type "$(awk 'BEGIN{IGNORECASE=1} /^content-type:/ {sub(/\r$/, ""); print $2; exit}' "$tmpdir/stream.headers")"
emit stream_ttft_seconds "$stream_ttft"
emit stream_total_seconds "$stream_total"
emit stream_event_names "$(sort "$tmpdir/events.txt" | uniq -c | awk '{print $2 ":" $1}' | paste -sd, -)"
emit stream_json_type_counts "$(jq -s -c '[.[].type // "missing"] | sort | group_by(.) | map({type:.[0],count:length})' "$tmpdir/data.jsonl")"
emit stream_completed "$(printf '%s' "$stream_terminal" | jq -r '.completed')"
emit stream_failed "$(test "$stream_failure_count" -gt 0 && echo true || echo false)"
emit stream_terminal_count "$stream_terminal_count"
emit stream_failure_type "$stream_failure_type"
emit stream_tool_name "$(printf '%s' "$stream_call" | jq -r '.name // ""')"
emit stream_tool_call_id_present "$(test -n "$stream_call_id" && echo true || echo false)"
emit stream_tool_namespace "$stream_call_namespace"
emit stream_arguments_valid "$(printf '%s' "$stream_call" | jq -r '.arguments // "{}"' | jq -e '.key == "stream-followup-v104"' >/dev/null 2>&1 && echo true || echo false)"
emit stream_call_id_sha256 "$(printf '%s' "$stream_call_id" | shasum -a 256 | cut -d ' ' -f 1)"
emit stream_continue_http "$stream_continue_http"
emit stream_continue_completed "$stream_continue_completed"
emit stream_continue_terminal_count "$stream_continue_terminal_count"
emit stream_continue_failure_type "$stream_continue_failure_type"
emit stream_continue_text_nonempty "$stream_continue_text_nonempty"
emit stream_continue_output_matches "$(stream_output_text_matches "$tmpdir/stream-continue.jsonl" "$stream_marker" && echo true || echo false)"
emit stream_continue_mode explicit_replay
