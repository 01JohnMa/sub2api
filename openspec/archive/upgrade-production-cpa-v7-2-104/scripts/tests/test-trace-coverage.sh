#!/usr/bin/env bash
set -Eeuo pipefail

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
scripts_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
source "$scripts_dir/validator-case-runner.sh"

tmpdir=$(mktemp -d /private/tmp/cpa-v104-trace-coverage-test.XXXXXX)
trap 'rm -rf -- "$tmpdir"' EXIT

fail_test() {
  printf 'trace_coverage_test_error=%s\n' "$1" >&2
  exit 1
}

expect_eq() {
  local actual=$1 expected=$2 label=$3
  test "$actual" = "$expected" || fail_test "$label"
}

expect_failure() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then
    fail_test "$label"
  fi
}

require_literal() {
  local file=$1 literal=$2 label=$3
  grep -Fq -- "$literal" "$file" || fail_test "$label"
}

valid_headers=$tmpdir/valid.headers
printf '%s\r\n' \
  'HTTP/1.1 200 OK' \
  'Content-Type: application/json' \
  'X-CPA-TRACE-ID: 20260802123456-0123456789abcdef-deadbeef' \
  '' > "$valid_headers"
expect_eq "$(http_final_header_count "$valid_headers" X-CPA-TRACE-ID)" 1 valid_header_count
expect_eq "$(cpa_trace_auth_index_from_headers "$valid_headers")" 0123456789abcdef valid_trace_parse

multi_block_headers=$tmpdir/multi-block.headers
printf '%s\r\n' \
  'HTTP/1.1 200 Connection established' \
  'X-CPA-TRACE-ID: ignored-proxy-value' \
  '' \
  'HTTP/2 103 Early Hints' \
  'X-CPA-TRACE-ID: ignored-interim-value' \
  '' \
  'HTTP/2 200' \
  'x-cpa-trace-id: 20260802123457-aaaaaaaaaaaaaaaa-0123abcd' \
  'content-type: application/json' \
  '' > "$multi_block_headers"
expect_eq "$(http_final_header_count "$multi_block_headers" x-cpa-trace-id)" 1 final_block_header_count
expect_eq "$(cpa_trace_auth_index_from_headers "$multi_block_headers")" aaaaaaaaaaaaaaaa final_block_trace_parse

missing_final_headers=$tmpdir/missing-final.headers
printf '%s\r\n' \
  'HTTP/1.1 200 Connection established' \
  'X-CPA-TRACE-ID: 20260802123456-0123456789abcdef-deadbeef' \
  '' \
  'HTTP/2 200' \
  'content-type: application/json' \
  '' > "$missing_final_headers"
expect_eq "$(http_final_header_count "$missing_final_headers" X-CPA-TRACE-ID)" 0 missing_final_header_count
expect_failure missing_final_trace cpa_trace_auth_index_from_headers "$missing_final_headers"

duplicate_headers=$tmpdir/duplicate.headers
printf '%s\r\n' \
  'HTTP/2 200' \
  'X-CPA-TRACE-ID: 20260802123456-0123456789abcdef-deadbeef' \
  'x-cpa-trace-id: 20260802123457-aaaaaaaaaaaaaaaa-0123abcd' \
  '' > "$duplicate_headers"
expect_eq "$(http_final_header_count "$duplicate_headers" X-CPA-TRACE-ID)" 2 duplicate_header_count
expect_failure duplicate_trace cpa_trace_auth_index_from_headers "$duplicate_headers"

duplicate_trailing_empty_headers=$tmpdir/duplicate-trailing-empty.headers
printf '%s\r\n' \
  'HTTP/2 200' \
  'X-CPA-TRACE-ID: 20260802123456-0123456789abcdef-deadbeef' \
  'X-CPA-TRACE-ID:' \
  '' > "$duplicate_trailing_empty_headers"
expect_eq "$(http_final_header_count "$duplicate_trailing_empty_headers" X-CPA-TRACE-ID)" 2 duplicate_trailing_empty_header_count
expect_failure duplicate_trailing_empty_trace cpa_trace_auth_index_from_headers "$duplicate_trailing_empty_headers"

invalid_trace_values=(
  '20260802123456-0123456789ABCDEF-deadbeef'
  '20260802123456-0123456789abcdef-DEADBEEF'
  '2026080212345-0123456789abcdef-deadbeef'
  '20260802123456-0123456789abcde-deadbeef'
  '20260802123456-0123456789abcdef-deadbee'
  '20260802123456_0123456789abcdef_deadbeef'
  '20260802123456-0123456789abcdef-deadbeef-extra'
)
for invalid_trace in "${invalid_trace_values[@]}"; do
  invalid_headers=$tmpdir/invalid.headers
  printf '%s\r\n' \
    'HTTP/1.1 200 OK' \
    "X-CPA-TRACE-ID: $invalid_trace" \
    '' > "$invalid_headers"
  expect_failure invalid_trace_format cpa_trace_auth_index_from_headers "$invalid_headers"
done
printf 'trace_header_parsing=passed\n'

expected='["2222222222222222","aaaaaaaaaaaaaaaa","1111111111111111"]'
covered='[]'
covered=$(trace_coverage_add "$expected" "$covered" aaaaaaaaaaaaaaaa)
expect_eq "$covered" '["aaaaaaaaaaaaaaaa"]' coverage_add_first
expect_failure incomplete_coverage trace_coverage_complete "$expected" "$covered"
covered=$(trace_coverage_add "$expected" "$covered" 1111111111111111)
expect_eq "$covered" '["1111111111111111","aaaaaaaaaaaaaaaa"]' coverage_add_sorted
covered=$(trace_coverage_add "$expected" "$covered" aaaaaaaaaaaaaaaa)
expect_eq "$covered" '["1111111111111111","aaaaaaaaaaaaaaaa"]' coverage_add_deduplicated
expect_failure unknown_auth_index trace_coverage_add "$expected" "$covered" ffffffffffffffff
expect_failure invalid_existing_coverage trace_coverage_add "$expected" '["ffffffffffffffff"]' 2222222222222222
covered=$(trace_coverage_add "$expected" "$covered" 2222222222222222)
expect_eq "$covered" '["1111111111111111","2222222222222222","aaaaaaaaaaaaaaaa"]' coverage_add_complete
trace_coverage_complete "$expected" "$covered" || fail_test coverage_3_of_3
expect_failure invalid_expected_coverage trace_coverage_complete '["1111111111111111","1111111111111111","aaaaaaaaaaaaaaaa"]' "$covered"
printf 'trace_coverage_set=passed\n'

for valid_counter in '[0,0,0]' '[0,1,1]' '[2,7,3]'; do
  require_auth_counter_deltas "$valid_counter" || fail_test valid_auth_counter
done
for invalid_counter in '[-1,0,1]' '[0,1]' '[0,0,1,2]' '[0,0,"1"]' '[0,0,1.5]' '[0,null,1]' '{"a":1}' 'not-json'; do
  expect_failure invalid_auth_counter require_auth_counter_deltas "$invalid_counter"
done
printf 'auth_counter_contract=passed\n'

live_script=$scripts_dir/cases/live-direct.sh
responses_script=$scripts_dir/cases/responses-paths.sh
validate_script=$scripts_dir/validate-immediate.sh

require_literal "$live_script" 'trace_required=${CPA_REQUIRE_TRACE_COVERAGE:-0}' live_trace_defaults_legacy
require_literal "$live_script" 'if [ "$trace_required" = 1 ]; then' live_trace_candidate_only
require_literal "$live_script" 'trace_coverage_mode=legacy' live_legacy_mode
require_literal "$live_script" 'trace_coverage_mode=candidate' live_candidate_mode
require_literal "$live_script" 'trace_probe_limit=12' live_probe_limit
require_literal "$live_script" 'trace_window_seconds=30' live_trace_window
require_literal "$live_script" 'input:("Reply with exactly "+$marker)' live_marker_without_period
require_literal "$live_script" 'response_output_text_matches "$tmpdir/trace-$n.json" "$marker"' live_exact_marker_match
require_literal "$live_script" 'trace_deadline=$((trace_started + trace_window_seconds))' live_absolute_deadline
require_literal "$live_script" 'remaining=$((trace_deadline - now))' live_remaining_budget
require_literal "$live_script" '--max-time "$remaining"' live_curl_remaining_budget
require_literal "$live_script" 'if [ "$remaining" -le 1 ]; then' live_deadline_guard
require_literal "$live_script" 'if [ "$remaining" -gt 2 ]; then' live_bounded_sleep
require_literal "$live_script" 'trace_coverage_exhausted=true' live_coverage_exhausted_state
require_literal "$live_script" 'if ! trace_coverage_complete "$expected_trace_indices" "$trace_covered_indices"; then' live_coverage_exhaustion_check
require_literal "$live_script" 'emit trace_coverage_exhausted "$trace_coverage_exhausted"' live_coverage_exhausted_output

require_literal "$responses_script" '-D "$tmpdir/local.headers"' responses_local_headers
require_literal "$responses_script" '-D "$tmpdir/public.headers"' responses_public_headers
require_literal "$responses_script" 'emit "${path}_cpa_trace_header_count" "$(http_final_header_count "$tmpdir/$path.headers" X-CPA-TRACE-ID)"' responses_trace_header_counts

require_literal "$validate_script" 'export CPA_REQUIRE_TRACE_COVERAGE=1' validate_candidate_trace_enabled
require_literal "$validate_script" 'require_eq "$out" trace_coverage_mode candidate' validate_candidate_mode
require_literal "$validate_script" 'require_eq "$out" trace_expected_count 3' validate_expected_3
require_literal "$validate_script" 'require_eq "$out" trace_covered_count 3' validate_covered_3
require_literal "$validate_script" 'require_eq "$out" local_cpa_trace_header_count 0' validate_local_trace_filtered
require_literal "$validate_script" 'require_eq "$out" public_cpa_trace_header_count 0' validate_public_trace_filtered
printf 'trace_static_contract=passed\n'
