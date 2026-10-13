#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/../validator-case-runner.sh"

tmpdir=$(mktemp -d /private/tmp/cpa-v104-validator-runner-test.XXXXXX)
attempt_file=$tmpdir/attempt
trap 'rm -rf -- "$tmpdir"' EXIT

fail() {
  printf 'immediate_validation_error=%s\n' "$1" >&2
  exit 1
}

run_case_once() {
  local name=$1 sequence=$2 output=$3 attempt
  attempt=$(cat "$attempt_file")
  attempt=$((attempt + 1))
  printf '%s\n' "$attempt" > "$attempt_file"
  sed -n "${attempt}p" "$sequence" > "$output"
  case "$(sed -n '1p' "$output")" in
    transport_28) classify_case_exit 28; return 28 ;;
    transport_56) classify_case_exit 56; return 56 ;;
  esac
}

fake_validator() {
  case "$(sed -n '1p' "$1")" in
    pass) return 0 ;;
    failure_deltas_011) require_auth_failure_deltas '[0,1,1]'; return $? ;;
    failure_deltas_273) require_auth_failure_deltas '[2,7,3]'; return $? ;;
    known_5xx) case_failure_class=known_5xx ;;
    auth_hard) case_failure_class=auth_hard ;;
    invalid_terminal_count) classify_error_type invalid_terminal_count ;;
    *) case_failure_class=contract ;;
  esac
  return 1
}

write_sequence() {
  local file=$1
  shift
  printf '%s\n' "$@" > "$file"
}

expect_success() {
  local label=$1 name=$2 expected_attempts=$3 expected_output=$4
  shift 4
  local sequence=$tmpdir/$label.sequence output attempts
  write_sequence "$sequence" "$@"
  printf '0\n' > "$attempt_file"
  output=$(run_retryable_case "$name" "$sequence" fake_validator)
  attempts=$(cat "$attempt_file")
  test "$attempts" = "$expected_attempts"
  test "$output" = "$expected_output"
  printf 'retry_success_%s=passed\n' "$label"
}

expect_failure() {
  local label=$1 name=$2 expected_attempts=$3 expected_error=$4
  shift 4
  local sequence=$tmpdir/$label.sequence output attempts rc
  write_sequence "$sequence" "$@"
  printf '0\n' > "$attempt_file"
  set +e
  output=$(run_retryable_case "$name" "$sequence" fake_validator 2>&1)
  rc=$?
  set -e
  test "$rc" = 1
  attempts=$(cat "$attempt_file")
  test "$attempts" = "$expected_attempts"
  printf '%s' "$output" | grep -Fq "immediate_validation_error=$expected_error"
  printf 'retry_failure_%s=passed\n' "$label"
}

expect_success first_pass live 1 'case_live=passed' pass
expect_success known_5xx_then_pass live 2 'case_live=passed_after_known_5xx_retry' known_5xx pass
expect_success transport_then_pass responses 2 'case_responses=passed_after_transport_transient_retry' transport_28 pass
expect_success malformed_terminal_then_pass stream_continuation 2 'case_stream_continuation=passed_after_transport_transient_retry' invalid_terminal_count pass
expect_success observed_failure_deltas_011 live 1 'case_live=passed' failure_deltas_011
expect_success observed_failure_deltas_273 final_live 1 'case_final_live=passed' failure_deltas_273

expect_failure contract_is_not_retryable responses 1 responses_contract_failure contract pass
expect_failure auth_hard_is_not_retryable live 1 live_auth_hard_failure auth_hard pass
expect_failure repeated_transport responses 2 responses_consecutive_or_changed_failure transport_56 transport_28
expect_failure repeated_malformed_terminal stream_continuation 2 stream_continuation_consecutive_or_changed_failure invalid_terminal_count invalid_terminal_count

case_failure_class=contract
require_auth_failure_deltas '[0,0,0]'
test "$case_failure_class" = contract

for valid in '[0,0,1]' '[0,1,1]' '[2,7,3]'; do
  case_failure_class=known_5xx
  require_auth_failure_deltas "$valid"
  test "$case_failure_class" = contract
done

for invalid in '[-1,0,1]' '[0,1]' '[0,0,1,2]' '[0,0,"1"]' '[0,0,1.5]' 'not-json'; do
  case_failure_class=known_5xx
  if require_auth_failure_deltas "$invalid"; then exit 1; fi
  test "$case_failure_class" = contract
done

printf 'auth_failure_delta_shape=passed\n'

healthy_auth_fixture=$tmpdir/healthy-auth.json
hard_auth_fixture=$tmpdir/hard-auth.json
printf '%s\n' '{"files":[{"status":"error","status_message":"temporary upstream error","unavailable":false},{"status":"active","status_message":"","unavailable":false},{"status":"active","unavailable":false}]}' > "$healthy_auth_fixture"
test "$(auth_hard_status_count "$healthy_auth_fixture")" = 0
hard_auth_messages=(
  'invalid_grant'
  'invalid credential'
  'authentication_error'
  'unauthorized'
  'auth unavailable'
  'no available account'
  'refresh failure'
  'refresh token failed'
  'refresh-token-failure'
  'failed to refresh token'
  'upstream HTTP 401'
  'upstream HTTP 403'
)
for hard_auth_message in "${hard_auth_messages[@]}"; do
  jq -n --arg message "$hard_auth_message" \
    '{files:[{status:"error",status_message:$message,unavailable:false},{status:"active",status_message:"",unavailable:false},{status:"active",unavailable:false}]}' \
    > "$hard_auth_fixture"
  test "$(auth_hard_status_count "$hard_auth_fixture")" = 1
done
case_failure_class=known_5xx
require_no_auth_hard_errors 0
test "$case_failure_class" = contract
case_failure_class=contract
if require_no_auth_hard_errors 1; then exit 1; fi
test "$case_failure_class" = auth_hard
case_failure_class=known_5xx
if require_no_auth_hard_errors invalid; then exit 1; fi
test "$case_failure_class" = contract
printf 'auth_hard_status_summary=passed\n'

case_failure_class=contract
classify_http_failure 503 server_is_overloaded
test "$case_failure_class" = known_5xx
classify_http_failure 429 rate_limit_error
test "$case_failure_class" = known_5xx
classify_http_failure 504 temporarily_unavailable
test "$case_failure_class" = known_5xx
classify_http_failure 401 invalid_grant
test "$case_failure_class" = auth_hard
classify_http_failure 503 moderation_blocked
test "$case_failure_class" = contract
printf 'http_failure_classification=passed\n'

for terminal_error in dual_terminal invalid_terminal_count invalid_terminal_order; do
  case_failure_class=contract
  classify_error_type "$terminal_error"
  test "$case_failure_class" = transport_transient
done
printf 'malformed_terminal_retry_classification=passed\n'

completed_jsonl=$tmpdir/completed.jsonl
dual_jsonl=$tmpdir/dual.jsonl
printf '%s\n' '{"type":"response.completed"}' > "$completed_jsonl"
printf '%s\n' '{"type":"response.completed"}' '{"type":"response.failed","response":{"error":{"type":"server_is_overloaded"}}}' > "$dual_jsonl"
ordered_jsonl=$tmpdir/ordered.jsonl
printf '%s\n' '{"type":"response.completed"}' '{"type":"response.output_text.delta","delta":"late"}' > "$ordered_jsonl"
summary=$(stream_terminal_summary "$completed_jsonl")
test "$(printf '%s' "$summary" | jq -r '.completed_count')" = 1
test "$(printf '%s' "$summary" | jq -r '.failure_count')" = 0
summary=$(stream_terminal_summary "$dual_jsonl")
test "$(printf '%s' "$summary" | jq -r '.completed_count')" = 1
test "$(printf '%s' "$summary" | jq -r '.failure_count')" = 1
test "$(printf '%s' "$summary" | jq -r '.terminal_count')" = 2
test "$(printf '%s' "$summary" | jq -r '.completed')" = false
test "$(printf '%s' "$summary" | jq -r '.failure_type')" = dual_terminal
summary=$(stream_terminal_summary "$ordered_jsonl")
test "$(printf '%s' "$summary" | jq -r '.terminal_is_last')" = false
test "$(printf '%s' "$summary" | jq -r '.failure_type')" = invalid_terminal_order
printf 'stream_terminal_classification=passed\n'

response_fixture=$tmpdir/response-fixture.json
printf '%s\n' '{"output":[{"content":[{"type":"output_text","text":"exact-marker"}]}]}' > "$response_fixture"
response_output_text_matches "$response_fixture" exact-marker
if response_output_text_matches "$response_fixture" wrong-marker; then exit 1; fi
stream_fixture=$tmpdir/stream-fixture.jsonl
printf '%s\n' '{"type":"response.output_text.delta","delta":"exact-"}' '{"type":"response.output_text.delta","delta":"marker"}' > "$stream_fixture"
stream_output_text_matches "$stream_fixture" exact-marker
if stream_output_text_matches "$stream_fixture" wrong-marker; then exit 1; fi
printf 'exact_tool_output_matching=passed\n'

sample_call='{"type":"function_call","call_id":"call_fixture","name":"fixture_tool","arguments":"{\"key\":\"fixture\"}"}'
replay_input=$(build_explicit_tool_replay_input "$sample_call" fixture-marker 'Use the tool output.')
printf '%s' "$replay_input" | jq -e '
  length == 3
  and .[0].type == "function_call"
  and .[0].call_id == "call_fixture"
  and .[1].type == "function_call_output"
  and .[1].call_id == .[0].call_id
  and (.[1].output | fromjson | .value) == "fixture-marker"
  and .[2].role == "user"
  and all(.[]; has("previous_response_id") | not)
' >/dev/null
if build_explicit_tool_replay_input '{"type":"function_call"}' fixture-marker instruction >/dev/null 2>&1; then exit 1; fi
printf 'explicit_tool_replay_input=passed\n'
