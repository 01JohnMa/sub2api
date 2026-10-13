#!/usr/bin/env bash
set -euo pipefail

config=/Users/lawrence/.codex/config.toml
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"
local_port=18085
tmpdir="$(mktemp -d /private/tmp/cpa-v104-stream-continuation.XXXXXX)"
tunnel_pid=""

cleanup() {
  if [ -n "$tunnel_pid" ]; then
    kill "$tunnel_pid" 2>/dev/null || true
    wait "$tunnel_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT
emit() { printf '%s=%s\n' "$1" "$2"; }

nc -z 127.0.0.1 "$local_port" 2>/dev/null && exit 2
ssh -o BatchMode=yes -o ConnectTimeout=12 -o ExitOnForwardFailure=yes -N -L "$local_port:127.0.0.1:8080" "$CPA_REMOTE_HOST" &
tunnel_pid=$!
for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$local_port" 2>/dev/null && break
  sleep 0.2
done
nc -z 127.0.0.1 "$local_port" 2>/dev/null

token="$(awk -F= '/^[[:space:]]*experimental_bearer_token[[:space:]]*=/ {v=$2; sub(/^[[:space:]]*/,"",v); sub(/[[:space:]]*$/, "", v); sub(/^"/,"",v); sub(/"$/, "", v); print v; exit}' "$config")"
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
base="http://127.0.0.1:$local_port"
tool_def='[{"type":"function","name":"get_stream_continuation_value","description":"Return a deterministic synthetic value.","parameters":{"type":"object","properties":{"key":{"type":"string"}},"required":["key"],"additionalProperties":false}}]'
continuation_marker=stream-continuation-ok
replay_instruction='Reply with exactly the value field from the preceding function_call_output and nothing else.'

tool_completed=false
tool_http=0
tool_attempts=1
stream_call='{}'
jq -n --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:"Call get_stream_continuation_value with key stream-continuation-v104-attempt-1.",tools:$tools,tool_choice:"required",parallel_tool_calls:false,prompt_cache_key:"cpa-v104-stream-continuation",stream:true}' > "$tmpdir/tool-request.json"
tool_http="$(curl --config "$tmpdir/request.curl" -N -o "$tmpdir/tool.sse" -w '%{http_code}' -X POST --data-binary "@$tmpdir/tool-request.json" "$base/v1/responses")"
awk '/^data: / {sub(/^data: /, ""); sub(/\r$/, ""); if ($0 != "[DONE]") print}' "$tmpdir/tool.sse" > "$tmpdir/tool.jsonl"
tool_terminal=$(stream_terminal_summary "$tmpdir/tool.jsonl")
tool_completed_count=$(printf '%s' "$tool_terminal" | jq -r '.completed_count')
tool_failure_type=$(printf '%s' "$tool_terminal" | jq -r '.failure_type')
tool_terminal_count=$(printf '%s' "$tool_terminal" | jq -r '.terminal_count')
if [ "$tool_http" = 200 ] && [ "$(printf '%s' "$tool_terminal" | jq -r '.completed')" = true ]; then
  stream_call="$(jq -s -c '[.[] | select(.type=="response.output_item.done") | .item | select(.type=="function_call")] | first // {}' "$tmpdir/tool.jsonl")"
  if [ "$(printf '%s' "$stream_call" | jq -r '.call_id // empty')" != "" ]; then
    tool_completed=true
  fi
fi

call_id="$(printf '%s' "$stream_call" | jq -r '.call_id // empty')"
call_name="$(printf '%s' "$stream_call" | jq -r '.name // empty')"
call_args="$(printf '%s' "$stream_call" | jq -r '.arguments // empty')"
call_namespace="$(printf '%s' "$stream_call" | jq -r '.namespace // ""')"

continue_completed=false
continue_text=false
continue_http=0
continue_attempts=0
continue_terminal_count=0
continue_failure_type=""
printf '' > "$tmpdir/continue.jsonl"
if [ "$tool_completed" = true ] && [ -n "$call_id" ] && [ -n "$call_name" ] && [ -n "$call_args" ]; then
  continue_attempts=1
  continue_replay_input=$(build_explicit_tool_replay_input "$stream_call" "$continuation_marker" "$replay_instruction")
  jq -n --argjson input "$continue_replay_input" --argjson tools "$tool_def" '{model:"gpt-5.6-sol",input:$input,tools:$tools,prompt_cache_key:"cpa-v104-stream-continuation",stream:true}' > "$tmpdir/continue-request.json"
  continue_http="$(curl --config "$tmpdir/request.curl" -N -o "$tmpdir/continue.sse" -w '%{http_code}' -X POST --data-binary "@$tmpdir/continue-request.json" "$base/v1/responses")"
  awk '/^data: / {sub(/^data: /, ""); sub(/\r$/, ""); if ($0 != "[DONE]") print}' "$tmpdir/continue.sse" > "$tmpdir/continue.jsonl"
  continue_terminal=$(stream_terminal_summary "$tmpdir/continue.jsonl")
  continue_completed_count=$(printf '%s' "$continue_terminal" | jq -r '.completed_count')
  continue_failure_type=$(printf '%s' "$continue_terminal" | jq -r '.failure_type')
  continue_terminal_count=$(printf '%s' "$continue_terminal" | jq -r '.terminal_count')
  if [ "$continue_http" = 200 ] && [ "$(printf '%s' "$continue_terminal" | jq -r '.completed')" = true ]; then
    continue_completed=true
    continue_text="$(jq -s '[.[] | select(.type=="response.output_text.delta") | .delta | select(length>0)] | length > 0' "$tmpdir/continue.jsonl")"
  fi
fi

expected_key="stream-continuation-v104-attempt-$tool_attempts"
emit tool_attempts "$tool_attempts"
emit tool_http "$tool_http"
emit tool_completed "$tool_completed"
emit tool_terminal_count "$tool_terminal_count"
emit tool_failure_type "$tool_failure_type"
emit tool_name "$call_name"
emit tool_call_id_present "$(test -n "$call_id" && echo true || echo false)"
emit tool_namespace "$call_namespace"
emit tool_arguments_valid "$(test -n "$call_args" && printf '%s' "$call_args" | jq -e --arg key "$expected_key" '.key == $key' >/dev/null 2>&1 && echo true || echo false)"
emit tool_call_id_sha256 "$(printf '%s' "$call_id" | shasum -a 256 | cut -d ' ' -f 1)"
emit continuation_attempts "$continue_attempts"
emit continuation_http "$continue_http"
emit continuation_completed "$continue_completed"
emit continuation_terminal_count "$continue_terminal_count"
emit continuation_failure_type "$continue_failure_type"
emit continuation_text_nonempty "$continue_text"
emit continuation_output_matches "$(stream_output_text_matches "$tmpdir/continue.jsonl" "$continuation_marker" && echo true || echo false)"
emit continuation_input_tokens "$(jq -s -r '[.[] | select(.type=="response.completed") | .response.usage.input_tokens] | first // 0' "$tmpdir/continue.jsonl" 2>/dev/null || echo 0)"
emit continuation_cached_tokens "$(jq -s -r '[.[] | select(.type=="response.completed") | .response.usage.input_tokens_details.cached_tokens] | first // 0' "$tmpdir/continue.jsonl" 2>/dev/null || echo 0)"
