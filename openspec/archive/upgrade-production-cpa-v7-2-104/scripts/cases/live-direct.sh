#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"

secret_file=${CPA_MANAGEMENT_SECRET_FILE:-/Users/lawrence/.codex/secrets/cpa-management-key-69.12.86.145.txt}
: "${CPA_REMOTE_HOST:?set approved production SSH target}"
local_port=18318
base=http://127.0.0.1:$local_port
tmpdir="$(mktemp -d /private/tmp/cpa-v104-live.XXXXXX)"
tunnel_pid=""
trace_required=${CPA_REQUIRE_TRACE_COVERAGE:-0}
trace_probe_limit=12
trace_window_seconds=30

case "$trace_required" in
  0|1) ;;
  *) printf 'baseline_error=invalid_trace_coverage_mode\n' >&2; exit 1 ;;
esac

cleanup() {
  if [ -n "$tunnel_pid" ]; then
    kill "$tunnel_pid" 2>/dev/null || true
    wait "$tunnel_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

emit() { printf '%s=%s\n' "$1" "$2"; }
fail() { printf 'baseline_error=%s\n' "$1" >&2; exit 1; }

[ -r "$secret_file" ] || fail management_secret_missing
if nc -z 127.0.0.1 "$local_port" 2>/dev/null; then
  fail local_tunnel_port_busy
fi

ssh -o BatchMode=yes -o ConnectTimeout=12 -o ExitOnForwardFailure=yes \
  -N -L "$local_port:127.0.0.1:8317" "$CPA_REMOTE_HOST" &
tunnel_pid=$!

for _ in $(seq 1 30); do
  if nc -z 127.0.0.1 "$local_port" 2>/dev/null; then
    break
  fi
  sleep 0.2
done
nc -z 127.0.0.1 "$local_port" 2>/dev/null || fail tunnel_not_ready

management_key="$(tr -d '\r\n' < "$secret_file")"
cat > "$tmpdir/management.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 60
header = "Authorization: Bearer $management_key"
EOF
chmod 0600 "$tmpdir/management.curl"
unset management_key

auth_status="$(curl --config "$tmpdir/management.curl" -o "$tmpdir/auth-before.json" -w '%{http_code}' "$base/v0/management/auth-files")"
[ "$auth_status" = 200 ] || fail "auth_files_http_$auth_status"
keys_status="$(curl --config "$tmpdir/management.curl" -o "$tmpdir/api-keys.json" -w '%{http_code}' "$base/v0/management/api-keys")"
[ "$keys_status" = 200 ] || fail "api_keys_http_$keys_status"

proxy_key="$(jq -r '."api-keys"[0] // empty' "$tmpdir/api-keys.json")"
[ -n "$proxy_key" ] || fail proxy_key_missing
cat > "$tmpdir/proxy.curl" <<EOF
silent
show-error
connect-timeout = 10
max-time = 180
header = "Authorization: Bearer $proxy_key"
header = "Content-Type: application/json"
EOF
chmod 0600 "$tmpdir/proxy.curl"
unset proxy_key

models_status="$(curl --config "$tmpdir/proxy.curl" -o "$tmpdir/models.json" -w '%{http_code}' "$base/v1/models")"
[ "$models_status" = 200 ] || fail "models_http_$models_status"
model="$(jq -r '[.data[]?.id] as $ids | if ($ids | index("gpt-5.6-terra")) then "gpt-5.6-terra" elif ($ids | index("gpt-5.6-sol")) then "gpt-5.6-sol" else ($ids | map(select(startswith("gpt-5"))) | first // empty) end' "$tmpdir/models.json")"
[ -n "$model" ] || fail model_missing

selected_model_catalog_count=0
for slot in 0 1 2; do
  auth_name="$(jq -r --argjson slot "$slot" '.files | sort_by(.auth_index // .id // "") | .[$slot].name // empty' "$tmpdir/auth-before.json")"
  [ -n "$auth_name" ] || fail auth_name_missing
  catalog_status="$(curl --config "$tmpdir/management.curl" \
    --get --data-urlencode "name=$auth_name" \
    -o "$tmpdir/model-catalog-$slot.json" -w '%{http_code}' \
    "$base/v0/management/auth-files/models")"
  [ "$catalog_status" = 200 ] || fail "auth_model_catalog_http_$catalog_status"
  jq -e --arg model "$model" '[.models[]?.id] | index($model) != null' \
    "$tmpdir/model-catalog-$slot.json" >/dev/null || fail selected_model_catalog_incomplete
  selected_model_catalog_count=$((selected_model_catalog_count + 1))
done
unset auth_name

expected_trace_indices='[]'
if [ "$trace_required" = 1 ]; then
  expected_trace_indices="$(jq -c '[.files[]?.auth_index] | sort' "$tmpdir/auth-before.json")"
  printf '%s' "$expected_trace_indices" | jq -e '
    type == "array"
    and length == 3
    and (unique | length) == 3
    and all(.[]; type == "string" and test("^[0-9a-f]{16}$"))
  ' >/dev/null || fail auth_index_contract_invalid
fi

jq -n --arg model "$model" '{model:$model,input:"Reply with exactly BASELINE-ONE.",stream:false}' > "$tmpdir/response-1-request.json"
response1_status="$(curl --config "$tmpdir/proxy.curl" -o "$tmpdir/response-1.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/response-1-request.json" "$base/v1/responses")"

response1_id="$(jq -r '.id // empty' "$tmpdir/response-1.json")"
if [ "$response1_status" = 200 ] && [ -n "$response1_id" ]; then
  jq -n --arg model "$model" --arg previous "$response1_id" '{model:$model,previous_response_id:$previous,input:"Reply with exactly BASELINE-TWO.",stream:false}' > "$tmpdir/response-2-request.json"
  response2_status="$(curl --config "$tmpdir/proxy.curl" -o "$tmpdir/response-2.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/response-2-request.json" "$base/v1/responses")"
else
  response2_status=skipped
  printf '{}' > "$tmpdir/response-2.json"
fi

jq -n --arg model "$model" '{model:$model,messages:[{role:"user",content:"Reply with exactly CHAT-BASELINE."}],stream:false,max_completion_tokens:32}' > "$tmpdir/chat-request.json"
chat_status="$(curl --config "$tmpdir/proxy.curl" -o "$tmpdir/chat.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/chat-request.json" "$base/v1/chat/completions")"

for n in 1 2 3; do
  jq -n --arg model "$model" --arg n "$n" '{model:$model,input:("Reply with exactly RR-"+$n+"."),stream:false,prompt_cache_key:"cpa-v104-preflight-round-robin"}' > "$tmpdir/rr-$n-request.json"
  status="$(curl --config "$tmpdir/proxy.curl" -o "$tmpdir/rr-$n.json" -w '%{http_code}' -X POST --data-binary "@$tmpdir/rr-$n-request.json" "$base/v1/responses")"
  emit "rr_${n}_http" "$status"
done

trace_coverage_mode=legacy
trace_covered_indices='[]'
trace_probe_attempts=0
trace_coverage_elapsed_seconds=0
trace_coverage_exhausted=false
if [ "$trace_required" = 1 ]; then
  trace_coverage_mode=candidate
  trace_started=$(date +%s)
  trace_deadline=$((trace_started + trace_window_seconds))
  for n in $(seq 1 "$trace_probe_limit"); do
    now=$(date +%s)
    remaining=$((trace_deadline - now))
    if [ "$remaining" -le 1 ]; then
      trace_coverage_exhausted=true
      break
    fi

    marker="TRACE-COVERAGE-$n"
    jq -n --arg model "$model" --arg marker "$marker" \
      '{model:$model,input:("Reply with exactly "+$marker),stream:false}' \
      > "$tmpdir/trace-$n-request.json"
    set +e
    trace_status="$(curl --config "$tmpdir/proxy.curl" --max-time "$remaining" \
      -D "$tmpdir/trace-$n.headers" -o "$tmpdir/trace-$n.json" -w '%{http_code}' \
      -X POST --data-binary "@$tmpdir/trace-$n-request.json" "$base/v1/responses")"
    trace_curl_rc=$?
    set -e
    trace_probe_attempts=$n
    [ "$trace_curl_rc" = 0 ] || fail trace_coverage_deadline_or_transport
    [ "$trace_status" = 200 ] || fail "trace_probe_http_$trace_status"
    [ "$(jq -r '.model // ""' "$tmpdir/trace-$n.json")" = "$model" ] || fail trace_probe_model_mismatch
    response_output_text_matches "$tmpdir/trace-$n.json" "$marker" || fail trace_probe_text_mismatch
    [ "$(jq -r '.usage.input_tokens // 0' "$tmpdir/trace-$n.json")" -gt 0 ] || fail trace_probe_input_tokens_missing
    [ "$(jq -r '.usage.output_tokens // 0' "$tmpdir/trace-$n.json")" -gt 0 ] || fail trace_probe_output_tokens_missing
    [ -z "$(jq -r '.error.type // .error.code // empty' "$tmpdir/trace-$n.json")" ] || fail trace_probe_error_present

    trace_auth_index="$(cpa_trace_auth_index_from_headers "$tmpdir/trace-$n.headers")" \
      || fail trace_header_missing_duplicate_or_invalid
    trace_covered_indices="$(trace_coverage_add "$expected_trace_indices" "$trace_covered_indices" "$trace_auth_index")" \
      || fail trace_header_unknown_auth_index
    unset trace_auth_index
    if trace_coverage_complete "$expected_trace_indices" "$trace_covered_indices"; then
      break
    fi
    remaining=$((trace_deadline - $(date +%s)))
    if [ "$remaining" -le 1 ]; then
      trace_coverage_exhausted=true
      break
    fi
    if [ "$remaining" -gt 2 ]; then
      sleep 1
    fi
  done
  trace_coverage_elapsed_seconds=$(( $(date +%s) - trace_started ))
  if ! trace_coverage_complete "$expected_trace_indices" "$trace_covered_indices"; then
    trace_coverage_exhausted=true
  fi
fi

auth_after_status="$(curl --config "$tmpdir/management.curl" -o "$tmpdir/auth-after.json" -w '%{http_code}' "$base/v0/management/auth-files")"

emit auth_files_http "$auth_status"
emit api_key_count "$(jq '."api-keys" | length' "$tmpdir/api-keys.json")"
emit auth_file_count "$(jq '.files | length' "$tmpdir/auth-before.json")"
emit auth_identity_before "$(jq -c '[.files[]? | (.auth_index // .id // "")] | sort' "$tmpdir/auth-before.json")"
emit auth_provider_counts "$(jq -c '[.files[]? | (.provider // .type // "unknown")] | sort | group_by(.) | map({provider:.[0],count:length})' "$tmpdir/auth-before.json")"
emit auth_status_counts "$(jq -c '[.files[]?.status // "unknown"] | sort | group_by(.) | map({status:.[0],count:length})' "$tmpdir/auth-before.json")"
emit auth_disabled_count "$(jq '[.files[]? | select(.disabled == true)] | length' "$tmpdir/auth-before.json")"
emit auth_unavailable_count "$(jq '[.files[]? | select(.unavailable == true)] | length' "$tmpdir/auth-before.json")"
emit auth_runtime_only_count "$(jq '[.files[]? | select(.runtime_only == true)] | length' "$tmpdir/auth-before.json")"
emit auth_hard_error_count "$(auth_hard_status_count "$tmpdir/auth-before.json")"
emit auth_success_total "$(jq '[.files[]?.success // 0] | add // 0' "$tmpdir/auth-before.json")"
emit auth_failed_total "$(jq '[.files[]?.failed // 0] | add // 0' "$tmpdir/auth-before.json")"
emit auth_recent_success "$(jq '[.files[]?.recent_requests[]?.success // 0] | add // 0' "$tmpdir/auth-before.json")"
emit auth_recent_failed "$(jq '[.files[]?.recent_requests[]?.failed // 0] | add // 0' "$tmpdir/auth-before.json")"
emit auth_after_http "$auth_after_status"
emit auth_file_count_after "$(jq '.files | length' "$tmpdir/auth-after.json")"
emit auth_identity_after "$(jq -c '[.files[]? | (.auth_index // .id // "")] | sort' "$tmpdir/auth-after.json")"
emit auth_provider_counts_after "$(jq -c '[.files[]? | (.provider // .type // "unknown")] | sort | group_by(.) | map({provider:.[0],count:length})' "$tmpdir/auth-after.json")"
emit auth_status_counts_after "$(jq -c '[.files[]?.status // "unknown"] | sort | group_by(.) | map({status:.[0],count:length})' "$tmpdir/auth-after.json")"
emit auth_disabled_count_after "$(jq '[.files[]? | select(.disabled == true)] | length' "$tmpdir/auth-after.json")"
emit auth_unavailable_count_after "$(jq '[.files[]? | select(.unavailable == true)] | length' "$tmpdir/auth-after.json")"
emit auth_runtime_only_count_after "$(jq '[.files[]? | select(.runtime_only == true)] | length' "$tmpdir/auth-after.json")"
emit auth_hard_error_count_after "$(auth_hard_status_count "$tmpdir/auth-after.json")"
emit models_http "$models_status"
emit models_count "$(jq '.data | length' "$tmpdir/models.json")"
emit model_selected "$model"
emit model_gpt56_ids "$(jq -c '[.data[]?.id | select(startswith("gpt-5.6"))] | sort' "$tmpdir/models.json")"
emit auth_selected_model_catalog_count "$selected_model_catalog_count"
emit trace_coverage_mode "$trace_coverage_mode"
emit trace_expected_count "$(printf '%s' "$expected_trace_indices" | jq 'length')"
emit trace_covered_count "$(printf '%s' "$trace_covered_indices" | jq 'length')"
emit trace_probe_attempts "$trace_probe_attempts"
emit trace_probe_limit "$trace_probe_limit"
emit trace_window_seconds "$trace_window_seconds"
emit trace_coverage_elapsed_seconds "$trace_coverage_elapsed_seconds"
emit trace_coverage_exhausted "$trace_coverage_exhausted"

emit responses_first_http "$response1_status"
emit responses_first_model "$(jq -r '.model // ""' "$tmpdir/response-1.json")"
emit responses_first_text_nonempty "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/response-1.json")"
emit responses_first_input_tokens "$(jq -r '.usage.input_tokens // 0' "$tmpdir/response-1.json")"
emit responses_first_cached_tokens "$(jq -r '.usage.input_tokens_details.cached_tokens // 0' "$tmpdir/response-1.json")"
emit responses_first_output_tokens "$(jq -r '.usage.output_tokens // 0' "$tmpdir/response-1.json")"
emit responses_first_reasoning_tokens "$(jq -r '.usage.output_tokens_details.reasoning_tokens // 0' "$tmpdir/response-1.json")"
emit responses_first_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/response-1.json")"
emit responses_first_id_sha256 "$(printf '%s' "$response1_id" | shasum -a 256 | cut -d ' ' -f 1)"

emit responses_continue_http "$response2_status"
emit responses_continue_text_nonempty "$(jq '[.output[]?.content[]? | select(.type=="output_text") | .text | select(length>0)] | length > 0' "$tmpdir/response-2.json")"
emit responses_continue_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/response-2.json")"
emit responses_continue_error_code "$(jq -r '.error.code // ""' "$tmpdir/response-2.json")"

emit chat_http "$chat_status"
emit chat_model "$(jq -r '.model // ""' "$tmpdir/chat.json")"
emit chat_text_nonempty "$(jq '[.choices[]?.message.content // "" | select(length>0)] | length > 0' "$tmpdir/chat.json")"
emit chat_input_tokens "$(jq -r '.usage.prompt_tokens // 0' "$tmpdir/chat.json")"
emit chat_cached_tokens "$(jq -r '.usage.prompt_tokens_details.cached_tokens // 0' "$tmpdir/chat.json")"
emit chat_output_tokens "$(jq -r '.usage.completion_tokens // 0' "$tmpdir/chat.json")"
emit chat_reasoning_tokens "$(jq -r '.usage.completion_tokens_details.reasoning_tokens // 0' "$tmpdir/chat.json")"
emit chat_error_type "$(jq -r '.error.type // .type // ""' "$tmpdir/chat.json")"

emit auth_success_deltas_sorted "$(jq -n -c --slurpfile before "$tmpdir/auth-before.json" --slurpfile after "$tmpdir/auth-after.json" '[($after[0].files[]? | {key:(.auth_index // .id), value:(.success // 0)})] | from_entries as $a | [($before[0].files[]? | {key:(.auth_index // .id), value:(.success // 0)})] | from_entries as $b | [$a | to_entries[] | (.value - ($b[.key] // 0))] | sort')"
emit auth_failed_deltas_sorted "$(jq -n -c --slurpfile before "$tmpdir/auth-before.json" --slurpfile after "$tmpdir/auth-after.json" '[($after[0].files[]? | {key:(.auth_index // .id), value:(.failed // 0)})] | from_entries as $a | [($before[0].files[]? | {key:(.auth_index // .id), value:(.failed // 0)})] | from_entries as $b | [$a | to_entries[] | (.value - ($b[.key] // 0))] | sort')"
