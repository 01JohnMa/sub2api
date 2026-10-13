#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case_script=$script_dir/../cases/tools-followup.sh
stream_case=$script_dir/../cases/stream-continuation.sh
validator_script=$script_dir/../validate-immediate.sh
controller_script=$script_dir/../controller.sh
runner_script=$script_dir/../validator-case-runner.sh

fail() {
  printf 'tools_followup_contract_test_failed=%s\n' "$1" >&2
  exit 1
}

if grep -Fq 'previous_response_id' "$case_script"; then
  fail public_http_previous_response_id_present
fi

grep -Fq 'build_explicit_tool_replay_input "$call_item"' "$case_script" \
  || fail non_stream_explicit_replay_missing
grep -Fq 'build_explicit_tool_replay_input "$stream_call"' "$case_script" \
  || fail stream_explicit_replay_missing
grep -Fq 'type:"function_call_output"' "$runner_script" \
  || fail explicit_tool_output_builder_missing

if grep -Fq 'server_continue_' "$case_script"; then
  fail unsupported_server_continuation_probe_present
fi

grep -Fq 'emit stream_continue_mode explicit_replay' "$case_script" \
  || fail stream_continuation_mode_evidence_missing

grep -Fq 'require_http_200 "$out" explicit_continue_http explicit_continue_error_type' "$validator_script" \
  || fail non_stream_replay_gate_missing
grep -Fq 'require_http_200 "$out" stream_continue_http stream_continue_failure_type' "$validator_script" \
  || fail stream_replay_gate_missing
grep -Fq 'require_eq "$out" stream_continue_text_nonempty true' "$validator_script" \
  || fail stream_replay_text_gate_missing

for required in \
  'require_eq "$out" tool_status completed' \
  'require_eq "$out" tool_call_count 1' \
  'require_eq "$out" tool_call_id_present true' \
  'require_eq "$out" tool_namespace ""' \
  'require_eq "$out" explicit_continue_status completed' \
  'require_eq "$out" explicit_continue_completed true' \
  'require_eq "$out" explicit_continue_output_matches true' \
  'require_eq "$out" stream_continue_output_matches true' \
  'require_eq "$out" continuation_output_matches true'; do
  grep -Fq "$required" "$validator_script" || fail "missing_gate_$required"
done

grep -Fq 'Reply with exactly the value field from the preceding function_call_output' "$case_script" \
  || fail replay_consumption_instruction_missing
grep -Fq 'build_explicit_tool_replay_input' "$runner_script" \
  || fail explicit_replay_builder_missing
grep -Fq 'build_explicit_tool_replay_input' "$case_script" \
  || fail case_does_not_use_explicit_replay_builder
grep -Fq 'emit tool_namespace "$call_namespace"' "$stream_case" \
  || fail stream_namespace_evidence_missing
grep -Fq 'emit continuation_output_matches' "$stream_case" \
  || fail stream_output_match_evidence_missing
grep -Fq 'freeze_artifact_snapshot "$script_dir" "$manifest_sha"' "$controller_script" \
  || fail controller_artifact_snapshot_missing
grep -Fq 'conditional_usage=absent' "$controller_script" \
  || fail controller_conditional_usage_gate_missing

printf 'tools_followup_contract_test=passed\n'
