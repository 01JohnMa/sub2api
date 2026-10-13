#!/usr/bin/env bash
set -Eeuo pipefail

: "${CPA_REMOTE_HOST:?set approved production SSH target}"
: "${CPA_BASELINE_FILE:?set pre-switch baseline file}"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/lib-local.sh"
source "$script_dir/validator-case-runner.sh"
case_dir=$script_dir/cases
tmpdir=$(mktemp -d /private/tmp/cpa-v104-validator.XXXXXX)
validator_started=$(date +%s)
log_snapshot=""

cleanup() {
  if test -n "$log_snapshot"; then
    run_with_timeout 15 \
      ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" "bash -s -- $log_snapshot" \
      < "$script_dir/remove-log-snapshot-remote.sh" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT
trap 'cancel_active_timeout_command; exit 130' INT
trap 'cancel_active_timeout_command; exit 143' TERM
trap 'cancel_active_timeout_command; exit 129' HUP

fail() {
  printf 'immediate_validation_error=%s\n' "$1" >&2
  exit 1
}

kv() {
  local file=$1
  local key=$2
  awk -F= -v key="$key" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "$file"
}

require_eq() {
  local file=$1 key=$2 expected=$3
  test "$(kv "$file" "$key")" = "$expected" || return 1
}

require_gt_zero() {
  local value
  value=$(kv "$1" "$2")
  test "${value:-0}" -gt 0
}

require_int_between() {
  local value
  value=$(kv "$1" "$2")
  case "$value" in
    ''|*[!0-9]*) return 1 ;;
  esac
  test "$value" -ge "$3" && test "$value" -le "$4"
}

case_failure_class=contract
require_http_200() {
  local file=$1
  local key=$2
  local error_key=${3:-}
  local value error_type
  value=$(kv "$file" "$key")
  if test "$value" != 200; then
    error_type=$(test -n "$error_key" && kv "$file" "$error_key" || true)
    classify_http_failure "$value" "$error_type"
    return 1
  fi
  return 0
}

run_case_once() {
  local name=$1 script=$2 output=$3
  local rc
  if run_with_timeout 600 "$script" > "$output" 2>&1; then
    return 0
  else
    rc=$?
  fi
  classify_case_exit "$rc" || true
  printf 'case_execution_exit=%s\n' "$rc" >> "$output"
  return "$rc"
}

validate_live() {
  local out=$1
  require_eq "$out" auth_file_count 3 || return 1
  require_eq "$out" auth_disabled_count 0 || return 1
  require_eq "$out" auth_unavailable_count 0 || return 1
  require_eq "$out" auth_runtime_only_count 0 || return 1
  require_no_auth_hard_errors "$(kv "$out" auth_hard_error_count)" || return 1
  printf '%s' "$(kv "$out" auth_identity_before)" \
    | jq -e 'length == 3 and (unique | length) == 3 and all(.[]; type == "string" and length > 0)' >/dev/null || return 1
  printf '%s' "$(kv "$out" auth_provider_counts)" \
    | jq -e 'length == 1 and .[0].provider == "codex" and .[0].count == 3' >/dev/null || return 1
  printf '%s' "$(kv "$out" auth_status_counts)" \
    | jq -e '
        (map(.count) | add) == 3
        and all(.[]; (.status == "active" or .status == "error") and .count > 0)
      ' >/dev/null || return 1
  require_http_200 "$out" auth_after_http || return 1
  require_eq "$out" auth_file_count_after 3 || return 1
  test "$(kv "$out" auth_identity_after)" = "$(kv "$out" auth_identity_before)" || return 1
  printf '%s' "$(kv "$out" auth_provider_counts_after)" \
    | jq -e 'length == 1 and .[0].provider == "codex" and .[0].count == 3' >/dev/null || return 1
  printf '%s' "$(kv "$out" auth_status_counts_after)" \
    | jq -e '
        (map(.count) | add) == 3
        and all(.[]; (.status == "active" or .status == "error") and .count > 0)
      ' >/dev/null || return 1
  require_eq "$out" auth_disabled_count_after 0 || return 1
  require_eq "$out" auth_unavailable_count_after 0 || return 1
  require_eq "$out" auth_runtime_only_count_after 0 || return 1
  require_no_auth_hard_errors "$(kv "$out" auth_hard_error_count_after)" || return 1
  require_http_200 "$out" models_http || return 1
  test "$(kv "$out" models_count)" -ge 25 || return 1
  printf '%s' "$(kv "$out" model_gpt56_ids)" \
    | jq -e 'index("gpt-5.6-luna") and index("gpt-5.6-sol") and index("gpt-5.6-terra")' >/dev/null || return 1
  require_eq "$out" auth_selected_model_catalog_count 3 || return 1
  require_eq "$out" trace_coverage_mode candidate || return 1
  require_eq "$out" trace_expected_count 3 || return 1
  require_eq "$out" trace_covered_count 3 || return 1
  require_eq "$out" trace_probe_limit 12 || return 1
  require_eq "$out" trace_window_seconds 30 || return 1
  require_eq "$out" trace_coverage_exhausted false || return 1
  require_int_between "$out" trace_probe_attempts 3 12 || return 1
  require_int_between "$out" trace_coverage_elapsed_seconds 0 30 || return 1
  require_http_200 "$out" responses_first_http responses_first_error_type || return 1
  test "$(kv "$out" responses_first_model)" = "$(kv "$out" model_selected)" || return 1
  require_eq "$out" responses_first_text_nonempty true || return 1
  require_gt_zero "$out" responses_first_input_tokens || return 1
  require_gt_zero "$out" responses_first_output_tokens || return 1
  require_http_200 "$out" responses_continue_http responses_continue_error_type || return 1
  require_eq "$out" responses_continue_text_nonempty true || return 1
  require_http_200 "$out" chat_http chat_error_type || return 1
  test "$(kv "$out" chat_model)" = "$(kv "$out" model_selected)" || return 1
  require_eq "$out" chat_text_nonempty true || return 1
  for n in 1 2 3; do require_http_200 "$out" "rr_${n}_http" || return 1; done
  require_auth_counter_deltas "$(kv "$out" auth_success_deltas_sorted)" || return 1
  require_auth_counter_deltas "$(kv "$out" auth_failed_deltas_sorted)" || return 1
}

validate_responses() {
  local out=$1
  require_http_200 "$out" local_http local_error_type || return 1
  require_eq "$out" local_model gpt-5.6-sol || return 1
  require_eq "$out" local_text_nonempty true || return 1
  require_gt_zero "$out" local_input_tokens || return 1
  require_gt_zero "$out" local_output_tokens || return 1
  require_http_200 "$out" public_http public_error_type || return 1
  require_eq "$out" public_model gpt-5.6-sol || return 1
  require_eq "$out" public_text_nonempty true || return 1
  require_gt_zero "$out" public_input_tokens || return 1
  require_gt_zero "$out" public_output_tokens || return 1
  require_eq "$out" local_cpa_trace_header_count 0 || return 1
  require_eq "$out" public_cpa_trace_header_count 0 || return 1
}

validate_chat() {
  local out=$1
  require_http_200 "$out" local_http local_error_type || return 1
  require_eq "$out" local_text_nonempty true || return 1
  require_gt_zero "$out" local_input_tokens || return 1
  require_gt_zero "$out" local_output_tokens || return 1
  require_http_200 "$out" public_http public_error_type || return 1
  require_eq "$out" public_text_nonempty true || return 1
}

validate_tools() {
  local out=$1
  require_http_200 "$out" tool_http tool_error_type || return 1
  require_eq "$out" tool_status completed || return 1
  require_eq "$out" tool_call_count 1 || return 1
  require_eq "$out" tool_call_id_present true || return 1
  require_eq "$out" tool_namespace "" || return 1
  require_eq "$out" tool_name get_followup_value || return 1
  require_eq "$out" tool_arguments_valid true || return 1
  require_http_200 "$out" explicit_continue_http explicit_continue_error_type || return 1
  require_eq "$out" explicit_continue_status completed || return 1
  require_eq "$out" explicit_continue_completed true || return 1
  require_eq "$out" explicit_continue_text_nonempty true || return 1
  require_eq "$out" explicit_continue_output_matches true || return 1
  require_http_200 "$out" stream_http stream_failure_type || return 1
  if test "$(kv "$out" stream_failed)" = true; then
    classify_error_type "$(kv "$out" stream_failure_type)"
    return 1
  fi
  require_eq "$out" stream_terminal_count 1 || return 1
  require_eq "$out" stream_completed true || return 1
  require_eq "$out" stream_tool_call_id_present true || return 1
  require_eq "$out" stream_tool_namespace "" || return 1
  require_eq "$out" stream_tool_name get_followup_value || return 1
  require_eq "$out" stream_arguments_valid true || return 1
  require_http_200 "$out" stream_continue_http stream_continue_failure_type || return 1
  if test -n "$(kv "$out" stream_continue_failure_type)"; then
    classify_error_type "$(kv "$out" stream_continue_failure_type)"
    return 1
  fi
  require_eq "$out" stream_continue_terminal_count 1 || return 1
  require_eq "$out" stream_continue_completed true || return 1
  require_eq "$out" stream_continue_text_nonempty true || return 1
  require_eq "$out" stream_continue_output_matches true || return 1
}

validate_stream_continuation() {
  local out=$1
  require_http_200 "$out" tool_http tool_failure_type || return 1
  if test -n "$(kv "$out" tool_failure_type)"; then
    classify_error_type "$(kv "$out" tool_failure_type)"
    return 1
  fi
  require_eq "$out" tool_terminal_count 1 || return 1
  require_eq "$out" tool_completed true || return 1
  require_eq "$out" tool_call_id_present true || return 1
  require_eq "$out" tool_namespace "" || return 1
  require_eq "$out" tool_name get_stream_continuation_value || return 1
  require_eq "$out" tool_arguments_valid true || return 1
  require_http_200 "$out" continuation_http continuation_failure_type || return 1
  if test -n "$(kv "$out" continuation_failure_type)"; then
    classify_error_type "$(kv "$out" continuation_failure_type)"
    return 1
  fi
  require_eq "$out" continuation_terminal_count 1 || return 1
  require_eq "$out" continuation_completed true || return 1
  require_eq "$out" continuation_text_nonempty true || return 1
  require_eq "$out" continuation_output_matches true || return 1
  require_gt_zero "$out" continuation_input_tokens || return 1
}

validate_item_id() {
  local out=$1
  require_http_200 "$out" seed_http seed_error_type || return 1
  require_http_200 "$out" compliant_replay_http compliant_error_type || return 1
  require_eq "$out" compliant_text_nonempty true || return 1
  require_http_200 "$out" overlong_replay_http overlong_error_type || return 1
  require_eq "$out" overlong_text_nonempty true || return 1
}

validate_long_history() {
  local out=$1
  require_http_200 "$out" long_history_http long_history_failure_type || return 1
  if test -n "$(kv "$out" long_history_failure_type)"; then
    classify_error_type "$(kv "$out" long_history_failure_type)"
    return 1
  fi
  require_eq "$out" long_history_terminal_count 1 || return 1
  require_eq "$out" long_history_completed true || return 1
  require_eq "$out" long_history_text_nonempty true || return 1
  require_gt_zero "$out" long_history_input_tokens || return 1
  require_gt_zero "$out" long_history_output_tokens || return 1
}

validate_log_rate() {
  local out=$1
  for n in 1 2 3; do require_http_200 "$out" "request_${n}_http" || return 1; done
  test "$(kv "$out" log_file_count_after)" -ge "$(kv "$out" log_file_count_before)" || return 1
  test "$(kv "$out" nginx_lines_delta)" -gt 0 || return 1
  test "$(kv "$out" log_bytes_delta)" -ge 0 || return 1
  test "$(kv "$out" approximate_log_bytes_per_access_line)" -ge 0 || return 1
  test "$(kv "$out" approximate_log_bytes_per_access_line)" -le 70 || return 1
}

test -r "$CPA_BASELINE_FILE" || fail baseline_missing
test "$(wc -l < "$CPA_BASELINE_FILE" | tr -d ' ')" = 6 || fail baseline_invalid

remote_out=$tmpdir/remote.out
run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s' \
  < "$script_dir/validate-remote.sh" > "$remote_out"
require_eq "$remote_out" remote_process_check passed || fail remote_process_gate

log_snapshot=/run/cpa-v104-log-offsets-$validator_started-$$
log_snapshot_out=$tmpdir/log-snapshot.out
run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" "bash -s -- $log_snapshot" \
  < "$script_dir/capture-log-offsets-remote.sh" > "$log_snapshot_out"
require_eq "$log_snapshot_out" log_offset_snapshot created || fail log_snapshot_gate

export CPA_REQUIRE_TRACE_COVERAGE=1
run_retryable_case live "$case_dir/live-direct.sh" validate_live
run_retryable_case responses "$case_dir/responses-paths.sh" validate_responses
run_retryable_case chat "$case_dir/chat-paths.sh" validate_chat
run_retryable_case tools "$case_dir/tools-followup.sh" validate_tools
run_retryable_case stream_continuation "$case_dir/stream-continuation.sh" validate_stream_continuation
run_retryable_case long_history "$case_dir/long-history.sh" validate_long_history
run_retryable_case item_id "$case_dir/item-id.sh" validate_item_id

run_with_timeout 1500 "$case_dir/models-used.sh" > "$tmpdir/models.out" 2>&1 || fail models_used_gate
run_with_timeout 300 "$case_dir/billing.sh" > "$tmpdir/billing.out" 2>&1 || fail billing_gate
run_retryable_case log_rate "$case_dir/log-rate.sh" validate_log_rate

sleep 900
post_window=15
post_file=$tmpdir/post-observation.out
run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s -- 15' \
  < "$script_dir/observe-remote.sh" > "$post_file"
sample=$(sed -n '1p' "$post_file" | jq '[.endpoints | group_by(.endpoint)[] | .[0].total] | add // 0')
if test "$sample" -lt 20; then
  sleep 900
  post_window=30
  run_with_timeout 45 \
    ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s -- 30' \
    < "$script_dir/observe-remote.sh" > "$post_file"
fi
"$script_dir/compare-observation.sh" "$CPA_BASELINE_FILE" "$post_file" "$post_window" \
  > "$tmpdir/observation-compare.out" || fail short_observation_threshold

run_retryable_case final_responses "$case_dir/responses-paths.sh" validate_responses
run_retryable_case final_chat "$case_dir/chat-paths.sh" validate_chat
run_retryable_case final_tools "$case_dir/tools-followup.sh" validate_tools
run_retryable_case final_live "$case_dir/live-direct.sh" validate_live

final_remote_out=$tmpdir/final-remote.out
run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" 'bash -s' \
  < "$script_dir/validate-remote.sh" > "$final_remote_out"
require_eq "$final_remote_out" remote_process_check passed || fail final_remote_process_gate

log_out=$tmpdir/log-metadata.out
run_with_timeout 45 \
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$CPA_REMOTE_HOST" \
    "bash -s -- $validator_started $log_snapshot $CPA_LOG_BODY_CANARY $CPA_LOG_SESSION_CANARY" \
  < "$script_dir/validate-log-remote.sh" > "$log_out"
require_eq "$log_out" sensitive_metadata_matches 0 || fail sensitive_metadata
require_eq "$log_out" sensitive_file_append_matches 0 || fail sensitive_file_metadata
require_eq "$log_out" log_permission_changes 0 || fail log_permission_change
require_eq "$log_out" world_readable_new_log_files 0 || fail new_log_file_mode
log_snapshot=""

printf 'immediate_and_short_observation=passed\n'
printf 'observation_window_minutes=%s\n' "$post_window"
