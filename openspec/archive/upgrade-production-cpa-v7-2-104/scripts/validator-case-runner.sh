#!/usr/bin/env bash

classify_case_exit() {
  local rc=$1
  case_failure_class=contract
  case "$rc" in
    28|35|52|56|124|137|143|255)
      case_failure_class=transport_transient
      return 0
      ;;
  esac
  return 1
}

classify_error_type() {
  local error_type=${1:-}
  case_failure_class=contract
  case "$error_type" in
    dual_terminal|invalid_terminal_count|invalid_terminal_order)
      case_failure_class=transport_transient
      ;;
    invalid_grant|invalid_credential|invalid_credentials|authentication_error|unauthorized|auth_unavailable|no_available_account|no_available_accounts|refresh_failed|refresh_failure)
      case_failure_class=auth_hard
      ;;
    upstream_error|api_error|server_error|response.failed|upstream_failure|server_is_overloaded|service_unavailable_error|overloaded_error|rate_limit_error|temporarily_unavailable|timeout|connection_error)
      case_failure_class=known_5xx
      ;;
  esac
}

classify_http_failure() {
  local status=$1
  local error_type=${2:-}
  classify_error_type "$error_type"
  if test "$case_failure_class" = auth_hard; then
    return 0
  fi
  case "$status" in
    401|403)
      case_failure_class=auth_hard
      ;;
    429)
      if test -z "$error_type" || test "$case_failure_class" = known_5xx; then
        case_failure_class=known_5xx
      else
        case_failure_class=contract
      fi
      ;;
    500|502|503|504)
      test "$case_failure_class" = known_5xx || case_failure_class=contract
      ;;
    *)
      case_failure_class=contract
      ;;
  esac
}

stream_terminal_summary() {
  local file=$1
  jq -s -c '
    ([.[] | select(.type == "response.completed")] | length) as $completed_count |
    ([.[] | select(.type == "response.failed" or .type == "error")]) as $failures |
    ($failures | length) as $failure_count |
    ($completed_count + $failure_count) as $terminal_count |
    ([.[] | .type] | last // "") as $last_type |
    (($last_type == "response.completed" or $last_type == "response.failed" or $last_type == "error") and $terminal_count == 1) as $terminal_is_last |
    {
      completed_count: $completed_count,
      failure_count: $failure_count,
      terminal_count: $terminal_count,
      terminal_is_last: $terminal_is_last,
      completed: ($completed_count == 1 and $failure_count == 0 and $terminal_is_last),
      failure_type: (
        if $completed_count > 0 and $failure_count > 0 then "dual_terminal"
        elif $terminal_count != 1 then "invalid_terminal_count"
        elif $terminal_is_last | not then "invalid_terminal_order"
        else ([$failures[] |
        select(.type == "response.failed" or .type == "error") |
        (.response.error.type // .response.error.code // .error.type // .error.code // .type)
        ] | first // "")
        end
      )
    }
  ' "$file"
}

response_output_text_matches() {
  local file=$1
  local expected=$2
  jq -e --arg expected "$expected" '
    ([.output[]?.content[]? | select(.type == "output_text") | .text] | join("")) == $expected
  ' "$file" >/dev/null
}

stream_output_text_matches() {
  local file=$1
  local expected=$2
  jq -s -e --arg expected "$expected" '
    ([.[] | select(.type == "response.output_text.delta") | .delta] | join("")) == $expected
  ' "$file" >/dev/null
}

build_explicit_tool_replay_input() {
  local call_item=$1
  local marker=$2
  local instruction=$3
  local call_id
  call_id=$(printf '%s' "$call_item" | jq -r '.call_id // empty')
  if test -z "$call_id"; then
    return 1
  fi
  jq -n \
    --argjson call_item "$call_item" \
    --arg call "$call_id" \
    --arg marker "$marker" \
    --arg instruction "$instruction" \
    '[$call_item,{type:"function_call_output",call_id:$call,output:("{\"value\":\"" + $marker + "\"}")},{role:"user",content:$instruction}]'
}

auth_hard_status_count() {
  local file=$1
  jq '[
    .files[]?
    | select(
        (.status_message // "" | tostring)
        | test(
            "invalid[_ -]?grant|invalid[_ -]?credentials?|authentication[_ -]?error|unauthorized|auth[_ -]?unavailable|no[_ -]?available[_ -]?(account|accounts|credential|credentials)|refresh([_ -]?token)?[_ -]?(failed|failure|error)|(failed|failure|error)[_ -]+to[_ -]+refresh([_ -]+token)?|(^|[^0-9])(401|403)([^0-9]|$)";
            "i"
          )
      )
  ] | length' "$file"
}

require_no_auth_hard_errors() {
  local value=$1
  case_failure_class=contract
  case "$value" in
    ''|*[!0-9]*) return 1 ;;
  esac
  if test "$value" -eq 0; then
    return 0
  fi
  case_failure_class=auth_hard
  return 1
}

require_auth_counter_deltas() {
  local value=$1
  case_failure_class=contract
  printf '%s' "$value" \
    | jq -e '
        type == "array"
        and length == 3
        and all(.[]; type == "number" and . >= 0 and floor == .)
      ' >/dev/null 2>&1
}

require_auth_failure_deltas() {
  require_auth_counter_deltas "$1"
}

http_final_header_values() {
  local file=$1
  local header_name=$2
  awk -v wanted="$header_name" '
    {
      sub(/\r$/, "")
    }
    /^HTTP\/[0-9.]+[[:space:]]+[0-9][0-9][0-9]([[:space:]]|$)/ {
      block++
      count=0
      next
    }
    block > 0 {
      colon=index($0, ":")
      if (colon > 0 && tolower(substr($0, 1, colon - 1)) == tolower(wanted)) {
        value=substr($0, colon + 1)
        sub(/^[[:space:]]*/, "", value)
        values[++count]=value
      }
    }
    END {
      for (i=1; i<=count; i++) print values[i]
    }
  ' "$file"
}

http_final_header_count() {
  http_final_header_values "$1" "$2" | awk 'END {print NR+0}'
}

cpa_trace_auth_index_from_headers() {
  local file=$1
  local values count trace_value
  count=$(http_final_header_count "$file" X-CPA-TRACE-ID) || return 1
  test "$count" = 1 || return 1
  values=$(http_final_header_values "$file" X-CPA-TRACE-ID) || return 1
  test -n "$values" || return 1
  trace_value=$values
  if [[ ! "$trace_value" =~ ^[0-9]{14}-([0-9a-f]{16})-[0-9a-f]{8}$ ]]; then
    return 1
  fi
  printf '%s\n' "${BASH_REMATCH[1]}"
}

trace_coverage_add() {
  local expected=$1
  local covered=$2
  local auth_index=$3
  printf '%s\n' "$expected" | jq -e --arg auth_index "$auth_index" '
    type == "array"
    and length == 3
    and (unique | length) == 3
    and all(.[]; type == "string" and test("^[0-9a-f]{16}$"))
    and index($auth_index) != null
  ' >/dev/null 2>&1 || return 1
  printf '%s\n' "$covered" | jq -c --arg auth_index "$auth_index" --argjson expected "$expected" '
    if type != "array"
       or any(.[]; . as $item | type != "string" or ($expected | index($item) == null))
    then error("invalid coverage set")
    else (. + [$auth_index] | unique | sort)
    end
  ' 2>/dev/null
}

trace_coverage_complete() {
  local expected=$1
  local covered=$2
  jq -n -e --argjson expected "$expected" --argjson covered "$covered" '
    ($expected | type == "array" and length == 3 and (unique | length) == 3)
    and ($covered | type == "array")
    and (($expected | sort) == ($covered | unique | sort))
  ' >/dev/null 2>&1
}

report_case_failure_detail() {
  local name=$1
  local output=$2
  local detail mode expected covered attempts limit window exhausted

  detail=$(awk -F= '$1 == "baseline_error" {print $2; exit}' "$output" 2>/dev/null || true)
  if [[ "$detail" =~ ^[A-Za-z0-9_.-]+$ ]]; then
    printf 'case_%s_failure_detail=%s\n' "$name" "$detail" >&2
    return 0
  fi

  if test "$name" = live; then
    mode=$(awk -F= '$1 == "trace_coverage_mode" {print $2; exit}' "$output" 2>/dev/null || true)
    expected=$(awk -F= '$1 == "trace_expected_count" {print $2; exit}' "$output" 2>/dev/null || true)
    covered=$(awk -F= '$1 == "trace_covered_count" {print $2; exit}' "$output" 2>/dev/null || true)
    attempts=$(awk -F= '$1 == "trace_probe_attempts" {print $2; exit}' "$output" 2>/dev/null || true)
    limit=$(awk -F= '$1 == "trace_probe_limit" {print $2; exit}' "$output" 2>/dev/null || true)
    window=$(awk -F= '$1 == "trace_window_seconds" {print $2; exit}' "$output" 2>/dev/null || true)
    exhausted=$(awk -F= '$1 == "trace_coverage_exhausted" {print $2; exit}' "$output" 2>/dev/null || true)
    if test -n "$mode$expected$covered$attempts$limit$window$exhausted"; then
      printf 'case_live_trace_summary=mode:%s,expected:%s,covered:%s,attempts:%s,limit:%s,window:%s,exhausted:%s\n' \
        "${mode:-unknown}" "${expected:-unknown}" "${covered:-unknown}" "${attempts:-unknown}" \
        "${limit:-unknown}" "${window:-unknown}" "${exhausted:-unknown}" >&2
    fi
  fi
}

run_retryable_case() {
  local name=$1 script=$2 validator=$3
  local output=$tmpdir/$name.out
  local retry_class

  case_failure_class=contract
  if run_case_once "$name" "$script" "$output"; then
    if "$validator" "$output"; then
      printf 'case_%s=passed\n' "$name"
      return 0
    fi
  fi

  report_case_failure_detail "$name" "$output"

  retry_class=$case_failure_class
  case "$retry_class" in
    known_5xx|transport_transient) ;;
    auth_hard) fail "${name}_auth_hard_failure" ;;
    *) fail "${name}_contract_failure" ;;
  esac

  case_failure_class=contract
  if run_case_once "$name" "$script" "$output"; then
    if "$validator" "$output"; then
      printf 'case_%s=passed_after_%s_retry\n' "$name" "$retry_class"
      return 0
    fi
  fi
  report_case_failure_detail "$name" "$output"
  fail "${name}_consecutive_or_changed_failure"
}
